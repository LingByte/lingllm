# 降噪模块集成指南

## 📋 概述

本指南说明如何将新的 CGO 降噪模块与现有的 voiceprint 和其他音频处理模块集成。

## 🏗️ 架构设计

```
┌─────────────────────────────────────────────┐
│         Audio Input Stream                  │
└──────────────────┬──────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────┐
│    Denoise Module (CGO)                     │
│  ┌──────────────────────────────────────┐   │
│  │ AEC (Echo Cancellation)              │   │
│  │ AGC (Automatic Gain Control)         │   │
│  └──────────────────────────────────────┘   │
└──────────────────┬──────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────┐
│    Voiceprint Module                        │
│  ┌──────────────────────────────────────┐   │
│  │ HTTP / Xunfei / Volcengine Provider  │   │
│  │ Feature Creation/Matching             │   │
│  └──────────────────────────────────────┘   │
└──────────────────┬──────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────┐
│    Output / Storage                         │
└─────────────────────────────────────────────┘
```

## 🔧 集成步骤

### 1. 导入降噪模块

```go
import (
    "github.com/LingByte/lingllm/denoise"
    "github.com/LingByte/lingllm/voiceprint"
)
```

### 2. 创建降噪处理器

```go
// 创建降噪处理器实例
denoiser, err := denoise.NewDenoiseProcessor(&denoise.DenoiseConfig{
    AECEnable:     true,
    AGCEnable:     true,
    SampleRate:    16000,
    Channels:      1,
    BitsPerSample: 16,
})
if err != nil {
    log.Fatal("Failed to create denoiser:", err)
}
defer denoiser.Close()
```

### 3. 创建声纹提供商

```go
// 创建声纹提供商
factory := voiceprint.NewFactory()
provider, err := factory.CreateProvider(&voiceprint.ProviderConfig{
    Provider: voiceprint.ProviderVolcengine,
    Options: map[string]interface{}{
        "access_key": os.Getenv("VOLCENGINE_ACCESS_KEY"),
        "secret_key": os.Getenv("VOLCENGINE_SECRET_KEY"),
    },
})
if err != nil {
    log.Fatal("Failed to create provider:", err)
}
defer provider.Close()
```

### 4. 处理音频流

```go
// 读取原始音频数据
rawAudio := readAudioFile("input.wav")

// 应用降噪处理
denoisedAudio, err := denoiser.Process(rawAudio)
if err != nil {
    log.Fatal("Denoise failed:", err)
}

// 创建声纹特征
ctx := context.Background()
result, err := provider.CreateFeature(
    ctx,
    "group-001",           // 特征库ID
    "speaker-001",         // 特征ID
    "Speaker metadata",    // 元信息
    denoisedAudio,         // 降噪后的音频
)
if err != nil {
    log.Fatal("CreateFeature failed:", err)
}

log.Printf("Feature created: %+v", result)
```

## 📊 完整示例

### 声纹注册流程

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/LingByte/lingllm/denoise"
    "github.com/LingByte/lingllm/voiceprint"
)

func main() {
    // 1. 初始化降噪处理器
    denoiser, err := denoise.NewDenoiseProcessor(&denoise.DenoiseConfig{
        AECEnable:     true,
        AGCEnable:     true,
        SampleRate:    16000,
        Channels:      1,
        BitsPerSample: 16,
    })
    if err != nil {
        log.Fatal("Failed to create denoiser:", err)
    }
    defer denoiser.Close()

    // 2. 初始化声纹提供商
    factory := voiceprint.NewFactory()
    provider, err := factory.CreateProvider(&voiceprint.ProviderConfig{
        Provider: voiceprint.ProviderVolcengine,
        Options: map[string]interface{}{
            "access_key": os.Getenv("VOLCENGINE_ACCESS_KEY"),
            "secret_key": os.Getenv("VOLCENGINE_SECRET_KEY"),
        },
    })
    if err != nil {
        log.Fatal("Failed to create provider:", err)
    }
    defer provider.Close()

    // 3. 读取音频文件
    audioData, err := os.ReadFile("speaker_voice.wav")
    if err != nil {
        log.Fatal("Failed to read audio file:", err)
    }

    // 4. 应用降噪处理
    denoisedAudio, err := denoiser.Process(audioData)
    if err != nil {
        log.Fatal("Denoise failed:", err)
    }

    // 5. 创建声纹特征
    ctx := context.Background()
    result, err := provider.CreateFeature(
        ctx,
        "speakers-group",
        "alice-voice",
        "Alice's voice sample",
        denoisedAudio,
    )
    if err != nil {
        log.Fatal("CreateFeature failed:", err)
    }

    log.Printf("✓ Voice print registered: %+v", result)
}
```

### 声纹验证流程

```go
func verifyVoiceprint(
    ctx context.Context,
    denoiser *denoise.DenoiseProcessor,
    provider voiceprint.VoiceprintProvider,
    testAudio []byte,
) error {
    // 1. 降噪处理
    denoisedAudio, err := denoiser.Process(testAudio)
    if err != nil {
        return fmt.Errorf("denoise failed: %w", err)
    }

    // 2. 1:1 比对 (与特定特征比对)
    scoreResult, err := provider.SearchScoreFea(
        ctx,
        "speakers-group",
        "alice-voice",
        denoisedAudio,
    )
    if err != nil {
        return fmt.Errorf("search score failed: %w", err)
    }

    log.Printf("Match score: %.2f", scoreResult.Score)

    // 3. 1:N 比对 (与所有特征比对)
    searchResult, err := provider.SearchFea(
        ctx,
        "speakers-group",
        5,  // 返回前5个匹配
        denoisedAudio,
    )
    if err != nil {
        return fmt.Errorf("search failed: %w", err)
    }

    log.Printf("Found %d matches", len(searchResult.ScoreList))
    return nil
}
```

## 🔌 与其他模块的集成

### 与 VAD (Voice Activity Detection) 集成

```go
// 在 voiceprint 模块中添加 VAD 支持
type AudioProcessor struct {
    denoiser *denoise.DenoiseProcessor
    vad      *vad.VADProcessor
    provider voiceprint.VoiceprintProvider
}

