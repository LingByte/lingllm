// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package denoise

import (
	"testing"
)

func TestNewDenoiseProcessor(t *testing.T) {
	tests := []struct {
		name    string
		config  *DenoiseConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false, // Should use default config
		},
		{
			name: "valid config",
			config: &DenoiseConfig{
				AECEnable:     true,
				AGCEnable:     true,
				SampleRate:    16000,
				Channels:      1,
				BitsPerSample: 16,
			},
			wantErr: false,
		},
		{
			name: "stereo config",
			config: &DenoiseConfig{
				AECEnable:     true,
				AGCEnable:     true,
				SampleRate:    16000,
				Channels:      2,
				BitsPerSample: 16,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor, err := NewDenoiseProcessor(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDenoiseProcessor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && processor == nil {
				t.Error("NewDenoiseProcessor() returned nil processor")
				return
			}
			if processor != nil {
				defer processor.Close()
			}
		})
	}
}

func TestDenoiseProcessor_Process(t *testing.T) {
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

	// Create test audio data (16-bit PCM)
	input := make([]byte, 1024)
	for i := 0; i < len(input); i += 2 {
		// Simple sine wave pattern
		input[i] = 0x00
		input[i+1] = 0x10
	}

	output, err := processor.Process(input)
	if err != nil {
		t.Errorf("Process() error = %v", err)
		return
	}

	if len(output) != len(input) {
		t.Errorf("Process() output length = %d, want %d", len(output), len(input))
	}
}

func TestDenoiseProcessor_ProcessInPlace(t *testing.T) {
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

	// Create test audio data
	data := make([]byte, 1024)
	for i := 0; i < len(data); i += 2 {
		data[i] = 0x00
		data[i+1] = 0x10
	}

	err = processor.ProcessInPlace(data)
	if err != nil {
		t.Errorf("ProcessInPlace() error = %v", err)
	}
}

func TestDenoiseProcessor_Reset(t *testing.T) {
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

	err = processor.Reset()
	if err != nil {
		t.Errorf("Reset() error = %v", err)
	}
}

func TestDenoiseProcessor_SetAECEnable(t *testing.T) {
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

	err = processor.SetAECEnable(false)
	if err != nil {
		t.Errorf("SetAECEnable() error = %v", err)
	}

	config := processor.GetConfig()
	if config.AECEnable != false {
		t.Errorf("SetAECEnable() config.AECEnable = %v, want false", config.AECEnable)
	}
}

func TestDenoiseProcessor_SetAGCEnable(t *testing.T) {
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

	err = processor.SetAGCEnable(false)
	if err != nil {
		t.Errorf("SetAGCEnable() error = %v", err)
	}

	config := processor.GetConfig()
	if config.AGCEnable != false {
		t.Errorf("SetAGCEnable() config.AGCEnable = %v, want false", config.AGCEnable)
	}
}

func TestDenoiseProcessor_Version(t *testing.T) {
	version := Version()
	if version == "" {
		t.Error("Version() returned empty string")
	}
	t.Logf("Denoise version: %s", version)
}

func TestDenoiseProcessor_Close(t *testing.T) {
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

	err = processor.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func BenchmarkDenoiseProcessor_Process(b *testing.B) {
	processor, err := NewDenoiseProcessor(&DenoiseConfig{
		AECEnable:     true,
		AGCEnable:     true,
		SampleRate:    16000,
		Channels:      1,
		BitsPerSample: 16,
	})
	if err != nil {
		b.Fatalf("NewDenoiseProcessor() error = %v", err)
	}
	defer processor.Close()

	// Create test audio data
	input := make([]byte, 1024)
	for i := 0; i < len(input); i += 2 {
		input[i] = 0x00
		input[i+1] = 0x10
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = processor.Process(input)
	}
}
