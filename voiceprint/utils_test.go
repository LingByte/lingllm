// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package voiceprint

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestLoadAudioFile(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "invalid extension",
			path:    "test.mp3",
			wantErr: true,
		},
		{
			name:    "file not found",
			path:    "nonexistent.wav",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadAudioFile(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadAudioFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateWAVFormat(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "too short",
			data:    make([]byte, 10),
			wantErr: true,
		},
		{
			name:    "invalid RIFF header",
			data:    append([]byte("XXXX"), make([]byte, 40)...),
			wantErr: true,
		},
		{
			name: "valid WAV header",
			data: func() []byte {
				data := make([]byte, 44)
				copy(data[0:4], "RIFF")
				copy(data[8:12], "WAVE")
				copy(data[12:16], "fmt ")
				return data
			}(),
			wantErr: false,
		},
		{
			name:    "invalid WAVE identifier",
			data:    append([]byte("RIFF"), append(make([]byte, 4), append([]byte("XXXX"), make([]byte, 32)...)...)...),
			wantErr: true,
		},
		{
			name:    "invalid fmt chunk",
			data:    append([]byte("RIFF"), append(make([]byte, 4), append([]byte("WAVE"), append([]byte("XXXX"), make([]byte, 28)...)...)...)...),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWAVFormat(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWAVFormat() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetAudioInfo(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    *AudioInfo
		wantErr bool
	}{
		{
			name:    "invalid format",
			data:    make([]byte, 10),
			wantErr: true,
		},
		{
			name: "valid WAV file",
			data: func() []byte {
				data := make([]byte, 44)
				copy(data[0:4], "RIFF")
				binary.LittleEndian.PutUint32(data[4:8], 36)
				copy(data[8:12], "WAVE")
				copy(data[12:16], "fmt ")
				binary.LittleEndian.PutUint32(data[16:20], 16)

				// Audio format (1 = PCM)
				binary.LittleEndian.PutUint16(data[20:22], 1)
				// Channels (2)
				binary.LittleEndian.PutUint16(data[22:24], 2)
				// Sample rate (16000)
				binary.LittleEndian.PutUint32(data[24:28], 16000)
				// Byte rate
				binary.LittleEndian.PutUint32(data[28:32], 64000)
				// Block align
				binary.LittleEndian.PutUint16(data[32:34], 4)
				// Bits per sample (16)
				binary.LittleEndian.PutUint16(data[34:36], 16)

				return data
			}(),
			want: &AudioInfo{
				Format:        "WAV",
				SampleRate:    16000,
				Channels:      2,
				BitsPerSample: 16,
				FileSize:      44,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAudioInfo(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAudioInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != nil {
				if got.Format != tt.want.Format ||
					got.SampleRate != tt.want.SampleRate ||
					got.Channels != tt.want.Channels ||
					got.BitsPerSample != tt.want.BitsPerSample ||
					got.FileSize != tt.want.FileSize {
					t.Errorf("GetAudioInfo() = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestExtractPCMData(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "invalid format",
			data:    make([]byte, 10),
			wantErr: true,
		},
		{
			name: "valid WAV with data chunk",
			data: func() []byte {
				// Create a minimal valid WAV file
				data := make([]byte, 100)
				copy(data[0:4], "RIFF")
				binary.LittleEndian.PutUint32(data[4:8], 92)
				copy(data[8:12], "WAVE")
				copy(data[12:16], "fmt ")
				binary.LittleEndian.PutUint32(data[16:20], 16)
				binary.LittleEndian.PutUint16(data[20:22], 1) // PCM
				binary.LittleEndian.PutUint16(data[22:24], 1) // Channels
				binary.LittleEndian.PutUint32(data[24:28], 16000) // Sample rate
				binary.LittleEndian.PutUint32(data[28:32], 32000) // Byte rate
				binary.LittleEndian.PutUint16(data[32:34], 2) // Block align
				binary.LittleEndian.PutUint16(data[34:36], 16) // Bits per sample

				// Add data chunk
				copy(data[36:40], "data")
				binary.LittleEndian.PutUint32(data[40:44], 20)

				return data
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractPCMData(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractPCMData() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateAudioDuration(t *testing.T) {
	tests := []struct {
		name       string
		pcmData    []byte
		sampleRate int
		channels   int
		want       time.Duration
	}{
		{
			name:       "zero length",
			pcmData:    []byte{},
			sampleRate: 16000,
			channels:   1,
			want:       0,
		},
		{
			name:       "1 second of audio",
			pcmData:    make([]byte, 16000*2), // 16000 samples * 2 bytes per sample
			sampleRate: 16000,
			channels:   1,
			want:       time.Second,
		},
		{
			name:       "stereo audio",
			pcmData:    make([]byte, 16000*2*2), // 16000 samples * 2 channels * 2 bytes per sample
			sampleRate: 16000,
			channels:   2,
			want:       time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateAudioDuration(tt.pcmData, tt.sampleRate, tt.channels)
			if got != tt.want {
				t.Errorf("CalculateAudioDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeAudioData(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: false,
		},
		{
			name:    "valid PCM data",
			data:    make([]byte, 1000),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeAudioData(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeAudioData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Error("NormalizeAudioData() returned nil")
			}
		})
	}
}

func TestConvertAudioFormat(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		fromFormat string
		toFormat   string
		wantErr    bool
	}{
		{
			name:       "unsupported format",
			data:       []byte{},
			fromFormat: "unknown",
			toFormat:   "pcm",
			wantErr:    true,
		},
		{
			name:       "pcm to pcm",
			data:       make([]byte, 100),
			fromFormat: "pcm",
			toFormat:   "pcm",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertAudioFormat(tt.data, tt.fromFormat, tt.toFormat)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertAudioFormat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Error("ConvertAudioFormat() returned nil")
			}
		})
	}
}

func TestComputeAudioHash(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: false,
		},
		{
			name:    "valid data",
			data:    []byte("test audio data"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ComputeAudioHash(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ComputeAudioHash() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Error("ComputeAudioHash() returned empty string")
			}
		})
	}
}

func TestIsValidAudioFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   bool
	}{
		{
			name:   "valid PCM",
			format: "pcm",
			want:   true,
		},
		{
			name:   "valid WAV",
			format: "wav",
			want:   true,
		},
		{
			name:   "invalid format",
			format: "unknown",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidAudioFormat(tt.format)
			if got != tt.want {
				t.Errorf("IsValidAudioFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAudioInfo_String(t *testing.T) {
	info := &AudioInfo{
		Format:        "WAV",
		SampleRate:    16000,
		Channels:      2,
		BitsPerSample: 16,
		Duration:      time.Second,
		FileSize:      64000,
	}

	result := info.String()
	if result == "" {
		t.Error("AudioInfo.String() returned empty string")
	}

	// Check that the string contains expected information
	if !bytes.Contains([]byte(result), []byte("WAV")) {
		t.Error("AudioInfo.String() does not contain format")
	}
}
