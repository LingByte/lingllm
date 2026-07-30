// Package dashscope implements ChatModel for Alibaba DashScope native text-generation API.
package dashscope

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LingByte/lingllm/metrics"
	"github.com/LingByte/lingllm/protocol"
)

const (
	defaultBaseURL = "https://dashscope.aliyuncs.com"
	generationPath = "/api/v1/services/aigc/text-generation/generation"
)

// Config configures the DashScope native client.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// Client implements protocol.ChatModel for DashScope generation API.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient constructs a DashScope client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("dashscope api key is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{cfg: cfg, httpClient: client}, nil
}

func (c *Client) Name() string { return "dashscope" }

func init() {
	protocol.RegisterFactory(protocol.ProviderDashScope, func(cfg protocol.ClientConfig) (protocol.ChatModel, error) {
		return NewClient(Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL})
	})
}

type dsMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type dsRequest struct {
	Model string `json:"model"`
	Input struct {
		Messages []dsMessage `json:"messages"`
	} `json:"input"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type dsResponse struct {
	RequestID string `json:"request_id"`
	Output    struct {
		Text         string `json:"text"`
		FinishReason string `json:"finish_reason"`
		Choices      []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func buildRequest(req protocol.ChatRequest) dsRequest {
	var r dsRequest
	r.Model = req.Model
	r.Input.Messages = make([]dsMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		r.Input.Messages = append(r.Input.Messages, dsMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	params := map[string]any{
		"result_format": "message",
	}
	if req.MaxTokens > 0 {
		params["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		params["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		params["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		params["stop"] = req.Stop
	}
	if req.Metadata != nil && req.Metadata["enable_thinking"] == "true" {
		params["enable_thinking"] = true
	}
	r.Parameters = params
	return r
}

func (c *Client) endpoint() string {
	return c.cfg.BaseURL + generationPath
}

// Chat calls the native generation endpoint.
func (c *Client) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	body, err := json.Marshal(buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal dashscope payload: %w", err)
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build dashscope request: %w", err)
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call dashscope: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read dashscope response: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("dashscope http %d: %s", httpResp.StatusCode, string(respBody))
	}

	var raw dsResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("decode dashscope response: %w", err)
	}
	if raw.Code != "" && raw.Code != "Success" {
		return nil, fmt.Errorf("dashscope error %s: %s", raw.Code, raw.Message)
	}

	content, finish := extractContent(raw)
	now := time.Now()
	total := raw.Usage.TotalTokens
	if total == 0 {
		total = raw.Usage.InputTokens + raw.Usage.OutputTokens
	}
	resp := &protocol.ChatResponse{
		ID:        raw.RequestID,
		Model:     req.Model,
		CreatedAt: now,
		Choices: []protocol.Choice{{
			Index: 0,
			Message: protocol.Message{
				Role:    protocol.RoleAssistant,
				Content: content,
			},
			FinishReason: finish,
		}},
		Usage: protocol.TokenUsage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      total,
		},
	}
	resp.Metrics = metrics.CallMetrics{
		Provider:         c.Name(),
		Model:            req.Model,
		StartAt:          start,
		FirstAt:          now,
		EndAt:            now,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		Chunks:           1,
		Bytes:            len(respBody),
		RequestBytes:     len(body),
		ResponseBytes:    len(respBody),
		HTTPStatus:       httpResp.StatusCode,
	}
	return resp, nil
}

func extractContent(raw dsResponse) (content, finish string) {
	if len(raw.Output.Choices) > 0 {
		ch := raw.Output.Choices[0]
		content = ch.Message.Content
		finish = ch.FinishReason
		return content, finish
	}
	return raw.Output.Text, raw.Output.FinishReason
}

// StreamChat enables SSE via X-DashScope-SSE header.
func (c *Client) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	payload := buildRequest(req)
	payload.Parameters["incremental_output"] = true

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal dashscope payload: %w", err)
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build dashscope request: %w", err)
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("X-DashScope-SSE", "enable")
	reqHTTP.Header.Set("Accept", "text/event-stream")

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call dashscope: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("dashscope http %d: %s", httpResp.StatusCode, string(b))
	}

	return &dsStream{
		body:         httpResp.Body,
		startAt:      start,
		model:        req.Model,
		httpStatus:   httpResp.StatusCode,
		requestBytes: len(body),
	}, nil
}

type dsStream struct {
	body          io.ReadCloser
	startAt       time.Time
	firstAt       time.Time
	endAt         time.Time
	model         string
	usage         protocol.TokenUsage
	chunks        int
	bytes         int
	responseBytes int
	requestBytes  int
	httpStatus    int
}

func (s *dsStream) Recv() (*protocol.ChatStreamChunk, error) {
	for {
		line, err := readLine(s.body, &s.bytes)
		if err != nil {
			if err == io.EOF {
				s.endAt = time.Now()
			}
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				s.endAt = time.Now()
				return nil, io.EOF
			}
			continue
		}

		var raw dsResponse
		if err := json.Unmarshal([]byte(payload), &raw); err != nil {
			return nil, fmt.Errorf("decode dashscope stream chunk: %w", err)
		}
		if raw.Code != "" && raw.Code != "Success" {
			return nil, fmt.Errorf("dashscope error %s: %s", raw.Code, raw.Message)
		}
		total := raw.Usage.TotalTokens
		if total == 0 {
			total = raw.Usage.InputTokens + raw.Usage.OutputTokens
		}
		if total > 0 {
			s.usage = protocol.TokenUsage{
				PromptTokens:     raw.Usage.InputTokens,
				CompletionTokens: raw.Usage.OutputTokens,
				TotalTokens:      total,
			}
		}
		content, finish := extractContent(raw)
		if content == "" && finish == "" {
			continue
		}
		if s.firstAt.IsZero() {
			s.firstAt = time.Now()
		}
		s.chunks++
		s.responseBytes += len(payload)
		chunk := &protocol.ChatStreamChunk{
			Index:        0,
			Role:         protocol.RoleAssistant,
			Delta:        content,
			FinishReason: finish,
		}
		if finish != "" && finish != "null" {
			s.endAt = time.Now()
		}
		return chunk, nil
	}
}

func (s *dsStream) Close() error {
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}

func (s *dsStream) Metrics() metrics.CallMetrics {
	return metrics.CallMetrics{
		Provider:         "dashscope",
		Model:            s.model,
		StartAt:          s.startAt,
		FirstAt:          s.firstAt,
		EndAt:            s.endAt,
		Bytes:            s.bytes,
		Chunks:           s.chunks,
		RequestBytes:     s.requestBytes,
		ResponseBytes:    s.responseBytes,
		HTTPStatus:       s.httpStatus,
		PromptTokens:     s.usage.PromptTokens,
		CompletionTokens: s.usage.CompletionTokens,
		TotalTokens:      s.usage.TotalTokens,
	}
}

func readLine(r io.Reader, counter *int) (string, error) {
	var buf [1]byte
	var line strings.Builder
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			*counter += n
			if buf[0] == '\n' {
				return line.String(), nil
			}
			line.WriteByte(buf[0])
		}
		if err != nil {
			if err == io.EOF && line.Len() > 0 {
				return line.String(), io.EOF
			}
			return "", err
		}
	}
}
