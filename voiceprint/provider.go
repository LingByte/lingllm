// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package voiceprint

import (
	"context"
	"fmt"
)

// Provider 声纹识别提供商类型
type Provider string

const (
	ProviderXunfei Provider = "xunfei"
)

// VoiceprintProvider 声纹识别提供商接口
type VoiceprintProvider interface {
	// 特征库管理
	CreateGroup(ctx context.Context, groupID, groupName, groupInfo string) (*CreateGroupResult, error)
	DeleteGroup(ctx context.Context, groupID string) error

	// 特征管理
	CreateFeature(ctx context.Context, groupID, featureID, featureInfo string, audioData []byte) (*CreateFeatureResult, error)
	UpdateFeature(ctx context.Context, groupID, featureID, featureInfo string, audioData []byte, cover bool) (*UpdateFeatureResult, error)
	DeleteFeature(ctx context.Context, groupID, featureID string) error
	QueryFeatureList(ctx context.Context, groupID string) (*QueryFeatureListResult, error)

	// 特征比对
	SearchScoreFea(ctx context.Context, groupID, dstFeatureID string, audioData []byte) (*SearchScoreFeaResult, error)
	SearchFea(ctx context.Context, groupID string, topK int, audioData []byte) (*SearchFeaResult, error)

	// 健康检查
	HealthCheck(ctx context.Context) error

	// 关闭
	Close() error
}

// ProviderConfig 提供商配置
type ProviderConfig struct {
	Provider Provider                `json:"provider"`
	Options  map[string]interface{}  `json:"options"`
}

// Factory 声纹识别工厂
type Factory struct{}

// NewFactory 创建工厂
func NewFactory() *Factory {
	return &Factory{}
}

// CreateProvider 创建提供商
func (f *Factory) CreateProvider(config *ProviderConfig) (VoiceprintProvider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	switch config.Provider {
	case ProviderXunfei:
		return f.createXunfeiProvider(config.Options)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}
}

// createXunfeiProvider 创建讯飞提供商
func (f *Factory) createXunfeiProvider(options map[string]interface{}) (VoiceprintProvider, error) {
	if options == nil {
		return nil, fmt.Errorf("options is required for xunfei provider")
	}

	apiKey, ok := options["api_key"].(string)
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("api_key is required")
	}

	apiSecret, ok := options["api_secret"].(string)
	if !ok || apiSecret == "" {
		return nil, fmt.Errorf("api_secret is required")
	}

	appID, ok := options["app_id"].(string)
	if !ok || appID == "" {
		return nil, fmt.Errorf("app_id is required")
	}

	config := &XunfeiConfig{
		APIKey:    apiKey,
		APISecret: apiSecret,
		AppID:     appID,
	}

	if baseURL, ok := options["base_url"].(string); ok && baseURL != "" {
		config.BaseURL = baseURL
	}

	client, err := NewXunfeiClient(config, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create xunfei client: %w", err)
	}

	return &XunfeiProviderAdapter{client: client}, nil
}

// XunfeiProviderAdapter 讯飞提供商适配器
type XunfeiProviderAdapter struct {
	client *XunfeiClient
}

// CreateGroup 创建特征库
func (a *XunfeiProviderAdapter) CreateGroup(ctx context.Context, groupID, groupName, groupInfo string) (*CreateGroupResult, error) {
	return a.client.CreateGroup(ctx, groupID, groupName, groupInfo)
}

// DeleteGroup 删除特征库
func (a *XunfeiProviderAdapter) DeleteGroup(ctx context.Context, groupID string) error {
	_, err := a.client.DeleteGroup(ctx, groupID)
	return err
}

// CreateFeature 创建特征
func (a *XunfeiProviderAdapter) CreateFeature(ctx context.Context, groupID, featureID, featureInfo string, audioData []byte) (*CreateFeatureResult, error) {
	return a.client.CreateFeature(ctx, groupID, featureID, featureInfo, audioData)
}

// UpdateFeature 更新特征
func (a *XunfeiProviderAdapter) UpdateFeature(ctx context.Context, groupID, featureID, featureInfo string, audioData []byte, cover bool) (*UpdateFeatureResult, error) {
	return a.client.UpdateFeature(ctx, groupID, featureID, featureInfo, audioData, cover)
}

// DeleteFeature 删除特征
func (a *XunfeiProviderAdapter) DeleteFeature(ctx context.Context, groupID, featureID string) error {
	_, err := a.client.DeleteFeature(ctx, groupID, featureID)
	return err
}

// QueryFeatureList 查询特征列表
func (a *XunfeiProviderAdapter) QueryFeatureList(ctx context.Context, groupID string) (*QueryFeatureListResult, error) {
	return a.client.QueryFeatureList(ctx, groupID)
}

// SearchScoreFea 1:1比对
func (a *XunfeiProviderAdapter) SearchScoreFea(ctx context.Context, groupID, dstFeatureID string, audioData []byte) (*SearchScoreFeaResult, error) {
	return a.client.SearchScoreFea(ctx, groupID, dstFeatureID, audioData)
}

// SearchFea 1:N比对
func (a *XunfeiProviderAdapter) SearchFea(ctx context.Context, groupID string, topK int, audioData []byte) (*SearchFeaResult, error) {
	return a.client.SearchFea(ctx, groupID, topK, audioData)
}

// HealthCheck 健康检查
func (a *XunfeiProviderAdapter) HealthCheck(ctx context.Context) error {
	// 讯飞API没有专门的健康检查端点，可以通过创建一个临时特征库来验证
	// 这里简单返回nil，实际应用可以根据需要实现
	return nil
}

// Close 关闭
func (a *XunfeiProviderAdapter) Close() error {
	return a.client.Close()
}
