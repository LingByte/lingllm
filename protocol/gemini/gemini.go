// Package gemini implements ChatModel for Google Gemini generateContent API.
package gemini

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

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// Config configures the Gemini client.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// Client implements protocol.ChatModel for Gemini.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient constructs a Gemini client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gemini api key is required")
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

func (c *Client) Name() string { return "gemini" }

func init() {
	protocol.RegisterFactory(protocol.ProviderGemini, func(cfg protocol.ClientConfig) (protocol.ChatModel, error) {
		return NewClient(Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL})
	})
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type generationConfig struct {
	Temperature     float32  `json:"temperature,omitempty"`
	TopP            float32  `json:"topP,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type generateRequest struct {
	Contents          []geminiContent   `json:"contents"`
	SystemInstruction *geminiContent    `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
			Role  string       `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func buildPayload(req protocol.ChatRequest) generateRequest {
	var systemParts []string
	contents := make([]geminiContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case protocol.RoleSystem:
			systemParts = append(systemParts, m.Content)
		case protocol.RoleAssistant:
			contents = append(contents, geminiContent{
				Role:  "model",
				Parts: []geminiPart{{Text: m.Content}},
			})
		default:
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: m.Content}},
			})
		}
	}
	payload := generateRequest{Contents: contents}
	if len(systemParts) > 0 {
		payload.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: strings.Join(systemParts, "\n")}},
		}
	}
	cfg := &generationConfig{}
	has := false
	if req.Temperature > 0 {
		cfg.Temperature = req.Temperature
		has = true
	}
	if req.TopP > 0 {
		cfg.TopP = req.TopP
		has = true
	}
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = req.MaxTokens
		has = true
	}
	if len(req.Stop) > 0 {
		cfg.StopSequences = req.Stop
		has = true
	}
	if has {
		payload.GenerationConfig = cfg
	}
	return payload
}

// Chat calls models/{model}:generateContent.
func (c *Client) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	body, err := json.Marshal(buildPayload(req))
	if err != nil {
		return nil, fmt.Errorf("marshal gemini payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent", c.cfg.BaseURL, req.Model)
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build gemini request: %w", err)
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("x-goog-api-key", c.cfg.APIKey)

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gemini response: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini http %d: %s", httpResp.StatusCode, string(respBody))
	}

	var raw generateResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}

	content := ""
	finish := ""
	if len(raw.Candidates) > 0 {
		finish = raw.Candidates[0].FinishReason
		var b strings.Builder
		for _, p := range raw.Candidates[0].Content.Parts {
			b.WriteString(p.Text)
		}
		content = b.String()
	}

	now := time.Now()
	resp := &protocol.ChatResponse{
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
			PromptTokens:     raw.UsageMetadata.PromptTokenCount,
			CompletionTokens: raw.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      raw.UsageMetadata.TotalTokenCount,
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

// StreamChat calls models/{model}:streamGenerateContent?alt=sse.
func (c *Client) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	body, err := json.Marshal(buildPayload(req))
	if err != nil {
		return nil, fmt.Errorf("marshal gemini payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", c.cfg.BaseURL, req.Model)
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build gemini request: %w", err)
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("x-goog-api-key", c.cfg.APIKey)
	reqHTTP.Header.Set("Accept", "text/event-stream")

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("gemini http %d: %s", httpResp.StatusCode, string(b))
	}

	return &geminiStream{
		body:         httpResp.Body,
		startAt:      start,
		model:        req.Model,
		httpStatus:   httpResp.StatusCode,
		requestBytes: len(body),
	}, nil
}

type geminiStream struct {
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

func (s *geminiStream) Recv() (*protocol.ChatStreamChunk, error) {
	for {
		line, err := readLine(s.body, &s.bytes)
		if err != nil {
			if err == io.EOF {
				s.endAt = time.Now()
			}
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "event:") {
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

		var raw generateResponse
		if err := json.Unmarshal([]byte(payload), &raw); err != nil {
			return nil, fmt.Errorf("decode gemini stream chunk: %w", err)
		}
		if raw.UsageMetadata.TotalTokenCount > 0 {
			s.usage = protocol.TokenUsage{
				PromptTokens:     raw.UsageMetadata.PromptTokenCount,
				CompletionTokens: raw.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      raw.UsageMetadata.TotalTokenCount,
			}
		}
		if len(raw.Candidates) == 0 {
			continue
		}
		var text strings.Builder
		for _, p := range raw.Candidates[0].Content.Parts {
			text.WriteString(p.Text)
		}
		delta := text.String()
		if delta == "" && raw.Candidates[0].FinishReason == "" {
			continue
		}
		if s.firstAt.IsZero() {
			s.firstAt = time.Now()
		}
		s.chunks++
		s.responseBytes += len(payload)
		return &protocol.ChatStreamChunk{
			Index:        0,
			Role:         protocol.RoleAssistant,
			Delta:        delta,
			FinishReason: raw.Candidates[0].FinishReason,
		}, nil
	}
}

func (s *geminiStream) Close() error {
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}

func (s *geminiStream) Metrics() metrics.CallMetrics {
	return metrics.CallMetrics{
		Provider:         "gemini",
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
