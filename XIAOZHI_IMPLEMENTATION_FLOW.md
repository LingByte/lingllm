# Xiaozhi 协议实现流程分析

## 📊 模式选择机制

### 当前实现的选择方式

目前有 **两层选择机制**：

#### 第一层：服务器初始化时 (Server 创建时)

```go
// server.go - NewServer()
func NewServer(cfg ServerConfig) (*Server, error) {
    switch normalizeMode(cfg.Mode) {
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

**选择来源:** `ServerConfig.Mode`

**时机:** 服务器启动时，一次性配置

**特点:** 
- 全局配置，所有会话使用同一模式
- 启动时验证必需的工厂和配置
- 不支持动态切换

#### 第二层：会话初始化时 (hello 消息处理)

```go
// session.go - handleHello()
func (s *wsSession) handleHello(ctx context.Context, raw []byte) {
    // 1. 解析 hello 消息
    var msg HelloMessage
    if err := json.Unmarshal(raw, &msg); err != nil {
        s.writeText(MakeError("bad hello", true))
        return
    }
    
    // 2. 协商音频参数
    ap := DefaultHelloAudio()
    if msg.AudioParams != nil {
        ap = *msg.AudioParams
    }
    MergeHelloAudio(&ap)
    
    // 3. 保存音频参数
    s.inFormat = ap.Format
    s.inSR = ap.SampleRate
    s.inFrameMs = ap.FrameDuration
    s.outFormat = ap.Format
    s.outSR = ap.SampleRate
    s.ttsWireFrameMs = ap.FrameDuration
    
    // 4. 初始化编解码器
    if s.inFormat == AudioFormatOpus {
        dec, err := encoder.CreateDecode(...)
        if err != nil {
            s.writeText(MakeError("opus decoder unavailable", true))
            return
        }
        s.opusDec = dec
    }
    if s.outFormat == AudioFormatOpus {
        enc, err := encoder.CreateEncode(...)
        if err != nil {
            s.writeText(MakeError("opus encoder unavailable", true))
            return
        }
        s.opusEnc = enc
    }
    
    // 5. 根据 ServerConfig.Mode 选择处理方式
    if normalizeMode(s.cfg.Mode) == ModeRealtime {
        s.handleHelloRealtime(ctx)
        return
    }
    
    s.handleHelloPipeline(ctx)
}
```

**选择来源:** `ServerConfig.Mode` (从 server.cfg 继承)

**时机:** 每个会话的 hello 消息处理时

**特点:**
- 基于服务器配置，不是客户端指定
- 每个会话独立处理
- 编解码器初始化在模式选择之前

---

## 🔄 完整实现流程

### 流程图

```
┌──────────────────────────────────────────────────────────────┐
│                  Xiaozhi 实现流程                             │
└──────────────────────────────────────────────────────────────┘

1️⃣ 服务器启动阶段
   ├─ NewServer(cfg ServerConfig)
   │  └─ cfg.Mode = "pipeline" 或 "realtime"
   │     ├─ 验证 Mode
   │     ├─ 验证必需的工厂 (SessionFactory 或 RealtimeFactory)
   │     └─ 验证必需的配置 (DialogWSURL for pipeline)
   │
   └─ http.HandleFunc("/ws", server.Handle)

2️⃣ 客户端连接阶段
   ├─ Device 发起 WebSocket 连接
   │  └─ GET /ws?device_id=xxx
   │
   ├─ server.Handle()
   │  ├─ 升级 HTTP 连接为 WebSocket
   │  ├─ 提取 device_id
   │  ├─ 生成 callID
   │  └─ 创建 wsSession
   │
   └─ sess.run(ctx)
      └─ 进入消息循环

3️⃣ 音频参数协商阶段
   ├─ Device 发送 hello 消息
   │  └─ {
   │      "type": "hello",
   │      "audio_params": {
   │        "format": "opus",
   │        "sample_rate": 16000,
   │        "channels": 1,
   │        "frame_duration": 60,
   │        "bit_depth": 16
   │      }
   │    }
   │
   ├─ Server 处理 handleHello()
   │  ├─ 解析 hello 消息
   │  ├─ 提取 audio_params
   │  ├─ 合并默认参数 (MergeHelloAudio)
   │  ├─ 保存到 wsSession
   │  │  ├─ inFormat, inSR, inFrameMs
   │  │  ├─ outFormat, outSR, ttsWireFrameMs
   │  │  └─ 调整 PCM 帧时长 (if >= 40ms → 20ms)
   │  │
   │  └─ Server 响应 hello
   │     └─ {
   │         "type": "hello",
   │         "session_id": "xz-123",
   │         "audio_params": {...}
   │       }

4️⃣ 编解码器初始化阶段
   ├─ if format == "opus"
   │  ├─ encoder.CreateDecode(opus → pcm)
   │  │  └─ 保存到 s.opusDec
   │  │
   │  └─ encoder.CreateEncode(pcm → opus)
   │     └─ 保存到 s.opusEnc
   │
   └─ if format == "pcm"
      └─ 跳过编解码器初始化

