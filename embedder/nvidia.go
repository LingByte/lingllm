package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

const (
	maxEmbedInputChars  = 12000
	maxEmbedBatchInputs = 16
)

// NvidiaEmbedder Nvidia embedding 实现
type NvidiaEmbedder struct {
	baseURL        string
	apiKey         string
	model          string
	inputKey       string
	embeddingsPath string
	dimension      int
	httpClient     *http.Client
	maxRetries     int
}

// NewNvidiaEmbedder 创建 Nvidia embedder
func NewNvidiaEmbedder(cfg *Config) *NvidiaEmbedder {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.nvcf.nvidia.com/v2"
	}

	dimension := cfg.Dimension
	if dimension <= 0 {
		dimension = 1024 // Nvidia 默认维度
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	inputKey := "input"
	if customCfg, ok := cfg.CustomConfig["input_key"].(string); ok {
		inputKey = customCfg
	}

	embeddingsPath := ""
	if customCfg, ok := cfg.CustomConfig["embeddings_path"].(string); ok {
		embeddingsPath = customCfg
	}

	return &NvidiaEmbedder{
		baseURL:        baseURL,
		apiKey:         cfg.APIKey,
		model:          cfg.Model,
		inputKey:       inputKey,
		embeddingsPath: embeddingsPath,
		dimension:      dimension,
		httpClient:     &http.Client{Timeout: time.Duration(timeout) * time.Second},
		maxRetries:     maxRetries,
	}
}

func (e *NvidiaEmbedder) Name() string {
	return "nvidia"
}

func (e *NvidiaEmbedder) Provider() string {
	return "nvidia"
}

func (e *NvidiaEmbedder) Dimension() int {
	return e.dimension
}

func (e *NvidiaEmbedder) Close() error {
	return nil
}

// Embed 批量向量化
func (e *NvidiaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrEmptyInput
	}

	// 清理输入
	sanitized := e.sanitizeInputs(texts)

	// 构建端点
	endpoint := e.buildEndpoint()

	// 批量处理
	var allVectors [][]float32

	for start := 0; start < len(sanitized); start += maxEmbedBatchInputs {
		end := start + maxEmbedBatchInputs
		if end > len(sanitized) {
			end = len(sanitized)
		}
		batch := sanitized[start:end]

		vectors, err := e.embedBatch(ctx, endpoint, batch)
		if err != nil {
			return nil, err
		}

		allVectors = append(allVectors, vectors...)
	}

	return allVectors, nil
}

// EmbedSingle 单个文本向量化
func (e *NvidiaEmbedder) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyInput
	}

	vectors, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(vectors) == 0 {
		return nil, ErrInvalidResponse
	}

	return vectors[0], nil
}

// sanitizeInputs 清理输入文本
func (e *NvidiaEmbedder) sanitizeInputs(texts []string) []string {
	sanitized := make([]string, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			text = " " // Nvidia 不接受空字符串
		}
		if len(text) > maxEmbedInputChars {
			text = text[:maxEmbedInputChars]
		}
		sanitized = append(sanitized, text)
	}
	return sanitized
}

// buildEndpoint 构建 API 端点
func (e *NvidiaEmbedder) buildEndpoint() string {
	endpoint := e.baseURL
	if strings.TrimSpace(e.embeddingsPath) != "" {
		p := strings.TrimSpace(e.embeddingsPath)
		p = strings.TrimLeft(p, "/")
		endpoint += "/" + p
	} else {
		if !strings.HasSuffix(endpoint, "/embeddings") {
			endpoint += "/embeddings"
		}
	}
	return endpoint
}

// embedBatch 处理单个批次
func (e *NvidiaEmbedder) embedBatch(ctx context.Context, endpoint string, batch []string) ([][]float32, error) {
	var lastErr error

	for attempt := 1; attempt <= e.maxRetries; attempt++ {
		// 构建请求体
		body := map[string]interface{}{
			"model":    e.model,
			e.inputKey: batch,
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEmbedFailed, err)
		}

		// 创建请求
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.apiKey)

		// 发送请求
		resp, err := e.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < e.maxRetries {
				time.Sleep(time.Duration(attempt*200) * time.Millisecond)
			}
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// 检查状态码
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode == http.StatusTooManyRequests {
				lastErr = ErrRateLimited
				if attempt < e.maxRetries {
					time.Sleep(time.Duration(attempt*500) * time.Millisecond)
				}
				continue
			}
			return nil, fmt.Errorf("%w: status %d: %s", ErrEmbedFailed, resp.StatusCode, truncateForError(respBody, 200))
		}

		// 解析响应
		var result struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}

		if err := json.Unmarshal(respBody, &result); err != nil {
			lastErr = fmt.Errorf("%w: %v", ErrInvalidResponse, err)
			if attempt < e.maxRetries {
				time.Sleep(time.Duration(attempt*300) * time.Millisecond)
			}
			continue
		}

		if len(result.Data) == 0 {
			lastErr = fmt.Errorf("%w: no embeddings returned", ErrInvalidResponse)
			if attempt < e.maxRetries {
				time.Sleep(time.Duration(attempt*300) * time.Millisecond)
			}
			continue
		}

		// 验证向量
		vectors := make([][]float32, 0, len(result.Data))
		for _, item := range result.Data {
			if len(item.Embedding) == 0 {
				return nil, fmt.Errorf("%w: empty embedding returned", ErrInvalidResponse)
			}
			vectors = append(vectors, item.Embedding)
		}

		return vectors, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, ErrEmbedFailed
}

// truncateForError 截断错误消息
func truncateForError(data []byte, maxLen int) string {
	s := string(data)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
