# Xiaozhi 模式选择机制详解

## 🎯 核心问题

**问题:** 目前 Xiaozhi 协议中，Pipeline 还是 Realtime 模式是通过哪里来选择的？

**答案:** 通过 `ServerConfig.Mode` 在服务器启动时全局配置，不支持客户端在 hello 消息中指定。

---

## 📍 选择位置分析

### 1️⃣ 第一个选择点：服务器启动 (server.go)

```go
// server.go - NewServer()
func NewServer(cfg ServerConfig) (*Server, error) {
    switch normalizeMode(cfg.Mode) {  // ← 第一次检查
    case ModeRealtime:
        if cfg.RealtimeFactory == nil {
            return nil, errors.New("xiaozhi: nil RealtimeFactory in realtime mode")
        }
    default:
        if cfg.SessionFactory == nil {
            return nil, errors.New("xiaozhi: nil SessionFactory in pipeline mode")
        }
        if strings.TrimSpace(cfg.DialogWSURL) == "" {
            return nil, errors.New("xiaozhi: empty DialogWSURL in pipeline mode")
        }
    }
    // ...
}
```

**作用:** 验证服务器配置是否完整

**时机:** 服务器启动时

**影响范围:** 全局 (所有会话)

---

### 2️⃣ 第二个选择点：会话初始化 (session.go)

```go
// session.go - handleHello()
func (s *wsSession) handleHello(ctx context.Context, raw []byte) {
    // ... 音频参数协商 ...
    
    // 第二次检查，根据 Mode 选择处理方式
    if normalizeMode(s.cfg.Mode) == ModeRealtime {  // ← 第二次检查
        s.handleHelloRealtime(ctx)
        return
    }
    
    s.handleHelloPipeline(ctx)
}
```

**作用:** 根据模式初始化不同的处理流程

**时机:** 每个会话的 hello 消息处理时

**影响范围:** 单个会话

---

## 🔄 完整的选择流程

```
┌─────────────────────────────────────────────────────────────┐
│              Xiaozhi 模式选择流程                            │
└─────────────────────────────────────────────────────────────┘

应用启动
  │
  ├─ NewServer(ServerConfig{
  │    Mode: "pipeline" 或 "realtime"  ← 全局配置
  │  })
  │  │
  │  └─ 第一次检查: normalizeMode(cfg.Mode)
  │     ├─ 验证 RealtimeFactory (if realtime)
  │     └─ 验证 SessionFactory + DialogWSURL (if pipeline)
  │
  └─ http.HandleFunc("/ws", server.Handle)

客户端连接
  │
  ├─ Device: GET /ws?device_id=xxx
  │
  ├─ server.Handle()
  │  │
  │  ├─ conn.Upgrade()
  │  ├─ 提取 device_id
  │  ├─ 生成 callID
  │  │
  │  └─ newSession(cfg, conn, callID, deviceID)
  │     └─ cfg.Mode 继承自 ServerConfig
  │
  └─ sess.run(ctx)
     └─ 进入消息循环

客户端发送 hello
  │
  ├─ Device: {
  │    "type": "hello",
  │    "audio_params": {...}
  │  }
  │
  ├─ Server: handleText() → handleHello()
  │  │
  │  ├─ 解析 HelloMessage
  │  ├─ 协商 audio_params
  │  │  ├─ 提取客户端参数
  │  │  ├─ 合并默认参数
  │  │  └─ 保存到 wsSession
  │  │
  │  ├─ 初始化编解码器
  │  │  ├─ if format == "opus"
  │  │  │  ├─ CreateDecode(opus → pcm)
  │  │  │  └─ CreateEncode(pcm → opus)
  │  │  └─ if format == "pcm"
  │  │     └─ 跳过
  │  │
  │  └─ 第二次检查: normalizeMode(s.cfg.Mode)
  │     ├─ if ModeRealtime
  │     │  └─ handleHelloRealtime(ctx)
  │     │     ├─ 创建 Realtime Agent
  │     │     ├─ 初始化 rtOut
  │     │     └─ 发送 welcome
  │     │
  │     └─ else (ModePipeline)
  │        └─ handleHelloPipeline(ctx)
  │           ├─ 创建 Dialog Session
  │           ├─ 创建 Gateway Client
  │           └─ 发送 welcome

对话处理
  │
  ├─ Device: listen:start
  ├─ Device: [audio frames]
  │
  └─ Server: 根据模式处理
     ├─ Pipeline: ASR → Dialog → TTS
     └─ Realtime: 流式处理
```

---

## 📊 当前实现 vs 改进方案

### 当前实现

```
ServerConfig.Mode (全局)
    ↓
所有会话使用同一模式
    ↓
不支持客户端指定
    ↓
不支持动态切换
```

**优点:**
- ✅ 简单，配置清晰
- ✅ 启动时验证完整
- ✅ 性能好

**缺点:**
- ❌ 不灵活，无法按会话选择
- ❌ 客户端无法指定模式
- ❌ 无法动态切换

### 改进方案 A: 在 hello 消息中添加 mode

```
ServerConfig.Mode (默认)
    ↓
HelloMessage.Mode (客户端指定)
    ↓
优先级: 客户端 > 服务器默认
    ↓
支持客户端灵活选择
```

**实现:**

