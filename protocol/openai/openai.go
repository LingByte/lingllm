package openai

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

const defaultBaseURL = "https://api.openai.com/v1"

// Config configures the OpenAI-compatible chat client.
type Config struct {
	APIKey       string
	BaseURL      string
	HTTPClient   *http.Client
	Organization string
	Project      string
}

// Client implements llm.ChatModel for OpenAI's /chat/completions endpoint.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient constructs a client with sane defaults.
func NewClient(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai api key is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{cfg: cfg, httpClient: client}, nil
}

func (c *Client) Name() string { return "openai" }

func init() {
	protocol.RegisterFactory(protocol.ProviderOpenAI, func(cfg protocol.ClientConfig) (protocol.ChatModel, error) {
		return NewClient(Config{
			APIKey:       cfg.APIKey,
			BaseURL:      cfg.BaseURL,
			Organization: cfg.Organization,
			Project:      cfg.Project,
		})
	})
}

// Chat executes a chat completion request against OpenAI.
func (c *Client) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	payload := struct {
		Model       string          `json:"model"`
		Messages    []openAIMessage `json:"messages"`
		MaxTokens   int             `json:"max_tokens,omitempty"`
		Temperature float32         `json:"temperature,omitempty"`
		TopP        float32         `json:"top_p,omitempty"`
		Stop        []string        `json:"stop,omitempty"`
	}{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal openai payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/chat/completions", c.cfg.BaseURL)
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build openai request: %w", err)
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")
	if c.cfg.Organization != "" {
		reqHTTP.Header.Set("OpenAI-Organization", c.cfg.Organization)
	}
	if c.cfg.Project != "" {
		reqHTTP.Header.Set("OpenAI-Project", c.cfg.Project)
	}

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call openai: %w", err)
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai response: %w", err)
	}
	if httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai http %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var raw openAIResponse
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	chatResp := raw.toChatResponse()
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

// StreamChat uses SSE stream from /chat/completions with stream=true.
func (c *Client) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	payload := struct {
		Model       string          `json:"model"`
		Messages    []openAIMessage `json:"messages"`
		MaxTokens   int             `json:"max_tokens,omitempty"`
		Temperature float32         `json:"temperature,omitempty"`
		TopP        float32         `json:"top_p,omitempty"`
		Stop        []string        `json:"stop,omitempty"`
		Stream      bool            `json:"stream"`
	}{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop,
		Stream:      true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal openai payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/chat/completions", c.cfg.BaseURL)
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build openai request: %w", err)
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("Accept", "text/event-stream")
	if c.cfg.Organization != "" {
		reqHTTP.Header.Set("OpenAI-Organization", c.cfg.Organization)
	}
	if c.cfg.Project != "" {
		reqHTTP.Header.Set("OpenAI-Project", c.cfg.Project)
	}

	httpResp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call openai: %w", err)
	}

	if httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("openai http %d: %s", httpResp.StatusCode, string(b))
	}

	stream := &openAIStream{
		body:         httpResp.Body,
		startAt:      start,
		model:        req.Model,
		httpStatus:   httpResp.StatusCode,
		requestBytes: len(body),
	}
	return stream, nil
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

// openAIStream implements llm.ChatStream for SSE responses.
type openAIStream struct {
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

func (s *openAIStream) Recv() (*protocol.ChatStreamChunk, error) {
	for {
		line, err := s.readLine()
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
			Usage openAIUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &raw); err != nil {
			return nil, fmt.Errorf("decode openai stream chunk: %w", err)
		}
		if s.firstAt.IsZero() {
			s.firstAt = time.Now()
		}
		s.chunks++
		s.bytes += len(payload)
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

func (s *openAIStream) readLine() (string, error) {
	var buf [1]byte
	var line strings.Builder
	for {
		n, err := s.body.Read(buf[:])
		if n > 0 {
			s.bytes += n
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

func (s *openAIStream) Close() error {
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}

func (s *openAIStream) Metrics() metrics.CallMetrics {
	return metrics.CallMetrics{
		Provider:         "openai",
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

func toOpenAIMessages(msgs []protocol.Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(msgs))
	for _, m := range msgs {
		role := string(m.Role)
		switch m.Role {
		case protocol.RoleTool:
			role = "tool"
		}
		out = append(out, openAIMessage{Role: role, Content: m.Content})
	}
	return out
}

func (r openAIResponse) toChatResponse() *protocol.ChatResponse {
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
		Model:     r.Model,
		CreatedAt: time.Unix(r.Created, 0),
		Choices:   choices,
		Usage: protocol.TokenUsage{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.TotalTokens,
		},
	}
}