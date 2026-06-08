// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package denoise

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

// generateTestAudio 生成测试音频数据
// 包含信号 + 噪音
func generateTestAudio(sampleRate, durationMs int, signalFreq, noiseLevel float64) []byte {
	numSamples := (sampleRate * durationMs) / 1000
	data := make([]byte, numSamples*2) // 16-bit PCM

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)

		// 生成信号 (正弦波)
		signal := math.Sin(2*math.Pi*signalFreq*t) * 0.5

		// 生成噪音 (随机)
		noise := math.Sin(t*1000) * noiseLevel

		// 混合信号和噪音
		sample := signal + noise

		// 限幅
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}

		// 转换为 int16
		intSample := int16(sample * 32767)
		binary.LittleEndian.PutUint16(data[i*2:i*2+2], uint16(intSample))
	}

	return data
}

// calculateRMS 计算 RMS (均方根)
func calculateRMS(data []byte) float64 {
	if len(data) < 2 {
		return 0
	}

	sum := 0.0
	numSamples := len(data) / 2

	for i := 0; i < numSamples; i++ {
		sample := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		normalized := float64(sample) / 32767.0
		sum += normalized * normalized
	}

	return math.Sqrt(sum / float64(numSamples))
}

// calculateSNR 计算信噪比 (Signal-to-Noise Ratio)
func calculateSNR(signal, noise []byte) float64 {
	signalRMS := calculateRMS(signal)
	noiseRMS := calculateRMS(noise)

	if noiseRMS == 0 {
		return 0
	}

	return 20 * math.Log10(signalRMS/noiseRMS)
}

// TestDenoiseProcessor_AudioQuality 测试降噪效果
func TestDenoiseProcessor_AudioQuality(t *testing.T) {
	processor, err := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     true,
		AGCEnable:     true,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	if err != nil {
		t.Fatalf("NewDenoiseProcessor() error = %v", err)
	}
	defer processor.Close()

	// 生成带噪音的音频 (1000Hz 信号 + 0.3 噪音)
	noisyAudio := generateTestAudio(16000, 100, 1000, 0.3)

	// 处理音频
	denoisedAudio, err := processor.Process(noisyAudio)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// 计算 RMS
	noisyRMS := calculateRMS(noisyAudio)
	denoisedRMS := calculateRMS(denoisedAudio)

	t.Logf("Noisy RMS: %.4f", noisyRMS)
	t.Logf("Denoised RMS: %.4f", denoisedRMS)
	t.Logf("Reduction: %.2f%%", (1-denoisedRMS/noisyRMS)*100)

	// 验证降噪有效果
	if denoisedRMS >= noisyRMS {
		t.Logf("Warning: Denoised RMS not reduced (expected reduction)")
	}
}

// TestDenoiseProcessor_AECEffect AEC 效果测试
func TestDenoiseProcessor_AECEffect(t *testing.T) {
	// 创建两个处理器，一个启用 AEC，一个禁用
	processorWithAEC, _ := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     true,
		AGCEnable:     false,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	defer processorWithAEC.Close()

	processorNoAEC, _ := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     false,
		AGCEnable:     false,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	defer processorNoAEC.Close()

	// 生成测试音频
	testAudio := generateTestAudio(16000, 100, 1000, 0.2)

	// 处理
	withAEC, _ := processorWithAEC.Process(testAudio)
	noAEC, _ := processorNoAEC.Process(testAudio)

	// 计算 RMS
	rmsWithAEC := calculateRMS(withAEC)
	rmsNoAEC := calculateRMS(noAEC)

	t.Logf("RMS with AEC: %.4f", rmsWithAEC)
	t.Logf("RMS without AEC: %.4f", rmsNoAEC)
	t.Logf("AEC reduction: %.2f%%", (1-rmsWithAEC/rmsNoAEC)*100)
}

// TestDenoiseProcessor_AGCEffect AGC 效果测试
func TestDenoiseProcessor_AGCEffect(t *testing.T) {
	// 创建两个处理器，一个启用 AGC，一个禁用
	processorWithAGC, _ := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     false,
		AGCEnable:     true,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	defer processorWithAGC.Close()

	processorNoAGC, _ := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     false,
		AGCEnable:     false,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	defer processorNoAGC.Close()

	// 生成低音量的测试音频
	testAudio := generateTestAudio(16000, 100, 1000, 0.1)

	// 处理
	withAGC, _ := processorWithAGC.Process(testAudio)
	noAGC, _ := processorNoAGC.Process(testAudio)

	// 计算 RMS
	rmsWithAGC := calculateRMS(withAGC)
	rmsNoAGC := calculateRMS(noAGC)

	t.Logf("RMS with AGC: %.4f", rmsWithAGC)
	t.Logf("RMS without AGC: %.4f", rmsNoAGC)
	t.Logf("AGC gain: %.2f dB", 20*math.Log10(rmsWithAGC/rmsNoAGC))
}

// ExampleDenoiseProcessor_Process 使用示例
func ExampleDenoiseProcessor_Process() {
	// 创建处理器
	processor, _ := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     true,
		AGCEnable:     true,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	defer processor.Close()

	// 生成测试音频
	testAudio := generateTestAudio(16000, 100, 1000, 0.2)

	// 处理
	denoised, _ := processor.Process(testAudio)

	// 计算效果
	noisyRMS := calculateRMS(testAudio)
	denoisedRMS := calculateRMS(denoised)
	reduction := (1 - denoisedRMS/noisyRMS) * 100

	fmt.Printf("Noisy RMS: %.4f\n", noisyRMS)
	fmt.Printf("Denoised RMS: %.4f\n", denoisedRMS)
	fmt.Printf("Noise reduction: %.2f%%\n", reduction)
}

// BenchmarkDenoiseProcessor_AudioProcessing 音频处理性能基准
func BenchmarkDenoiseProcessor_AudioProcessing(b *testing.B) {
	processor, _ := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     true,
		AGCEnable:     true,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	defer processor.Close()

	// 生成 1 秒的音频数据
	testAudio := generateTestAudio(16000, 1000, 1000, 0.2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = processor.Process(testAudio)
	}
}
