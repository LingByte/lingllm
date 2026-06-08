// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package vad

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// HTTPDetector HTTP VAD 检测器
type HTTPDetector struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
	config     *Config
}

// NewHTTPDetector 创建新的 HTTP VAD 检测器
func NewHTTPDetector(config *Config, logger *zap.Logger) (*HTTPDetector, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if config.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required for HTTP detector")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	detector := &HTTPDetector{
		baseURL: config.BaseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
		config: config,
	}

	return detector, nil
}

// Detect 检测音频中的语音活动
func (d *HTTPDetector) Detect(ctx context.Context, req *DetectRequest) (*DetectResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	// Base64 编码音频数据
	audioBase64 := base64.StdEncoding.EncodeToString(req.AudioData)

	httpReq := map[string]interface{}{
		"audio_data":   audioBase64,
		"audio_format": req.AudioFormat,
		"sample_rate":  req.SampleRate,
		"channels":     req.Channels,
	}

	if req.Threshold > 0 {
		httpReq["threshold"] = req.Threshold
	}

	jsonData, err := json.Marshal(httpReq)
	if err != nil {
		d.logger.Error("failed to marshal request", zap.Error(err))
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 构建 URL
	url := d.baseURL + "/vad"
	if req.SessionID != "" {
		url = fmt.Sprintf("%s?session_id=%s", url, req.SessionID)
	}

	// 创建 HTTP 请求
	httpRequest, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		d.logger.Error("failed to create request", zap.Error(err))
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := d.httpClient.Do(httpRequest)
	if err != nil {
		d.logger.Error("failed to send request", zap.Error(err))
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		d.logger.Error("VAD service error",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(body)))
		return nil, fmt.Errorf("VAD service error (status %d): %s", resp.StatusCode, string(body))
	}

	var detectResp DetectResponse
	if err := json.NewDecoder(resp.Body).Decode(&detectResp); err != nil {
		d.logger.Error("failed to decode response", zap.Error(err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	detectResp.Timestamp = time.Now()
	return &detectResp, nil
}

// HealthCheck 健康检查
func (d *HTTPDetector) HealthCheck(ctx context.Context) error {
	url := d.baseURL + "/health"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		d.logger.Error("failed to create health check request", zap.Error(err))
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		d.logger.Error("health check failed", zap.Error(err))
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	var healthResp HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		return fmt.Errorf("failed to decode health response: %w", err)
	}

	d.logger.Info("VAD service health check passed", zap.String("status", healthResp.Status))
	return nil
}

// Close 关闭检测器
func (d *HTTPDetector) Close() error {
	d.httpClient.CloseIdleConnections()
	return nil
}

// Provider 返回提供商名称
func (d *HTTPDetector) Provider() Provider {
	return ProviderHTTP
}

// SetTimeout 设置 HTTP 超时时间
func (d *HTTPDetector) SetTimeout(timeout time.Duration) {
	d.httpClient.Timeout = timeout
}

// ResetSession 重置会话
func (d *HTTPDetector) ResetSession(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/vad/reset?session_id=%s", d.baseURL, sessionID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		d.logger.Error("failed to create reset request", zap.Error(err))
		return fmt.Errorf("failed to create reset request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		d.logger.Error("failed to reset session", zap.Error(err))
		return fmt.Errorf("failed to reset session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reset session failed with status %d: %s", resp.StatusCode, string(body))
	}

	d.logger.Debug("session reset", zap.String("session_id", sessionID))
	return nil
}