5️⃣ 模式选择阶段
   ├─ normalizeMode(s.cfg.Mode)
   │  ├─ "realtime" → ModeRealtime
   │  ├─ "omni" → ModeRealtime
   │  ├─ "multimodal" → ModeRealtime
   │  └─ 其他 → ModePipeline (默认)
   │
   ├─ if ModeRealtime
   │  └─ s.handleHelloRealtime(ctx)
   │     ├─ 创建 Realtime Agent
   │     ├─ 初始化输出节流器
   │     └─ 启动 Realtime 处理
   │
   └─ else (ModePipeline)
      └─ s.handleHelloPipeline(ctx)
         ├─ 创建 Dialog Session
         ├─ 创建 Gateway Client
         └─ 启动 Dialog 处理

6️⃣ 对话处理阶段
   ├─ Pipeline 模式
   │  ├─ Device 发送 listen:start
   │  ├─ Device 发送 audio frames
   │  ├─ Server 解码 (if opus)
   │  ├─ Server 发送到 Dialog
   │  ├─ Dialog 进行 ASR → LLM → TTS
   │  ├─ Server 编码 (if opus)
   │  └─ Server 返回 audio frames
   │
   └─ Realtime 模式
      ├─ Device 发送 listen:start
      ├─ Device 持续发送 audio frames
      ├─ Server 解码 (if opus)
      ├─ Server 推送到 Realtime Agent
      ├─ Agent 进行流式 ASR/LLM/TTS
      ├─ Server 编码 (if opus)
      └─ Server 持续返回 audio frames
```

---

## 🎯 关键代码位置

### 1. 模式定义

**文件:** `mode.go`

```go
const (
    ModePipeline = "pipeline"
    ModeRealtime = "realtime"
)

func normalizeMode(m string) string {
    m = strings.ToLower(strings.TrimSpace(m))
    switch m {
    case ModeRealtime, "omni", "multimodal":
        return ModeRealtime
    default:
        return ModePipeline
    }
}
```

**说明:**
- 支持别名: "omni", "multimodal" → realtime
- 默认为 pipeline
- 大小写不敏感

### 2. 服务器配置

**文件:** `server.go`

```go
type ServerConfig struct {
    Mode                  string                    // ← 模式选择
    SessionFactory        transport.SessionFactory  // Pipeline 必需
    RealtimeFactory       RealtimeAgentFactory      // Realtime 必需
    DialogWSURL           string                    // Pipeline 必需
    CallIDPrefix          string
    ResolveDevicePayload  func(ctx, deviceID) ([]byte, error)
    ConfigureClient       func(*gateway.ClientConfig)
    OnSessionStart        func(ctx, callID, deviceID)
    OnSessionEnd          func(ctx, callID, reason)
}
```

**说明:**
- `Mode` 是全局配置，影响所有会话
- 不同模式需要不同的工厂

### 3. 会话初始化

**文件:** `session.go`

```go
type wsSession struct {
    cfg       ServerConfig      // ← 继承服务器配置
    // ...
    inFormat, outFormat string  // 音频格式
    inSR, outSR         int     // 采样率
    opusDec, opusEnc    media.EncoderFunc  // 编解码器
    
    // Pipeline 模式
    voiceSess *dialog.Session
    gw        *gateway.Client
    
    // Realtime 模式
    rtAgent   realtime.Agent
    rtOut     *realtimeOutPacer
}
```

**说明:**
- 从 ServerConfig 继承 Mode
- 根据 Mode 初始化不同的资源

### 4. Hello 消息处理

**文件:** `protocol.go` + `session.go`

```go
type HelloMessage struct {
    Type        string
    Version     int
    Transport   string
    Features    map[string]interface{}
    AudioParams *AudioParams  // ← 客户端指定的音频参数
}

