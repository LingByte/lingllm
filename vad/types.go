// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package vad

import (
	"context"
	"time"
)

// Provider VAD 提供商类型
type Provider string

const (
	ProviderHTTP      Provider = "http"       // HTTP 服务提供商
	ProviderWebSocket Provider = "websocket"  // WebSocket 提供商
)

// DetectRequest VAD 检测请求
type DetectRequest struct {
	AudioData   []byte    `json:"audio_data,omitempty"`   // 音频数据
	AudioFormat string    `json:"audio_format"`            // "pcm" 或 "opus"
	SampleRate  int       `json:"sample_rate"`             // 采样率
	Channels    int       `json:"channels"`                // 声道数
	Threshold   float64   `json:"threshold,omitempty"`     // VAD 阈值（可选）
	SessionID   string    `json:"session_id,omitempty"`    // 会话 ID
	Timestamp   time.Time `json:"timestamp,omitempty"`     // 时间戳
}

// DetectResponse VAD 检测响应
type DetectResponse struct {
	HaveVoice  bool    `json:"have_voice"`           // 是否有语音
	VoiceStop  bool    `json:"voice_stop"`           // 语音是否停止
	SpeechProb float64 `json:"speech_prob,omitempty"` // 语音概率
	Timestamp  time.Time `json:"timestamp,omitempty"` // 响应时间戳
}

// Detector VAD 检测器接口
type Detector interface {
	// Detect 检测音频中的语音活动
	Detect(ctx context.Context, req *DetectRequest) (*DetectResponse, error)

	// HealthCheck 健康检查
	HealthCheck(ctx context.Context) error

	// Close 关闭检测器
	Close() error

	// Provider 返回提供商名称
	Provider() Provider
}

// SessionManager VAD 会话管理器接口
type SessionManager interface {
	// GetOrCreateSession 获取或创建会话
	GetOrCreateSession(sessionID string) *Session

	// ProcessAudio 处理音频数据
	ProcessAudio(ctx context.Context, sessionID string, audioData []byte, format string, threshold ...float64) (*DetectResponse, error)

	// GetSession 获取会话
	GetSession(sessionID string) *Session

	// ResetSession 重置会话
	ResetSession(ctx context.Context, sessionID string) error

	// DeleteSession 删除会话
	DeleteSession(ctx context.Context, sessionID string) error

	// ListSessions 列出所有活跃会话
	ListSessions() []string

	// Close 关闭会话管理器
	Close() error
}

// Session VAD 会话
type Session struct {
	ID             string
	CreatedAt      time.Time
	LastActivityAt time.Time
	HaveVoice      bool
	VoiceStop      bool
	LastSpeechProb float64
	Metadata       map[string]interface{} // 自定义元数据
}

// Config VAD 配置
type Config struct {
	Provider    Provider              `json:"provider"`     // 提供商
	BaseURL     string                `json:"base_url"`     // 基础 URL（HTTP 提供商）
	Timeout     time.Duration         `json:"timeout"`      // 超时时间
	SessionTTL  time.Duration         `json:"session_ttl"`  // 会话过期时间
	Options     map[string]interface{} `json:"options"`      // 提供商特定选项
}

// Factory VAD 工厂接口
type Factory interface {
	// CreateDetector 创建检测器
	CreateDetector(config *Config) (Detector, error)

	// CreateSessionManager 创建会话管理器
	CreateSessionManager(detector Detector, config *Config) (SessionManager, error)
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Message string `json:"message,omitempty"`
}

// WebSocketMessage WebSocket 消息
type WebSocketMessage struct {
	Type      string          `json:"type"`                  // "audio", "reset"
	Data      string          `json:"data,omitempty"`        // Base64 编码的音频数据
	Format    string          `json:"format,omitempty"`      // "pcm" 或 "opus"
	SessionID string          `json:"session_id,omitempty"`  // 会话 ID
	Result    *DetectResponse `json:"result,omitempty"`      // 检测结果
	Error     string          `json:"error,omitempty"`       // 错误信息
	Timestamp time.Time       `json:"timestamp,omitempty"`   // 时间戳
}

// DetectorOptions 检测器选项
type DetectorOptions struct {
	Timeout      time.Duration
	MaxRetries   int
	RetryBackoff time.Duration
	Logger       interface{} // *zap.Logger
}

// SessionManagerOptions 会话管理器选项
type SessionManagerOptions struct {
	SessionTTL      time.Duration
	CleanupInterval time.Duration
	MaxSessions     int
	Logger          interface{} // *zap.Logger
}
