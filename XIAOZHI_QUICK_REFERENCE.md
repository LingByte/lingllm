# Xiaozhi Protocol 快速参考

## 🚀 快速开始

### 1. 创建服务器

```go
import "github.com/LingByte/lingllm/protocol/voice/xiaozhi"

// Pipeline 模式
server, err := xiaozhi.NewServer(xiaozhi.ServerConfig{
    Mode:           "pipeline",
    SessionFactory: yourSessionFactory,
    DialogWSURL:    "ws://dialog-server:8080/dialog",
    CallIDPrefix:   "xz",
    OnSessionStart: func(ctx context.Context, callID, deviceID string) {
        log.Printf("Session started: %s", callID)
    },
    OnSessionEnd: func(ctx context.Context, callID, reason string) {
        log.Printf("Session ended: %s, reason: %s", callID, reason)
    },
})

// HTTP 路由
http.HandleFunc("/ws", server.Handle)
http.ListenAndServe(":8080", nil)
```

### 2. 客户端连接

```javascript
// JavaScript/WebSocket
const ws = new WebSocket('ws://server:8080/ws?device_id=device-123');

// 发送 hello
ws.send(JSON.stringify({
    type: 'hello',
    audio_params: {
        format: 'opus',
        sample_rate: 16000,
        channels: 1,
        frame_duration: 60,
        bit_depth: 16
    }
}));

// 接收 hello 响应
ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === 'hello') {
        console.log('Connected, session:', msg.session_id);
    }
};
```

---

## 📨 消息速查表

### 客户端消息

| 消息 | 用途 | 示例 |
|------|------|------|
| **hello** | 初始化连接 | `{"type":"hello","audio_params":{...}}` |
| **listen** | 控制麦克风 | `{"type":"listen","state":"start"}` |
| **abort** | 中止操作 | `{"type":"abort"}` |
| **ping** | 心跳 | `{"type":"ping"}` |
| **[binary]** | 音频帧 | 二进制数据 |

### 服务器消息

| 消息 | 用途 | 示例 |
|------|------|------|
| **hello** | 连接确认 | `{"type":"hello","session_id":"xz-123",...}` |
| **stt** | 识别结果 | `{"type":"stt","text":"你好"}` |
| **tts** | TTS 状态 | `{"type":"tts","state":"start"}` |
| **llm_response** | LLM 回复 | `{"type":"llm_response","text":"..."}` |
| **pong** | 心跳响应 | `{"type":"pong","session_id":"xz-123"}` |
| **abort** | 中止确认 | `{"type":"abort","state":"confirmed"}` |
| **error** | 错误信息 | `{"type":"error","message":"...","fatal":true}` |
| **[binary]** | 音频帧 | 二进制数据 |

---

## 🎯 工作流程对比

### Pipeline 模式 (默认)

```
设备 → hello → 服务器
        ↓
      初始化
        ↓
设备 → listen:start
        ↓
设备 → [audio] → ASR → Dialog → TTS → [audio] → 设备
        ↓
      stt 结果
        ↓
      tts:start
        ↓
      [audio frames]
        ↓
      tts:stop
        ↓
      llm_response
```

**特点:**
- ✅ 顺序处理
- ✅ 支持 abort 中止
- ✅ 中等延迟
- ✅ 简单易用

### Realtime 模式

```
设备 → hello → 服务器
        ↓
      初始化 Realtime Agent
        ↓
设备 → listen:start
        ↓
设备 ⇄ [audio stream] ⇄ Agent
        ↓
      stt (final)
        ↓
      llm_response (流式)
        ↓
      tts:start
        ↓
      [audio stream]
        ↓
设备 → [audio] (barge-in)
        ↓
      新 stt
        ↓
      新 llm_response
```

**特点:**
- ✅ 全双工通信
- ✅ 支持 barge-in
- ✅ 极低延迟
- ✅ 复杂度高

---

## 🔧 常见操作

### 获取设备 ID

```go
// 从 HTTP 请求头
deviceID := r.Header.Get("Device-Id")

// 从 URL 查询参数
deviceID := r.URL.Query().Get("device_id")

// 从 WebSocket 连接
// (自动从请求中提取)
```

### 自定义音频参数

```go
// 客户端
ws.send(JSON.stringify({
    type: 'hello',
    audio_params: {
        format: 'pcm',           // 改为 PCM
        sample_rate: 8000,       // 改为 8kHz
        channels: 1,
        frame_duration: 20,      // 改为 20ms
        bit_depth: 16
    }
}));
```

### 处理错误

```go
// 致命错误 (会关闭会话)
MakeError("opus decoder unavailable", true)

// 非致命错误 (继续运行)
MakeError("audio processing failed", false)
```