type AudioParams struct {
    Format        string  // "opus" 或 "pcm"
    SampleRate    int     // 16000, 8000, 等
    Channels      int     // 通常为 1
    FrameDuration int     // 20, 60, 等 (毫秒)
    BitDepth      int     // 16
}
```

**说明:**
- 客户端在 hello 消息中指定音频参数
- 服务器协商并确认参数
- 不在 hello 中指定模式

---

## 🔍 当前实现的问题分析

### 问题 1: 模式选择不灵活

**现状:**
```
ServerConfig.Mode (全局) → 所有会话使用同一模式
```

**问题:**
- 不支持单个会话指定模式
- 不支持动态切换
- 客户端无法选择

**改进方案:**

#### 方案 A: 在 hello 消息中添加 mode 字段

```go
type HelloMessage struct {
    Type        string
    Version     int
    Transport   string
    Features    map[string]interface{}
    AudioParams *AudioParams
    Mode        string  // ← 新增: "pipeline" 或 "realtime"
}
```

**处理逻辑:**
```go
func (s *wsSession) handleHello(ctx context.Context, raw []byte) {
    var msg HelloMessage
    json.Unmarshal(raw, &msg)
    
    // 1. 协商音频参数 (同上)
    // ...
    
    // 2. 确定模式优先级
    mode := msg.Mode  // 客户端指定的模式
    if mode == "" {
        mode = s.cfg.Mode  // 使用服务器默认模式
    }
    
    // 3. 验证模式可用性
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

**缺点:**
- ❌ 需要修改协议
- ❌ 服务器需要同时支持两种模式

#### 方案 B: 通过 URL 查询参数指定

```go
// server.go - Handle()
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
    // ...
    
    // 从 URL 查询参数提取模式
    mode := strings.TrimSpace(r.URL.Query().Get("mode"))
    if mode == "" {
        mode = s.cfg.Mode  // 使用服务器默认模式
    }
    
    cfg := s.cfg
    cfg.Mode = mode  // 覆盖配置
    
    sess := newSession(cfg, conn, callID, deviceID)
    sess.run(r.Context())
}
```

**使用方式:**
```
ws://server:8080/ws?device_id=xxx&mode=realtime
```

**优点:**
- ✅ 不需要修改协议
- ✅ 在连接时就确定模式
- ✅ 服务器可以验证模式可用性

**缺点:**
- ❌ 需要修改 server.Handle()
- ❌ 模式在 hello 之前就确定

#### 方案 C: 通过 HTTP 请求头指定

```go
// server.go - Handle()
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
    // ...
    
    // 从请求头提取模式
    mode := strings.TrimSpace(r.Header.Get("X-Voice-Mode"))
    if mode == "" {
        mode = s.cfg.Mode
    }
    
    cfg := s.cfg
    cfg.Mode = mode
    
    sess := newSession(cfg, conn, callID, deviceID)
    sess.run(r.Context())
}
```

**使用方式:**
```
GET /ws?device_id=xxx
X-Voice-Mode: realtime
```

**优点:**
- ✅ 不需要修改协议
- ✅ 标准 HTTP 头方式
- ✅ 灵活性好

**缺点:**
- ❌ 需要修改 server.Handle()

---

## 📋 完整的实现流程总结

### 当前流程 (现有实现)

```
1. 服务器启动
   └─ NewServer(cfg)
      └─ cfg.Mode = "pipeline" 或 "realtime" (全局)

2. 客户端连接
   └─ GET /ws?device_id=xxx
      └─ server.Handle()
         └─ 创建 wsSession(cfg)
            └─ cfg.Mode 继承自服务器

3. 客户端发送 hello
   └─ Device: {"type":"hello","audio_params":{...}}
      └─ Server: handleHello()
         ├─ 解析 audio_params
         ├─ 初始化编解码器
         ├─ 检查 cfg.Mode
         └─ 调用 handleHelloRealtime() 或 handleHelloPipeline()

4. 对话处理
   └─ 根据选择的模式进行处理
```

### 改进后的流程 (推荐方案 A)

```
1. 服务器启动
   └─ NewServer(cfg)
      └─ cfg.Mode = "pipeline" 或 "realtime" (默认)

2. 客户端连接
   └─ GET /ws?device_id=xxx
      └─ server.Handle()
         └─ 创建 wsSession(cfg)

3. 客户端发送 hello
   └─ Device: {
        "type":"hello",
        "mode":"realtime",  // ← 新增
        "audio_params":{...}
      }
      └─ Server: handleHello()
         ├─ 解析 audio_params
         ├─ 解析 mode (优先级: 客户端 > 服务器默认)
         ├─ 初始化编解码器
         ├─ 验证模式可用性
         └─ 调用 handleHelloRealtime() 或 handleHelloPipeline()

4. 对话处理
   └─ 根据协商的模式进行处理
```

---

## 🎓 总结

### 当前实现的选择机制

| 层级 | 位置 | 来源 | 时机 | 灵活性 |
|------|------|------|------|--------|
| **第一层** | `NewServer()` | `ServerConfig.Mode` | 服务器启动 | 低 (全局) |
| **第二层** | `handleHello()` | `s.cfg.Mode` (继承) | 会话初始化 | 低 (无法覆盖) |

### 音频参数协商流程

```
1. 客户端在 hello 消息中指定 audio_params
2. 服务器解析并合并默认参数
3. 服务器初始化编解码器
4. 服务器确认参数并响应 hello
5. 后续音频使用协商的参数
```

### 编解码器初始化时机

```
在 handleHello() 中，模式选择之前
├─ 如果 format == "opus"
│  ├─ 创建 Opus 解码器 (opus → pcm)
│  └─ 创建 Opus 编码器 (pcm → opus)
└─ 如果 format == "pcm"
   └─ 跳过编解码器初始化
```

### 模式选择时机

```
在 handleHello() 中，编解码器初始化之后
├─ normalizeMode(s.cfg.Mode)
├─ 如果是 Realtime
│  └─ 调用 handleHelloRealtime()
└─ 否则 (Pipeline)
   └─ 调用 handleHelloPipeline()
```

---

## 💡 建议

如果你想支持客户端指定模式，建议采用 **方案 A** (在 hello 消息中添加 mode 字段)，这样：

1. ✅ 保持协议的一致性
2. ✅ 支持客户端灵活选择
3. ✅ 向后兼容 (mode 为空时使用服务器默认)
4. ✅ 在 hello 消息中完整协商所有参数 (audio_params + mode)

需要我帮你实现这个改进吗？
