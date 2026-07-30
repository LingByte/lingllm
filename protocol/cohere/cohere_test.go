package cohere

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
	if c.cfg.BaseURL != "https://example" || c.Name() != "cohere" {
		t.Fatalf("cfg=%+v", c.cfg)
	}
	c2, _ := NewClient(Config{APIKey: "k"})
	if c2.cfg.BaseURL != defaultBaseURL {
		t.Fatalf("default=%q", c2.cfg.BaseURL)
	}
}

func TestToMessagesAndBuildRequest(t *testing.T) {
	msgs := toMessages([]protocol.Message{
		{Role: protocol.RoleSystem, Content: "s"},
		{Role: protocol.RoleAssistant, Content: "a"},
		{Role: protocol.RoleTool, Content: "t"},
		{Role: protocol.RoleUser, Content: "u"},
	})
	if msgs[0].Role != "system" || msgs[1].Role != "assistant" || msgs[2].Role != "user" || msgs[3].Role != "user" {
		t.Fatalf("roles=%+v", msgs)
	}
	req := buildRequest(protocol.ChatRequest{
		Model:     "command-r",
		Messages:  []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
		MaxTokens: 8, Temperature: 0.2, TopP: 0.5, Stop: []string{"X"},
	}, true)
	if !req.Stream || req.MaxTokens != 8 || req.P != 0.5 || len(req.Stop) != 1 {
		t.Fatalf("req=%+v", req)
	}
}

func TestTokenUsageFallback(t *testing.T) {
	u := (chatResponse{Usage: struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
		BilledUnits struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"billed_units"`
	}{BilledUnits: struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}{InputTokens: 4, OutputTokens: 6}}}).tokenUsage()
	if u.TotalTokens != 10 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/chat" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ck" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["max_tokens"] == nil || body["temperature"] == nil || body["p"] == nil {
			t.Fatalf("body=%#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":            "id-1",
			"finish_reason": "COMPLETE",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]string{
					{"type": "thinking", "text": "ignore"},
					{"type": "text", "text": "hello cohere"},
				},
			},
			"usage": map[string]any{
				"tokens": map[string]int{"input_tokens": 1, "output_tokens": 2},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "ck", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Chat(context.Background(), *protocol.NewChatRequest("command-r-plus", protocol.UserMessage("hi")).
		WithMaxTokens(16).WithTemperature(0.1).WithTopP(0.8).WithStop("Z"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstContent() != "hello cohere" {
		t.Fatalf("content=%q", resp.FirstContent())
	}
	if resp.Usage.TotalTokens != 3 {
		t.Fatalf("tokens=%d", resp.Usage.TotalTokens)
	}
}

func TestChatErrors(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k"})
	if _, err := client.Chat(context.Background(), protocol.ChatRequest{}); err == nil {
		t.Fatal("expected validation error")
	}
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 400)
	}))
	defer badStatus.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: badStatus.URL})
	if _, err := client.Chat(context.Background(), *protocol.NewChatRequest("m", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected http error")
	}
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	defer badJSON.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: badJSON.URL})
	if _, err := client.Chat(context.Background(), *protocol.NewChatRequest("m", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: ping\n")
		_, _ = io.WriteString(w, "data: \n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content-delta\",\"delta\":{\"message\":{\"content\":{\"text\":\"\"}}},\"usage\":{\"tokens\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_delta\",\"delta\":{\"message\":{\"content\":{\"text\":\"hi\"}}}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message-end\",\"finish_reason\":\"COMPLETE\"}\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "k", BaseURL: server.URL})
	stream, err := client.StreamChat(context.Background(), *protocol.NewChatRequest("command-r", protocol.UserMessage("hi")))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	chunk, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Delta != "hi" {
		t.Fatalf("delta=%q", chunk.Delta)
	}
	end, err := stream.Recv()
	if err != nil || end.FinishReason != "COMPLETE" {
		t.Fatalf("end=%+v err=%v", end, err)
	}
	m := stream.Metrics()
	if m.Provider != "cohere" || m.TotalTokens != 2 {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestStreamChatMessageEndEOF(t *testing.T) {
	s := &cohereStream{body: io.NopCloser(strings.NewReader("data: {\"type\":\"message_end\"}\n\n"))}
	if _, err := s.Recv(); err != io.EOF {
		t.Fatalf("want EOF got %v", err)
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
	if _, err := client.StreamChat(context.Background(), *protocol.NewChatRequest("m", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected http error")
	}
	s := &cohereStream{body: io.NopCloser(strings.NewReader("data: bad\n\n"))}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected decode error")
	}
	s = &cohereStream{}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = &cohereStream{body: io.NopCloser(strings.NewReader("partial"))}
	line, err := readLine(s.body, &s.bytes)
	if line != "partial" || err != io.EOF {
		t.Fatalf("line=%q err=%v", line, err)
	}
	s = &cohereStream{body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}
	if _, err := s.Recv(); err != io.EOF {
		t.Fatalf("want EOF got %v", err)
	}
	now := time.Now()
	m := (&cohereStream{startAt: now, model: "m"}).Metrics()
	if m.Provider != "cohere" {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewClient(protocol.ClientConfig{
		Provider: protocol.ProviderCohere,
		APIKey:   "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "cohere" {
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
	req := *protocol.NewChatRequest("command-r", protocol.UserMessage("hi"))
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
	s := &cohereStream{body: boomReader{}}
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
