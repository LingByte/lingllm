package dashscope

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol"
)

func TestNewClientRequiresAPIKey(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(Config{APIKey: "k", BaseURL: "https://example/"})
	if err != nil {
		t.Fatal(err)
	}
	if c.cfg.BaseURL != "https://example" || c.Name() != "dashscope" {
		t.Fatalf("cfg=%+v", c.cfg)
	}
	c2, _ := NewClient(Config{APIKey: "k"})
	if c2.cfg.BaseURL != defaultBaseURL {
		t.Fatalf("default=%q", c2.cfg.BaseURL)
	}
}

func TestBuildRequest(t *testing.T) {
	req := buildRequest(*protocol.NewChatRequest("qwen-plus",
		protocol.SystemMessage("s"),
		protocol.UserMessage("u"),
	).WithMaxTokens(8).WithTemperature(0.3).WithTopP(0.7).WithStop("X").WithMetadata("enable_thinking", "true"))
	if req.Parameters["enable_thinking"] != true {
		t.Fatalf("params=%#v", req.Parameters)
	}
	if req.Parameters["max_tokens"] == nil || req.Parameters["temperature"] == nil {
		t.Fatalf("missing params %#v", req.Parameters)
	}
}

func TestChatEnableThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ds-key" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		params, _ := body["parameters"].(map[string]any)
		if params["enable_thinking"] != true {
			t.Fatalf("enable_thinking=%v", params["enable_thinking"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": "req-1",
			"output": map[string]any{
				"choices": []map[string]any{{
					"finish_reason": "stop",
					"message": map[string]string{
						"role":    "assistant",
						"content": "hello qwen",
					},
				}},
			},
			"usage": map[string]int{"input_tokens": 1, "output_tokens": 2, "total_tokens": 3},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "ds-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	req := protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi")).
		WithMetadata("enable_thinking", "true")
	resp, err := client.Chat(context.Background(), *req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstContent() != "hello qwen" {
		t.Fatalf("content=%q", resp.FirstContent())
	}
}

func TestChatTextFallbackAndErrors(t *testing.T) {
	textOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"text": "plain", "finish_reason": "stop"},
			"usage":  map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer textOnly.Close()
	client, _ := NewClient(Config{APIKey: "k", BaseURL: textOnly.URL})
	resp, err := client.Chat(context.Background(), *protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi")))
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstContent() != "plain" || resp.Usage.TotalTokens != 2 {
		t.Fatalf("resp=%+v", resp)
	}

	apiErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "InvalidParameter", "message": "bad"})
	}))
	defer apiErr.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: apiErr.URL})
	if _, err := client.Chat(context.Background(), *protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected api error")
	}

	client, _ = NewClient(Config{APIKey: "k"})
	if _, err := client.Chat(context.Background(), protocol.ChatRequest{}); err == nil {
		t.Fatal("expected validation error")
	}

	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 400)
	}))
	defer badStatus.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: badStatus.URL})
	if _, err := client.Chat(context.Background(), *protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected http error")
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	defer badJSON.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: badJSON.URL})
	if _, err := client.Chat(context.Background(), *protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-DashScope-SSE") != "enable" {
			t.Fatal("missing SSE header")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		params, _ := body["parameters"].(map[string]any)
		if params["incremental_output"] != true {
			t.Fatalf("incremental_output=%v", params["incremental_output"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "id: 1\n")
		_, _ = io.WriteString(w, "event: result\n")
		_, _ = io.WriteString(w, ": comment\n")
		_, _ = io.WriteString(w, "data: \n\n")
		_, _ = io.WriteString(w, "data: {\"output\":{\"choices\":[{\"message\":{\"content\":\"\"}}]},\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}\n\n")
		_, _ = io.WriteString(w, "data: {\"output\":{\"choices\":[{\"finish_reason\":\"stop\",\"message\":{\"content\":\"hi\"}}]}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "k", BaseURL: server.URL})
	stream, err := client.StreamChat(context.Background(), *protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi")))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	chunk, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Delta != "hi" || chunk.FinishReason != "stop" {
		t.Fatalf("chunk=%+v", chunk)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("want EOF got %v", err)
	}
	m := stream.Metrics()
	if m.Provider != "dashscope" || m.TotalTokens != 2 {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestStreamChatErrors(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k"})
	if _, err := client.StreamChat(context.Background(), protocol.ChatRequest{}); err == nil {
		t.Fatal("expected validation error")
	}
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	}))
	defer badStatus.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: badStatus.URL})
	if _, err := client.StreamChat(context.Background(), *protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected http error")
	}

	s := &dsStream{body: io.NopCloser(strings.NewReader("data: {\"code\":\"Err\",\"message\":\"x\"}\n\n"))}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected stream api error")
	}
	s = &dsStream{body: io.NopCloser(strings.NewReader("data: bad\n\n"))}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected decode error")
	}
	s = &dsStream{}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = &dsStream{body: io.NopCloser(strings.NewReader("partial"))}
	line, err := readLine(s.body, &s.bytes)
	if line != "partial" || err != io.EOF {
		t.Fatalf("line=%q err=%v", line, err)
	}
	now := time.Now()
	m := (&dsStream{startAt: now, model: "qwen"}).Metrics()
	if m.Provider != "dashscope" {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewClient(protocol.ClientConfig{
		Provider: protocol.ProviderDashScope,
		APIKey:   "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "dashscope" {
		t.Fatalf("name=%q", client.Name())
	}
}

type failRoundTripper struct{}

func (failRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestTransportErrors(t *testing.T) {
	client, _ := NewClient(Config{
		APIKey: "k", HTTPClient: &http.Client{Transport: failRoundTripper{}},
	})
	req := *protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi"))
	if _, err := client.Chat(context.Background(), req); err == nil {
		t.Fatal("expected chat transport error")
	}
	if _, err := client.StreamChat(context.Background(), req); err == nil {
		t.Fatal("expected stream transport error")
	}
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (errBody) Close() error             { return nil }

type errBodyRoundTripper struct{}

func (errBodyRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: errBody{}, Header: make(http.Header)}, nil
}

func TestChatReadBodyError(t *testing.T) {
	client, _ := NewClient(Config{
		APIKey: "k", BaseURL: "https://example.invalid",
		HTTPClient: &http.Client{Transport: errBodyRoundTripper{}},
	})
	if _, err := client.Chat(context.Background(), *protocol.NewChatRequest("m", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected read error")
	}
}

type boomReader struct{}

func (boomReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (boomReader) Close() error             { return nil }

func TestStreamRecvNonEOFReadError(t *testing.T) {
	s := &dsStream{body: boomReader{}}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected read error")
	}
}

func TestInvalidURL(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "http://example.com/" + string([]byte{0x7f})})
	req := *protocol.NewChatRequest("m", protocol.UserMessage("hi"))
	_, _ = client.Chat(context.Background(), req)
	_, _ = client.StreamChat(context.Background(), req)
}