func (ap *AudioProcessor) ProcessAudioStream(audioStream <-chan []byte) {
    for chunk := range audioStream {
        // 1. 降噪
        denoised, _ := ap.denoiser.Process(chunk)

        // 2. VAD 检测
        isVoice, _ := ap.vad.Detect(denoised)
        if !isVoice {
            continue
        }

        // 3. 声纹处理
        feature, _ := ap.provider.CreateFeature(
            context.Background(),
            "group-1",
            "feature-1",
            "metadata",
            denoised,
        )
        log.Printf("Feature created: %+v", feature)
    }
}
```

### 与 Media 模块集成

```go
// 在 media 模块中使用降噪
func (m *MediaProcessor) ProcessAudio(input *AudioFrame) (*AudioFrame, error) {
    // 1. 解码音频
    pcmData := m.decoder.Decode(input.Data)

    // 2. 应用降噪
    denoised, err := m.denoiser.Process(pcmData)
    if err != nil {
        return nil, err
    }

    // 3. 编码输出
    output := m.encoder.Encode(denoised)

    return &AudioFrame{
        Data:       output,
        SampleRate: input.SampleRate,
        Channels:   input.Channels,
    }, nil
}
```

## ⚙️ 配置建议

### 不同场景的推荐配置

#### 1. 实时通话场景
```go
&denoise.DenoiseConfig{
    AECEnable:     true,   // 启用回声消除
    AGCEnable:     true,   // 启用增益控制
    SampleRate:    16000,  // 宽带
    Channels:      1,      // 单声道
    BitsPerSample: 16,
}
```

#### 2. 离线处理场景
```go
&denoise.DenoiseConfig{
    AECEnable:     true,   // 启用回声消除
    AGCEnable:     true,   // 启用增益控制
    SampleRate:    16000,  // 可根据需要调整
    Channels:      2,      // 可支持立体声
    BitsPerSample: 16,
}
```

#### 3. 低延迟场景
```go
&denoise.DenoiseConfig{
    AECEnable:     false,  // 禁用AEC以降低延迟
    AGCEnable:     true,   // 保留AGC
    SampleRate:    16000,
    Channels:      1,
    BitsPerSample: 16,
}
```

## 📈 性能优化

### 1. 内存优化

```go
// 使用原地处理以节省内存
err := denoiser.ProcessInPlace(audioData)
// 而不是
// denoised, _ := denoiser.Process(audioData)
```

### 2. 批量处理

```go
// 批量处理多个音频块
for _, chunk := range audioChunks {
    denoised, _ := denoiser.Process(chunk)
    processFeature(denoised)
}
```

### 3. 并发处理

```go
// 使用多个处理器实例进行并发处理
denoisers := make([]*denoise.DenoiseProcessor, numWorkers)
for i := 0; i < numWorkers; i++ {
    denoisers[i], _ = denoise.NewDenoiseProcessor(config)
    defer denoisers[i].Close()
}

// 分发任务
for i, chunk := range audioChunks {
    go func(idx int, data []byte) {
        denoised, _ := denoisers[idx%numWorkers].Process(data)
        processFeature(denoised)
    }(i, chunk)
}
```

## 🧪 测试

### 单元测试

```bash
cd denoise
go test ./... -v
```

### 集成测试

```bash
cd voiceprint
go test ./... -v
```

### 基准测试

```bash
cd denoise
go test -bench=. -benchmem
```

## 🐛 故障排除

### 问题 1: CGO 编译失败

**症状**: `cgo: cannot find gcc`

**解决方案**:
```bash
# macOS
brew install gcc

# Ubuntu/Debian
sudo apt-get install build-essential

# CentOS/RHEL
sudo yum install gcc
```

### 问题 2: 降噪效果不明显

**症状**: 处理后的音频仍有噪音

**解决方案**:
1. 确保 AEC 和 AGC 都已启用
2. 检查输入音频格式是否正确 (16-bit PCM)
3. 尝试调整采样率和声道配置

### 问题 3: 内存泄漏

**症状**: 长时间运行后内存占用持续增长

**解决方案**:
1. 确保调用 `Close()` 释放资源
2. 使用 `defer` 确保清理
3. 检查是否有循环引用

## 📚 相关文档

- [Denoise Module README](./denoise/README.md)
- [Voiceprint Module Documentation](./voiceprint/PROVIDERS.md)
- [ConversationalAI Scan Report](./VOICEPRINT_SCAN_REPORT.md)

## 🔗 参考资源

- [Espressif ESP-AFE](https://github.com/espressif/esp-adf)
- [WebRTC Audio Processing](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/audio_processing/)
- [Go CGO Documentation](https://golang.org/cmd/cgo/)

## 📝 更新日志

### v1.0.0 (2026-06-08)
- ✅ 初始发布
- ✅ AEC 实现
- ✅ AGC 实现
- ✅ 完整的 API 和文档

## 许可证

AGPL-3.0
