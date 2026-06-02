package embedder

import (
	"context"
	"fmt"
	"sync"
)

// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// DefaultFactory 默认工厂实现
type DefaultFactory struct {
	mu        sync.RWMutex
	factories map[string]EmbedderFactory
}

// NewDefaultFactory 创建默认工厂
func NewDefaultFactory() *DefaultFactory {
	df := &DefaultFactory{
		factories: make(map[string]EmbedderFactory),
	}

	// 注册内置提供商
	df.Register(&OpenAIFactory{})
	df.Register(&OllamaFactory{})
	df.Register(&LocalFactory{})
	df.Register(&NvidiaFactory{})
	df.Register(&DashScopeFactory{})

	return df
}

// Register 注册工厂
func (f *DefaultFactory) Register(factory EmbedderFactory) error {
	if factory == nil {
		return ErrInvalidConfig
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	name := factory.Name()
	if name == "" {
		return ErrInvalidConfig
	}

	f.factories[name] = factory
	return nil
}

// Create 创建 embedder
func (f *DefaultFactory) Create(ctx context.Context, cfg *Config) (Embedder, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}

	if cfg.Provider == "" {
		return nil, ErrProviderNotFound
	}

	f.mu.RLock()
	factory, exists := f.factories[cfg.Provider]
	f.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, cfg.Provider)
	}

	return factory.Create(ctx, cfg)
}

// List 列出所有支持的提供商
func (f *DefaultFactory) List() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	providers := make([]string, 0, len(f.factories))
	for name := range f.factories {
		providers = append(providers, name)
	}
	return providers
}

// Supports 检查是否支持该提供商
func (f *DefaultFactory) Supports(provider string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	_, exists := f.factories[provider]
	return exists
}

// OpenAIFactory OpenAI embedder 工厂
type OpenAIFactory struct{}

func (f *OpenAIFactory) Name() string {
	return "openai"
}

func (f *OpenAIFactory) Supports(provider string) bool {
	return provider == "openai"
}

func (f *OpenAIFactory) Create(ctx context.Context, cfg *Config) (Embedder, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}
	if cfg.Model == "" {
		return nil, ErrModelNotFound
	}

	return NewOpenAIEmbedder(cfg), nil
}

// OllamaFactory Ollama embedder 工厂
type OllamaFactory struct{}

func (f *OllamaFactory) Name() string {
	return "ollama"
}

func (f *OllamaFactory) Supports(provider string) bool {
	return provider == "ollama"
}

func (f *OllamaFactory) Create(ctx context.Context, cfg *Config) (Embedder, error) {
	if cfg.Model == "" {
		return nil, ErrModelNotFound
	}

	return NewOllamaEmbedder(cfg), nil
}

// LocalFactory 本地 embedder 工厂
type LocalFactory struct{}

func (f *LocalFactory) Name() string {
	return "local"
}

func (f *LocalFactory) Supports(provider string) bool {
	return provider == "local"
}

func (f *LocalFactory) Create(ctx context.Context, cfg *Config) (Embedder, error) {
	if cfg.Model == "" {
		return nil, ErrModelNotFound
	}

	return NewLocalEmbedder(cfg), nil
}

// GlobalFactory 全局工厂实例
var globalFactory *DefaultFactory
var factoryOnce sync.Once

// GetFactory 获取全局工厂实例
func GetFactory() *DefaultFactory {
	factoryOnce.Do(func() {
		globalFactory = NewDefaultFactory()
	})
	return globalFactory
}

// Create 使用全局工厂创建 embedder
func Create(ctx context.Context, cfg *Config) (Embedder, error) {
	return GetFactory().Create(ctx, cfg)
}

// Register 向全局工厂注册提供商
func Register(factory EmbedderFactory) error {
	return GetFactory().Register(factory)
}

// List 列出所有支持的提供商
func List() []string {
	return GetFactory().List()
}

// Supports 检查是否支持该提供商
func Supports(provider string) bool {
	return GetFactory().Supports(provider)
}

// NvidiaFactory Nvidia embedder 工厂
type NvidiaFactory struct{}

func (f *NvidiaFactory) Name() string {
	return "nvidia"
}

func (f *NvidiaFactory) Supports(provider string) bool {
	return provider == "nvidia"
}

func (f *NvidiaFactory) Create(ctx context.Context, cfg *Config) (Embedder, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}
	if cfg.Model == "" {
		return nil, ErrModelNotFound
	}

	return NewNvidiaEmbedder(cfg), nil
}

// DashScopeFactory DashScope embedder 工厂
type DashScopeFactory struct{}

func (f *DashScopeFactory) Name() string {
	return "dashscope"
}

func (f *DashScopeFactory) Supports(provider string) bool {
	return provider == "dashscope"
}

func (f *DashScopeFactory) Create(ctx context.Context, cfg *Config) (Embedder, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}
	if cfg.Model == "" {
		return nil, ErrModelNotFound
	}

	return NewDashScopeEmbedder(cfg), nil
}