```go
type HelloMessage struct {
    Type        string
    Version     int
    Transport   string
    Features    map[string]interface{}
    AudioParams *AudioParams
    Mode        string  // ← 新增
}

func (s *wsSession) handleHello(ctx context.Context, raw []byte) {
    var msg HelloMessage
    if err := json.Unmarshal(raw, &msg); err != nil {
        s.writeText(MakeError("bad hello", true))
        return
    }
    
    // ... 音频参数协商 ...
    
    // 确定模式
    mode := msg.Mode
    if mode == "" {
        mode = s.cfg.Mode  // 使用服务器默认
    }
    
    // 验证模式可用性
    if normalizeMode(mode) == ModeRealtime {
        if s.cfg.RealtimeFactory == nil {
            s.writeText(MakeError("realtime unavailable", true))
            return
        }
        s.handleHelloRealtime(ctx)
    } else {
        if s.cfg.SessionFactory == nil {
            s.writeText(MakeError("pipeline unavailable", true))
            return
        }
        s.handleHelloPipeline(ctx)
    }
}
```

**优点:**
- ✅ 客户端可以指定模式
- ✅ 向后兼容 (mode 为空时使用服务器默认)
- ✅ 灵活性高
- ✅ 协议一致性好

**缺点:**
- ❌ 需要修改协议
- ❌ 服务器需要同时支持两种模式

### 改进方案 B: 通过 URL 查询参数

```
URL: /ws?device_id=xxx&mode=realtime
    ↓
server.Handle() 提取 mode
    ↓
覆盖 ServerConfig.Mode
    ↓
支持客户端灵活选择
```

**实现:**

```go
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
    conn, err := s.up.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    
    deviceID := strings.TrimSpace(r.Header.Get("Device-Id"))
    if deviceID == "" {
        deviceID = strings.TrimSpace(r.URL.Query().Get("device_id"))
    }
    
    // 新增: 从 URL 查询参数提取模式
    mode := strings.TrimSpace(r.URL.Query().Get("mode"))
    
    callID := fmt.Sprintf("%s-%d", s.cfg.CallIDPrefix, time.Now().UnixNano())
    cfg := s.cfg
    
    // 如果客户端指定了模式，覆盖服务器默认
    if mode != "" {
        cfg.Mode = mode
    }
    
    sess := newSession(cfg, conn, callID, deviceID)
    sess.run(r.Context())
}
```

**使用方式:**
```
ws://server:8080/ws?device_id=device-123&mode=realtime
```

**优点:**
- ✅ 不需要修改协议
- ✅ 在连接时就确定模式
- ✅ 服务器可以验证模式可用性

**缺点:**
- ❌ 模式在 hello 之前就确定
- ❌ 不够优雅 (应该在 hello 中协商)

### 改进方案 C: 通过 HTTP 请求头

```
Header: X-Voice-Mode: realtime
    ↓
server.Handle() 提取 mode
    ↓
覆盖 ServerConfig.Mode
    ↓
支持客户端灵活选择
```

**实现:**

```go
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
    conn, err := s.up.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    
    deviceID := strings.TrimSpace(r.Header.Get("Device-Id"))
    
    // 新增: 从请求头提取模式
    mode := strings.TrimSpace(r.Header.Get("X-Voice-Mode"))
    
    callID := fmt.Sprintf("%s-%d", s.cfg.CallIDPrefix, time.Now().UnixNano())
    cfg := s.cfg
    
    if mode != "" {
        cfg.Mode = mode
    }
    
    sess := newSession(cfg, conn, callID, deviceID)
    sess.run(r.Context())
}
```

**使用方式:**
```
GET /ws?device_id=device-123
X-Voice-Mode: realtime
```

**优点:**
- ✅ 不需要修改协议
- ✅ 标准 HTTP 头方式
- ✅ 灵活性好

**缺点:**
- ❌ 模式在 hello 之前就确定

---

## 🎓 推荐方案

### 最佳实践: 方案 A (在 hello 消息中添加 mode)

**原因:**
1. **协议一致性** - 所有参数在 hello 中协商
2. **向后兼容** - 旧客户端不指定 mode 时使用服务器默认
3. **灵活性** - 支持客户端按会话选择
4. **标准化** - 符合协议设计原则

**hello 消息示例:**

```json
// 客户端指定 realtime 模式
{
  "type": "hello",
  "version": 1,
  "transport": "websocket",
  "mode": "realtime",
  "audio_params": {
    "format": "opus",
    "sample_rate": 16000,
    "channels": 1,
    "frame_duration": 60,
    "bit_depth": 16
  }
}

// 服务器响应
{
  "type": "hello",
  "version": 1,
  "transport": "websocket",
  "session_id": "xz-1717756800000000000",
  "mode": "realtime",
  "audio_params": {
    "format": "opus",
    "sample_rate": 16000,
    "channels": 1,
    "frame_duration": 60,
    "bit_depth": 16
  }
}
```

---

## 📝 总结表

| 方面 | 当前实现 | 方案 A | 方案 B | 方案 C |
|------|---------|--------|--------|--------|
| **选择位置** | ServerConfig | hello 消息 | URL 参数 | HTTP 头 |
| **灵活性** | 低 | 高 | 中 | 中 |
| **向后兼容** | N/A | ✅ | ✅ | ✅ |
| **协议修改** | 无 | 有 | 无 | 无 |
| **时机** | 启动时 | hello 时 | 连接时 | 连接时 |
| **推荐度** | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ |

---

## 🚀 实现建议

如果要支持客户端指定模式，建议:

1. **短期** (快速方案): 使用方案 B 或 C
   - 不需要修改协议
   - 快速实现
   - 满足基本需求

2. **长期** (标准方案): 使用方案 A
   - 修改协议，在 hello 中添加 mode 字段
   - 更符合设计原则
   - 更灵活和可扩展

需要我帮你实现任何一个方案吗？
