// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package vad

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// DefaultFactory 默认工厂实现
type DefaultFactory struct {
	logger *zap.Logger
}

// NewDefaultFactory 创建新的工厂
func NewDefaultFactory(logger *zap.Logger) *DefaultFactory {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &DefaultFactory{
		logger: logger,
	}
}

// CreateDetector 创建检测器
func (f *DefaultFactory) CreateDetector(config *Config) (Detector, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	switch config.Provider {
	case ProviderHTTP:
		return NewHTTPDetector(config, f.logger)
	case ProviderWebSocket:
		return NewWebSocketDetector(config, f.logger)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}
}

// CreateSessionManager 创建会话管理器
func (f *DefaultFactory) CreateSessionManager(detector Detector, config *Config) (SessionManager, error) {
	if detector == nil {
		return nil, fmt.Errorf("detector is required")
	}

	if config == nil {
		config = &Config{
			SessionTTL: 5 * time.Minute, // 默认 5 分钟
		}
	}

	return NewDefaultSessionManager(detector, config, f.logger)
}

// CreateDetectorAndManager 创建检测器和会话管理器
func (f *DefaultFactory) CreateDetectorAndManager(config *Config) (Detector, SessionManager, error) {
	if config == nil {
		return nil, nil, fmt.Errorf("config is required")
	}

	detector, err := f.CreateDetector(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create detector: %w", err)
	}

	manager, err := f.CreateSessionManager(detector, config)
	if err != nil {
		_ = detector.Close()
		return nil, nil, fmt.Errorf("failed to create session manager: %w", err)
	}

	return detector, manager, nil
}

// ValidateConfig 验证配置
func (f *DefaultFactory) ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is required")
	}

	switch config.Provider {
	case ProviderHTTP:
		if config.BaseURL == "" {
			return fmt.Errorf("base_url is required for HTTP provider")
		}
	case ProviderWebSocket:
		if config.BaseURL == "" {
			return fmt.Errorf("base_url is required for WebSocket provider")
		}
	default:
		return fmt.Errorf("unsupported provider: %s", config.Provider)
	}

	return nil
}

// GetSupportedProviders 获取支持的提供商列表
func (f *DefaultFactory) GetSupportedProviders() []Provider {
	return []Provider{
		ProviderHTTP,
		ProviderWebSocket,
	}
}
