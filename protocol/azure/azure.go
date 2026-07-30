// Package azure implements ChatModel for Azure OpenAI deployments.
package azure

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

const defaultAPIVersion = "2024-10-21"

// Config configures the Azure OpenAI client.
type Config struct {
	APIKey     string
	BaseURL    string // e.g. https://{resource}.openai.azure.com
	APIVersion string
	Deployment string // optional default deployment; ChatRequest.Model overrides when set
	HTTPClient *http.Client
}

// Client implements protocol.ChatModel for Azure OpenAI chat completions.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient constructs an Azure OpenAI client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("azure api key is required")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("azure base url is required")
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAPIVersion
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{cfg: cfg, httpClient: client}, nil
}

func (c *Client) Name() string { return "azure" }

func init() {
	protocol.RegisterFactory(protocol.ProviderAzure, func(cfg protocol.ClientConfig) (protocol.ChatModel, error) {
		return NewClient(Config{
			APIKey:     cfg.APIKey,
			BaseURL:    cfg.BaseURL,
			APIVersion: cfg.APIVersion,
			Deployment: cfg.Deployment,
		})
	})
}

func (c *Client) deployment(req protocol.ChatRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return c.cfg.Deployment
}

func (c *Client) endpoint(deployment string) string {
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		c.cfg.BaseURL, deployment, c.cfg.APIVersion)
}

// Chat executes a non-streaming chat completion.
func (c *Client) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	start := time.Now()
	if req.Model == "" && c.cfg.Deployment != "" {
		req.Model = c.cfg.Deployment
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	deployment := c.deployment(req)

	payload := map[string]any{
		"messages": toMessages(req.Messages),
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		payload["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		payload["stop"] = req.Stop
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal azure payload: %w", err)
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(deployment), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build azure request: %w", err)
	}
	reqHTTP.Header.Set("api-key", c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call azure: %w", err)
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read azure response: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("azure http %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var raw azureResponse
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, fmt.Errorf("decode azure response: %w", err)
	}
	chatResp := raw.toChatResponse(deployment)
	now := time.Now()
	chatResp.Metrics = metrics.CallMetrics{
		Provider:         c.Name(),
		Model:            chatResp.Model,
		StartAt:          start,
		EndAt:            now,
		FirstAt:          now,
		PromptTokens:     chatResp.Usage.PromptTokens,
		CompletionTokens: chatResp.Usage.CompletionTokens,
		TotalTokens:      chatResp.Usage.TotalTokens,
		Chunks:           1,
		Bytes:            len(bodyBytes),
		RequestBytes:     len(body),
		ResponseBytes:    len(bodyBytes),
		HTTPStatus:       httpResp.StatusCode,
	}
	return chatResp, nil
}

// StreamChat streams chat completion deltas via SSE.
func (c *Client) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	start := time.Now()
	if req.Model == "" && c.cfg.Deployment != "" {
		req.Model = c.cfg.Deployment
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	deployment := c.deployment(req)

	payload := map[string]any{
		"messages": toMessages(req.Messages),
		"stream":   true,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		payload["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		payload["stop"] = req.Stop
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal azure payload: %w", err)
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(deployment), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build azure request: %w", err)
	}
	reqHTTP.Header.Set("api-key", c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("Accept", "text/event-stream")

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call azure: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("azure http %d: %s", httpResp.StatusCode, string(b))
	}

	return &azureStream{
		body:         httpResp.Body,
		startAt:      start,
		model:        deployment,
		httpStatus:   httpResp.StatusCode,
		requestBytes: len(body),
	}, nil
}

type azureMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type azureResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int          `json:"index"`
		Message      azureMessage `json:"message"`
		FinishReason string       `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func toMessages(msgs []protocol.Message) []azureMessage {
	out := make([]azureMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, azureMessage{Role: string(m.Role), Content: m.Content})
	}
	return out
}

func (r azureResponse) toChatResponse(fallbackModel string) *protocol.ChatResponse {
	model := r.Model
	if model == "" {
		model = fallbackModel
	}
	choices := make([]protocol.Choice, 0, len(r.Choices))
	for _, ch := range r.Choices {
		choices = append(choices, protocol.Choice{
			Index: ch.Index,
			Message: protocol.Message{
				Role:    protocol.MessageRole(ch.Message.Role),
				Content: ch.Message.Content,
			},
			FinishReason: ch.FinishReason,
		})
	}
	return &protocol.ChatResponse{
		ID:        r.ID,
		Model:     model,
		CreatedAt: time.Unix(r.Created, 0),
		Choices:   choices,
		Usage: protocol.TokenUsage{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.TotalTokens,
		},
	}
}

type azureStream struct {
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

func (s *azureStream) Recv() (*protocol.ChatStreamChunk, error) {
	for {
		line, err := readLine(s.body, &s.bytes)
		if err != nil {
			if err == io.EOF {
				s.endAt = time.Now()
			}
			return nil, err
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			s.endAt = time.Now()
			return nil, io.EOF
		}
		var raw struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &raw); err != nil {
			return nil, fmt.Errorf("decode azure stream chunk: %w", err)
		}
		if s.firstAt.IsZero() {
			s.firstAt = time.Now()
		}
		s.chunks++
		s.responseBytes += len(payload)
		if raw.Usage.TotalTokens > 0 {
			s.usage = protocol.TokenUsage{
				PromptTokens:     raw.Usage.PromptTokens,
				CompletionTokens: raw.Usage.CompletionTokens,
				TotalTokens:      raw.Usage.TotalTokens,
			}
		}
		if len(raw.Choices) == 0 {
			continue
		}
		ch := raw.Choices[0]
		return &protocol.ChatStreamChunk{
			Index:        ch.Index,
			Role:         protocol.MessageRole(ch.Delta.Role),
			Delta:        ch.Delta.Content,
			FinishReason: ch.FinishReason,
		}, nil
	}
}

func (s *azureStream) Close() error {
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}

func (s *azureStream) Metrics() metrics.CallMetrics {
	return metrics.CallMetrics{
		Provider:         "azure",
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
