# Denoise Module

Go CGO 降噪模块，集成 AEC (Acoustic Echo Cancellation) 和 AGC (Automatic Gain Control) 功能。

## 功能特性

- ✅ **AEC (回声消除)** - 消除音频中的回声
- ✅ **AGC (自动增益控制)** - 自动调整音频增益
- ✅ **灵活配置** - 支持自定义采样率、声道数等
- ✅ **原地处理** - 支持原地处理音频数据
- ✅ **状态管理** - 支持重置处理器状态
- ✅ **动态控制** - 运行时启用/禁用各功能

## 安装

### 前置条件

- Go 1.16+
- GCC 或 Clang (用于编译 C 代码)
- CGO 支持

### 构建

```bash
cd denoise
go build ./...
```

## 使用示例

### 基础使用

```go
package main

import (
    "log"
    "github.com/LingByte/lingllm/denoise"
)

func main() {
    // 创建降噪处理器
    processor, err := denoise.NewDenoiseProcessor(&denoise.DenoiseConfig{
        AECEnable:     true,
        AGCEnable:     true,
        SampleRate:    16000,
        Channels:      1,
        BitsPerSample: 16,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer processor.Close()

    // 读取音频数据
    audioData := readAudioFile("input.wav")

    // 处理音频
    denoised, err := processor.Process(audioData)
    if err != nil {
        log.Fatal(err)
    }

    // 保存处理后的音频
    saveAudioFile("output.wav", denoised)
}
```

### 原地处理

```go
// 原地处理音频数据（节省内存）
err := processor.ProcessInPlace(audioData)
if err != nil {
    log.Fatal(err)
}
```

### 动态控制

```go
// 禁用 AEC
err := processor.SetAECEnable(false)

// 禁用 AGC
err := processor.SetAGCEnable(false)

// 重置处理器状态
err := processor.Reset()
```

### 获取版本

```go
version := denoise.Version()
println("Denoise version:", version)
```

## API 文档

### DenoiseConfig 结构体

```go
type DenoiseConfig struct {
    AECEnable     bool // 启用 AEC (回声消除)
    AGCEnable     bool // 启用 AGC (自动增益控制)
    SampleRate    int  // 采样率 (Hz)
    Channels      int  // 声道数
    BitsPerSample int  // 位深 (bits)
}
```

### DenoiseProcessor 方法

#### NewDenoiseProcessor

创建新的降噪处理器。

```go
processor, err := denoise.NewDenoiseProcessor(config)
```

**参数：**
- `config`: 降噪配置，为 nil 时使用默认配置

**返回值：**
- `*DenoiseProcessor`: 处理器实例
- `error`: 错误信息

#### Process

处理音频数据，返回降噪后的数据。

```go
output, err := processor.Process(input)
```

**参数：**
- `input`: 输入音频数据 (PCM 格式)

**返回值：**
- `[]byte`: 降噪后的音频数据
- `error`: 错误信息

#### ProcessInPlace

原地处理音频数据。

```go
err := processor.ProcessInPlace(data)
```

**参数：**
- `data`: 音频数据 (将被修改)

**返回值：**
- `error`: 错误信息

#### Reset

重置处理器状态。

```go
err := processor.Reset()
```

**返回值：**
- `error`: 错误信息

#### SetAECEnable

设置 AEC 启用状态。

```go
err := processor.SetAECEnable(true)
```

**参数：**
- `enable`: 启用或禁用

**返回值：**
- `error`: 错误信息

#### SetAGCEnable

设置 AGC 启用状态。

```go
err := processor.SetAGCEnable(true)
```

**参数：**
- `enable`: 启用或禁用

**返回值：**
- `error`: 错误信息

#### GetConfig

获取当前配置。

```go
config := processor.GetConfig()
```

**返回值：**
- `DenoiseConfig`: 当前配置

#### Close

关闭处理器并释放资源。

```go
err := processor.Close()
```

**返回值：**
- `error`: 错误信息

### Version

获取降噪库版本。

```go
version := denoise.Version()
```

**返回值：**
- `string`: 版本号

## 配置参数

### 采样率 (SampleRate)

常见值：
- `8000` - 电话质量
- `16000` - 宽带 (推荐)
- `44100` - CD 质量
- `48000` - 专业音频

### 声道数 (Channels)

- `1` - 单声道 (推荐)
- `2` - 立体声

### 位深 (BitsPerSample)

- `16` - 16 位 (推荐)
- `32` - 32 位

## 性能考虑

### 内存使用

- 每个处理器约占用 `buffer_size * 2` 字节内存
- 对于 16kHz 单声道 16 位音频，约为 1.2KB

### 处理延迟

- 处理延迟约为 20ms (一帧)
- 实际延迟取决于硬件和系统负载

### CPU 使用

- AEC 处理：~5-10% CPU
- AGC 处理：~2-5% CPU
- 总体：~10-15% CPU (单核)

## 测试

运行单元测试：

```bash
go test ./... -v
```

运行基准测试：

```bash
go test -bench=. -benchmem
```

## 与 Voiceprint 模块集成

可以在 voiceprint 模块中使用降噪处理：

```go
import (
    "github.com/LingByte/lingllm/denoise"
    "github.com/LingByte/lingllm/voiceprint"
)

// 创建降噪处理器
denoiser, _ := denoise.NewDenoiseProcessor(nil)
defer denoiser.Close()

// 创建声纹处理器
provider, _ := voiceprint.NewFactory().CreateProvider(&voiceprint.ProviderConfig{
    Provider: voiceprint.ProviderVolcengine,
    Options: map[string]interface{}{
        "access_key": "your-key",
        "secret_key": "your-secret",
    },
})
defer provider.Close()

// 处理音频流
audioData := readAudioData()
denoised, _ := denoiser.Process(audioData)
feature, _ := provider.CreateFeature(ctx, "group-1", "feature-1", "info", denoised)
```

## 限制和已知问题

1. **当前实现** - 使用简化的 AEC/AGC 算法，适合演示和测试
2. **生产环境** - 建议链接到 Espressif 或其他专业库
3. **实时性** - 不适合超低延迟应用 (<10ms)

## 未来改进

- [ ] 集成 Espressif ESP-AFE 库
- [ ] 支持更多音频格式
- [ ] 添加频域处理
- [ ] 优化性能和延迟
- [ ] 支持多核处理

## 许可证

AGPL-3.0

## 参考资源

- [Espressif ESP-AFE](https://github.com/espressif/esp-adf)
- [ConversationalAI-Embedded-Kit](https://github.com/volcengine/ConversationalAI-Embedded-Kit)
- [WebRTC Audio Processing](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/audio_processing/)
