# Xiaozhi Voice Protocol 详细文档

## 📋 目录
1. [协议概览](#协议概览)
2. [消息格式](#消息格式)
3. [Pipeline 模式](#pipeline-模式)
4. [Realtime 模式](#realtime-模式)
5. [会话管理](#会话管理)
6. [编码转换](#编码转换)
7. [错误处理](#错误处理)

---

## 协议概览

### 架构图
```
┌──────────────────────────────────────────────────────────────┐
│                     Xiaozhi Voice Protocol                    │
└──────────────────────────────────────────────────────────────┘

Device/Browser
    │
    │ WebSocket (xiaozhi)
    │ ├─ Text: JSON 控制消息
    │ └─ Binary: 音频帧
    ▼
┌─────────────────────────────────────────┐
│      Xiaozhi Server (session.go)         │
│  ┌─────────────────────────────────────┐ │
│  │ 1. 音频参数协商 (hello)              │ │
│  │ 2. 编解码器初始化                    │ │
│  │ 3. 模式选择 (Pipeline/Realtime)     │ │
│  │ 4. 消息路由和处理                    │ │
│  └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
    │
    ├─ Pipeline Mode ──────────────────┐
    │                                  │
    │  WebSocket (dialog)              │
    │  ├─ Dialog Session               │
    │  └─ Gateway Client               │
    │      ├─ ASR (Recognizer)         │
    │      ├─ LLM (Dialog)             │
    │      └─ TTS (Synthesizer)        │
    │                                  │
    └──────────────────────────────────┘
    │
    └─ Realtime Mode ──────────────────┐
       │                               │
       │ Realtime Agent                │
       │ (Qwen-Omni, etc.)             │
       │ ├─ Streaming ASR              │
       │ ├─ Streaming LLM              │
       │ └─ Streaming TTS              │
       │                               │
       └───────────────────────────────┘
```

---

## 消息格式

### 1. Hello 消息 (初始化)

**客户端发送:**
```json
{
  "type": "hello",
  "version": 1,
  "transport": "websocket",
  "audio_params": {
    "format": "opus",
    "sample_rate": 16000,
    "channels": 1,
    "frame_duration": 60,
    "bit_depth": 16
  }
}
```

**服务器响应:**
```json
{
  "type": "hello",
  "version": 1,
  "transport": "websocket",
  "session_id": "xz-1717756800000000000",
  "audio_params": {
    "format": "opus",
    "sample_rate": 16000,
    "channels": 1,
    "frame_duration": 60,
    "bit_depth": 16
  }
}
```

### 2. Listen 消息 (麦克风控制)

**客户端发送:**
```json
{
  "type": "listen",
  "state": "start",
  "mode": "detect"
}
```

**状态值:**
- `start` - 开始录音
- `stop` - 停止录音
- `detect` - VAD 检测模式

### 3. STT 消息 (语音识别结果)

**服务器发送:**
```json
{
  "type": "stt",
  "text": "你好，请播放音乐",
  "session_id": "xz-1717756800000000000"
}
```

### 4. TTS 消息 (文本转语音状态)

**服务器发送:**
```json
{
  "type": "tts",
  "state": "start",
  "session_id": "xz-1717756800000000000",
  "audio_params": {
    "codec": "opus",
    "sample_rate": 16000,
    "channels": 1,
    "frame_duration": 60,
    "bit_depth": 16
  }
}
```

**状态值:**
- `start` - TTS 开始，后续为二进制音频帧
- `stop` - TTS 结束

### 5. LLM 响应消息

**服务器发送:**
```json
{
  "type": "llm_response",
  "text": "好的，为您播放最新的流行音乐。"
}
```

### 6. 控制消息

**Abort (中止):**
```json
{
  "type": "abort"
}
```

**Ping (心跳):**
```json
{
  "type": "ping"
}
```

**Pong (心跳响应):**
```json
{
  "type": "pong",
  "session_id": "xz-1717756800000000000"
}
```

**Error (错误):**
```json
{
  "type": "error",
  "message": "opus decoder unavailable",
  "fatal": true
}
```

---

## Pipeline 模式

### 工作流程

```
┌─────────────────────────────────────────────────────────────┐
│                    Pipeline Mode Flow                        │
└─────────────────────────────────────────────────────────────┘

时间轴 ─────────────────────────────────────────────────────────>

设备端                      服务器端
  │                           │
  │ 1. hello ────────────────>│
  │                           │ 初始化会话
  │                           │ 创建 Dialog Session
  │                           │ 创建 Gateway Client
  │                           │
  │<────────── hello ─────────│ 确认参数
  │                           │
  │ 2. listen:start ────────>│
  │                           │
  │ 3. [audio frames] ──────>│
  │    (opus/pcm)            │ 解码音频
  │                           │ 发送到 Dialog
  │                           │ ASR 处理
  │                           │
  │<─────────── stt ─────────│ 识别结果
  │    "你好"                 │
  │                           │
  │                           │ Dialog 处理
  │                           │ LLM 生成回复
  │                           │
  │<─────── tts:start ───────│ TTS 开始
  │                           │
  │<─── [audio frames] ──────│ 合成音频
  │    (opus/pcm)            │ (编码)
  │                           │
  │<─────── tts:stop ────────│ TTS 结束
  │                           │
  │<── llm_response ─────────│ 完整回复文本
  │    "你好，有什么..."      │
  │                           │
  │ 4. listen:stop ────────>│
  │                           │
  │ 5. [close] ────────────>│
  │                           │
```

### 关键特点

| 特点 | 说明 |
|------|------|
| **处理顺序** | ASR → Dialog → TTS (顺序执行) |
| **延迟** | 中等 (需要等待 LLM 响应) |
| **中断支持** | ✅ abort 可打断 TTS |
| **适用场景** | 标准对话、问答、指令执行 |
| **编码灵活性** | 支持 opus/pcm 自由选择 |

### 状态机

```
┌─────────────┐
│   IDLE      │ (初始状态)
└──────┬──────┘
       │ hello
       ▼
┌─────────────┐
│  CONNECTED  │ (已连接，等待命令)
└──────┬──────┘
       │ listen:start
       ▼
┌─────────────┐
│ LISTENING   │ (监听中)
└──────┬──────┘
       │ audio frames
       ▼
┌─────────────┐
│   ASR       │ (识别中)
└──────┬──────┘
       │ stt result
       ▼
┌─────────────┐
│  DIALOG     │ (对话处理中)
└──────┬──────┘
       │ tts:start
       ▼
┌─────────────┐
│    TTS      │ (合成中)
└──────┬──────┘
       │ tts:stop
       ▼
┌─────────────┐
│ LISTENING   │ (回到监听)
└──────┬──────┘
       │ listen:stop
       ▼
┌─────────────┐
│   IDLE      │ (关闭)
└─────────────┘
```

---

## Realtime 模式

### 工作流程

```
┌─────────────────────────────────────────────────────────────┐
│                   Realtime Mode Flow                         │
└─────────────────────────────────────────────────────────────┘

时间轴 ─────────────────────────────────────────────────────────>

设备端                      服务器端
  │                           │
  │ 1. hello ────────────────>│
  │                           │ 初始化 Realtime Agent
  │                           │ (Qwen-Omni, etc.)
  │                           │
  │<────────── hello ─────────│
  │                           │
  │ 2. listen:start ────────>│
  │                           │
  │ 3. [audio] ─────────────>│ 实时推送
  │    (持续流)               │ Agent 处理
  │                           │
  │<─────────── stt ─────────│ 识别结果
  │    "播放音乐"             │ (final=true)
  │                           │
  │<── llm_response ─────────│ LLM 回复
  │    "好的，为您..."        │ (流式)
  │                           │
  │<─────── tts:start ───────│ TTS 开始
  │                           │
  │<─── [audio] ────────────│ 合成音频
  │    (持续流)               │ (流式)
  │                           │
  │ 4. [audio] ─────────────>│ 用户打断
  │    (用户说话)             │ (Barge-in)
  │                           │
  │                           │ 检测到用户语音
  │                           │ 中止 TTS
  │                           │
  │<─────────── stt ─────────│ 新的识别结果
  │    "不，播放摇滚"         │
  │                           │
  │<── llm_response ─────────│ 新的 LLM 回复
  │    "好的，播放摇滚..."    │
  │                           │
  │<─────── tts:start ───────│ 重新开始 TTS
  │                           │
  │<─── [audio] ────────────│ 新的合成音频
  │                           │
  │<─────── tts:stop ────────│ TTS 结束
  │                           │
  │ 5. listen:stop ────────>│
  │                           │
```

### Barge-in 机制

```
用户说话 ──> VAD 检测 ──> 中止 TTS ──> 新的 STT ──> 新的 LLM ──> 新的 TTS
                                                                    ↑
                                                        (重新开始播放)
```

### 关键特点

| 特点 | 说明 |
|------|------|
| **通信模式** | 全双工 (同时发送和接收) |
| **延迟** | 极低 (毫秒级) |
| **中断支持** | ✅ Barge-in (用户打断助手) |
| **适用场景** | 实时对话、多轮交互、语音助手 |
| **智能 VAD** | 服务端检测用户语音，自动中止 TTS |

### 状态管理

```go
listening   bool  // 麦克风是否开启
ttsActive   bool  // TTS 是否正在播放
rtReplyBusy bool  // 是否在处理 LLM 回复 (阻止上行音频)
```

**rtReplyBusy 的作用:**
- `true`: LLM 正在生成回复，阻止上行音频 (防止噪音)
- `false`: 可以接收上行音频 (支持 barge-in)

---

## 会话管理

### 会话生命周期

```go
type wsSession struct {
    cfg       ServerConfig      // 配置
    conn      *websocket.Conn   // WebSocket 连接
    callID    string            // 会话 ID
    sessionID string            // 会话 ID (同 callID)
    deviceID  string            // 设备 ID
    
    // 音频配置
    inFormat, outFormat string  // 输入/输出格式 (opus/pcm)
    inSR, outSR         int     // 输入/输出采样率
    inFrameMs           int     // 输入帧时长
    ttsWireFrameMs      int     // TTS 传输帧时长
    
    // 编解码器
    opusDec media.EncoderFunc   // Opus 解码器
    opusEnc media.EncoderFunc   // Opus 编码器
    
    // Pipeline 模式
    voiceSess *dialog.Session   // Dialog 会话
    gw        *gateway.Client   // Gateway 客户端
    
    // Realtime 模式
    rtAgent         realtime.Agent      // Realtime Agent
    rtOut           *realtimeOutPacer   // 输出节流器
    rtInSR, rtOutSR int                 // 采样率
    
    // 状态
    listening   atomic.Bool  // 监听状态
    ttsActive   atomic.Bool  // TTS 活跃状态
    rtReplyBusy atomic.Bool  // Realtime 回复忙状态
    turnLLMText string       // 当前轮次 LLM 文本
    
    writeMu sync.Mutex       // 写操作互斥锁
    closed  atomic.Bool      // 关闭标志
}
```

### 初始化流程 (handleHello)

```
1. 解析 hello 消息
   ├─ 提取 audio_params
   └─ 合并默认参数
   
2. 初始化编解码器
   ├─ 如果 format=opus
   │  ├─ 创建 Opus 解码器
   │  └─ 创建 Opus 编码器
   └─ 如果 format=pcm
      └─ 跳过编解码器初始化
      
3. 根据模式初始化
   ├─ Pipeline 模式
   │  ├─ 创建 Dialog Session
   │  ├─ 创建 Gateway Client
   │  └─ 启动 Gateway
   └─ Realtime 模式
      ├─ 创建 Realtime Agent
      └─ 初始化输出节流器
      
4. 发送 welcome 响应
   └─ 确认音频参数
```

### 消息处理流程 (run)

```
while not closed:
    读取 WebSocket 消息
    ├─ 文本消息
    │  └─ parseTextFrame()
    │     ├─ hello → handleHello()
    │     ├─ listen → handleListen()
    │     ├─ abort → handleAbort()
    │     └─ ping → 发送 pong
    └─ 二进制消息
       └─ handleAudio()
          ├─ 解码 (if opus)
          ├─ 检查状态 (listening, !ttsActive)
          └─ 发送到 Dialog/Realtime
```

### 清理流程 (teardown)

```
teardown(reason):
    1. 标记为已关闭 (closed = true)
    2. 关闭 Realtime 资源
       ├─ 关闭输出节流器
       └─ 关闭 Realtime Agent
    3. 关闭 Pipeline 资源
       ├─ 关闭 Gateway
       └─ 关闭 Dialog Session
    4. 关闭 WebSocket
       ├─ 发送 close 消息
       └─ 关闭连接
    5. 调用回调
       └─ OnSessionEnd(callID, reason)
```

---

## 编码转换

### 支持的格式

| 格式 | 优点 | 缺点 | 适用场景 |
|------|------|------|---------|
| **Opus** | 高压缩率、低延迟、高质量 | 需要编解码 | 网络传输、带宽受限 |
| **PCM** | 无损、低延迟 | 高带宽占用 | 本地处理、高质量 |

### 入站音频处理

```
Device Audio (opus/pcm)
    ↓
[if format=opus]
    opusDec: opus → pcm
    ↓
PCM 16kHz, 1 channel, 16-bit
    ↓
[检查状态]
    listening=true && ttsActive=false
    ↓
Dialog/Realtime 处理
    ├─ ASR 识别
    ├─ LLM 处理
    └─ TTS 合成
```

### 出站音频处理

```
Dialog/Realtime 输出
    ↓
PCM 16kHz, 1 channel, 16-bit
    ↓
[if format=opus]
    opusEnc: pcm → opus
    ↓
Device Audio (opus/pcm)
    ↓
WebSocket Binary 消息
```

### 采样率转换

```go
// 如果采样率不匹配，自动重采样
if inSR != outSR {
    resampled := ResamplePCM(pcm, inSR, outSR)
}
```

---

## 错误处理

### 致命错误 (fatal=true)

```json
{
  "type": "error",
  "message": "opus decoder unavailable",
  "fatal": true
}
```

**会导致会话立即关闭:**
- 编解码器初始化失败
- Dialog 连接失败
- Realtime 初始化失败
- 设备绑定失败

### 非致命错误 (fatal=false)

```json
{
  "type": "error",
  "message": "audio processing failed",
  "fatal": false
}
```

**不会关闭会话:**
- 单个音频帧处理失败
- 网络暂时中断
- 编码/解码错误

### 超时控制

```go
// 读超时: 5 分钟
conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

// 写超时: 5 秒
conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
```

---

## 性能优化

### 缓冲管理

```go
ReadBuffer:  4 * 1024      // 4KB
WriteBuffer: 16 * 1024     // 16KB
```

### 并发控制

```go
writeMu sync.Mutex         // 保护 WebSocket 写操作
listening   atomic.Bool    // 原子操作
ttsActive   atomic.Bool    // 原子操作
rtReplyBusy atomic.Bool    // 原子操作
```

### 单线程消息处理

- 所有消息在单个 goroutine 中处理
- 避免竞态条件
- 保证消息顺序

---

## 扩展点

### ServerConfig

```go
type ServerConfig struct {
    Mode                  string                                    // "pipeline" 或 "realtime"
    SessionFactory        transport.SessionFactory                  // Dialog Session 工厂
    RealtimeFactory       RealtimeAgentFactory                      // Realtime Agent 工厂
    DialogWSURL           string                                    // Dialog WebSocket 地址
    CallIDPrefix          string                                    // 会话 ID 前缀
    ResolveDevicePayload  func(ctx, deviceID) ([]byte, error)      // 设备绑定解析
    ConfigureClient       func(*gateway.ClientConfig)               // Gateway 配置
    OnSessionStart        func(ctx, callID, deviceID)               // 会话开始回调
    OnSessionEnd          func(ctx, callID, reason)                 // 会话结束回调
}
```

### 自定义回调

```go
// 会话开始
OnSessionStart: func(ctx context.Context, callID, deviceID string) {
    log.Printf("Session started: %s from %s", callID, deviceID)
}

// 会话结束
OnSessionEnd: func(ctx context.Context, callID, reason string) {
    log.Printf("Session ended: %s, reason: %s", callID, reason)
}

// Gateway 配置
ConfigureClient: func(cfg *gateway.ClientConfig) {
    cfg.OnASRFinal = func(text string) {
        // 自定义 ASR 处理
    }
}
```

---

## 总结

| 方面 | Pipeline | Realtime |
|------|----------|----------|
| **架构** | ASR → Dialog → TTS | 流式多模态 |
| **延迟** | 中等 (秒级) | 极低 (毫秒级) |
| **中断** | abort 中止 TTS | barge-in 打断助手 |
| **适用** | 标准对话 | 实时交互 |
| **复杂度** | 低 | 高 |
| **成本** | 低 | 高 |

选择哪种模式取决于你的应用场景:
- **Pipeline**: 适合问答、指令执行、标准对话
- **Realtime**: 适合语音助手、实时交互、多轮对话