### 中止操作

```javascript
// 客户端发送 abort
ws.send(JSON.stringify({ type: 'abort' }));

// 服务器响应
// {"type":"abort","state":"confirmed","session_id":"xz-123"}
```

---

## 📊 音频参数预设

### 高质量 (推荐)

```json
{
  "format": "opus",
  "sample_rate": 16000,
  "channels": 1,
  "frame_duration": 60,
  "bit_depth": 16
}
```

### 低延迟

```json
{
  "format": "opus",
  "sample_rate": 16000,
  "channels": 1,
  "frame_duration": 20,
  "bit_depth": 16
}
```

### 最小带宽

```json
{
  "format": "opus",
  "sample_rate": 8000,
  "channels": 1,
  "frame_duration": 60,
  "bit_depth": 16
}
```

### 原始音频 (无压缩)

```json
{
  "format": "pcm",
  "sample_rate": 16000,
  "channels": 1,
  "frame_duration": 20,
  "bit_depth": 16
}
```

---

## ⚙️ 配置选项

### ServerConfig

```go
type ServerConfig struct {
    // 工作模式: "pipeline" (默认) 或 "realtime"
    Mode string
    
    // Pipeline 模式必需
    SessionFactory  transport.SessionFactory
    DialogWSURL     string
    
    // Realtime 模式必需
    RealtimeFactory RealtimeAgentFactory
    
    // 可选
    CallIDPrefix         string
    ResolveDevicePayload func(ctx, deviceID) ([]byte, error)
    ConfigureClient      func(*gateway.ClientConfig)
    OnSessionStart       func(ctx, callID, deviceID)
    OnSessionEnd         func(ctx, callID, reason)
}
```

---

## 🔍 调试技巧

### 启用日志

```go
import "github.com/sirupsen/logrus"

logrus.SetLevel(logrus.DebugLevel)
```

### 监控会话

```go
OnSessionStart: func(ctx context.Context, callID, deviceID string) {
    log.Printf("[START] callID=%s deviceID=%s", callID, deviceID)
},
OnSessionEnd: func(ctx context.Context, callID, reason string) {
    log.Printf("[END] callID=%s reason=%s", callID, reason)
},
```

### 测试连接

```bash
# 使用 websocat 测试
websocat ws://localhost:8080/ws?device_id=test-device

# 发送 hello
{"type":"hello","audio_params":{"format":"pcm","sample_rate":16000,"channels":1,"frame_duration":20,"bit_depth":16}}

# 发送 listen
{"type":"listen","state":"start"}

# 发送 ping
{"type":"ping"}
```

---

## 📈 性能指标

| 指标 | 值 |
|------|-----|
| 读缓冲 | 4KB |
| 写缓冲 | 16KB |
| 读超时 | 5 分钟 |
| 写超时 | 5 秒 |
| 最大并发会话 | 无限制 (受系统资源) |

---

## 🚨 常见问题

### Q: 如何选择 Pipeline 还是 Realtime?

**Pipeline:**
- 标准对话、问答
- 需要完整的 LLM 回复
- 延迟要求不高

**Realtime:**
- 实时交互、语音助手
- 需要低延迟
- 支持用户打断

### Q: Opus 和 PCM 怎么选?

**Opus:**
- 网络传输 (带宽受限)
- 移动设备
- 需要压缩

**PCM:**
- 本地处理
- 高质量要求
- 低延迟

### Q: 如何处理网络中断?

```javascript
ws.onclose = () => {
    console.log('Connection closed');
    // 重新连接
    setTimeout(() => {
        ws = new WebSocket('ws://...');
    }, 1000);
};

ws.onerror = (error) => {
    console.error('WebSocket error:', error);
};
```

### Q: 如何实现 barge-in?

Realtime 模式自动支持，只需:
1. 设置 `Mode: "realtime"`
2. 提供 `RealtimeFactory`
3. 客户端继续发送音频

---

## 📚 相关文件

| 文件 | 说明 |
|------|------|
| `protocol.go` | 协议定义和辅助函数 |
| `session.go` | 会话管理 |
| `server.go` | 服务器实现 |
| `mode.go` | 模式定义 |
| `realtime_session.go` | Realtime 模式处理 |
| `realtime_pacer.go` | 输出节流器 |

---

## 🎓 学习资源

1. 完整文档: `XIAOZHI_PROTOCOL.md`
2. 源代码: `/protocol/voice/xiaozhi/`
3. 测试: `*_test.go` 文件
4. 示例: `/examples/` 目录

---

**最后更新:** 2026-06-07
**版本:** 1.0
