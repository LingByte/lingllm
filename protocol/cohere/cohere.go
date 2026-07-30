// Package cohere implements ChatModel for Cohere Chat API v2.
package cohere

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

const defaultBaseURL = "https://api.cohere.com"

// Config configures the Cohere chat client.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// Client implements protocol.ChatModel for Cohere v2 chat.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient constructs a Cohere client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("cohere api key is required")
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

func (c *Client) Name() string { return "cohere" }

func init() {
	protocol.RegisterFactory(protocol.ProviderCohere, func(cfg protocol.ClientConfig) (protocol.ChatModel, error) {
		return NewClient(Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL})
	})
}

type cohereMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string          `json:"model"`
	Messages    []cohereMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
	P           float32         `json:"p,omitempty"`
	Stop        []string        `json:"stop_sequences,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type chatResponse struct {
	ID           string `json:"id"`
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Usage struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
		BilledUnits struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"billed_units"`
	} `json:"usage"`
}

func toMessages(msgs []protocol.Message) []cohereMessage {
	out := make([]cohereMessage, 0, len(msgs))
	for _, m := range msgs {
		role := string(m.Role)
		switch m.Role {
		case protocol.RoleSystem:
			role = "system"
		case protocol.RoleAssistant:
			role = "assistant"
		case protocol.RoleTool:
			role = "user"
		default:
			role = "user"
		}
		out = append(out, cohereMessage{Role: role, Content: m.Content})
	}
	return out
}

func buildRequest(req protocol.ChatRequest, stream bool) chatRequest {
	r := chatRequest{
		Model:    req.Model,
		Messages: toMessages(req.Messages),
		Stream:   stream,
	}
	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		r.Temperature = req.Temperature
	}
	if req.TopP > 0 {
		r.P = req.TopP
	}
	if len(req.Stop) > 0 {
		r.Stop = req.Stop
	}
	return r
}

func (r chatResponse) contentText() string {
	var b strings.Builder
	for _, c := range r.Message.Content {
		if c.Type == "text" || c.Type == "" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func (r chatResponse) tokenUsage() protocol.TokenUsage {
	in := r.Usage.Tokens.InputTokens
	out := r.Usage.Tokens.OutputTokens
	if in == 0 && out == 0 {
		in = r.Usage.BilledUnits.InputTokens
		out = r.Usage.BilledUnits.OutputTokens
	}
	return protocol.TokenUsage{
		PromptTokens:     in,
		CompletionTokens: out,
		TotalTokens:      in + out,
	}
}

// Chat calls POST /v2/chat.
func (c *Client) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	body, err := json.Marshal(buildRequest(req, false))
	if err != nil {
		return nil, fmt.Errorf("marshal cohere payload: %w", err)
	}

	endpoint := c.cfg.BaseURL + "/v2/chat"
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build cohere request: %w", err)
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("Accept", "application/json")

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call cohere: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read cohere response: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("cohere http %d: %s", httpResp.StatusCode, string(respBody))
	}

	var raw chatResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("decode cohere response: %w", err)
	}

	usage := raw.tokenUsage()
	now := time.Now()
	resp := &protocol.ChatResponse{
		ID:        raw.ID,
		Model:     req.Model,
		CreatedAt: now,
		Choices: []protocol.Choice{{
			Index: 0,
			Message: protocol.Message{
				Role:    protocol.RoleAssistant,
				Content: raw.contentText(),
			},
			FinishReason: raw.FinishReason,
		}},
		Usage: usage,
	}
	resp.Metrics = metrics.CallMetrics{
		Provider:         c.Name(),
		Model:            req.Model,
		StartAt:          start,
		FirstAt:          now,
		EndAt:            now,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Chunks:           1,
		Bytes:            len(respBody),
		RequestBytes:     len(body),
		ResponseBytes:    len(respBody),
		HTTPStatus:       httpResp.StatusCode,
	}
	return resp, nil
}

// StreamChat streams Cohere v2 chat events.
func (c *Client) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	body, err := json.Marshal(buildRequest(req, true))
	if err != nil {
		return nil, fmt.Errorf("marshal cohere payload: %w", err)
	}

	endpoint := c.cfg.BaseURL + "/v2/chat"
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build cohere request: %w", err)
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("Accept", "text/event-stream")

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call cohere: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("cohere http %d: %s", httpResp.StatusCode, string(b))
	}

	return &cohereStream{
		body:         httpResp.Body,
		startAt:      start,
		model:        req.Model,
		httpStatus:   httpResp.StatusCode,
		requestBytes: len(body),
	}, nil
}

type cohereStream struct {
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

func (s *cohereStream) Recv() (*protocol.ChatStreamChunk, error) {
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

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Message struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
			Usage        struct {
				Tokens struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("decode cohere stream chunk: %w", err)
		}

		in := event.Usage.Tokens.InputTokens
		out := event.Usage.Tokens.OutputTokens
		if in+out > 0 {
			s.usage = protocol.TokenUsage{
				PromptTokens:     in,
				CompletionTokens: out,
				TotalTokens:      in + out,
			}
		}

		switch event.Type {
		case "content-delta", "content_delta":
			text := event.Delta.Message.Content.Text
			if text == "" {
				continue
			}
			if s.firstAt.IsZero() {
				s.firstAt = time.Now()
			}
			s.chunks++
			s.responseBytes += len(payload)
			return &protocol.ChatStreamChunk{
				Index: 0,
				Role:  protocol.RoleAssistant,
				Delta: text,
			}, nil
		case "message-end", "message_end":
			s.endAt = time.Now()
			if event.FinishReason != "" {
				return &protocol.ChatStreamChunk{
					Role:         protocol.RoleAssistant,
					FinishReason: event.FinishReason,
				}, nil
			}
			return nil, io.EOF
		default:
			continue
		}
	}
}

func (s *cohereStream) Close() error {
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}

func (s *cohereStream) Metrics() metrics.CallMetrics {
	return metrics.CallMetrics{
		Provider:         "cohere",
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
