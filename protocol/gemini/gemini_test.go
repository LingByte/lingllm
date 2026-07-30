package gemini

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
	if c.cfg.BaseURL != "https://example" {
		t.Fatalf("baseURL=%q", c.cfg.BaseURL)
	}
	c2, _ := NewClient(Config{APIKey: "k"})
	if c2.cfg.BaseURL != defaultBaseURL {
		t.Fatalf("default=%q", c2.cfg.BaseURL)
	}
}

func TestBuildPayload(t *testing.T) {
	payload := buildPayload(protocol.ChatRequest{
		Model: "m",
		Messages: []protocol.Message{
			{Role: protocol.RoleSystem, Content: "sys"},
			{Role: protocol.RoleAssistant, Content: "prev"},
			{Role: protocol.RoleUser, Content: "hi"},
			{Role: protocol.RoleTool, Content: "tool"},
		},
		MaxTokens:   10,
		Temperature: 0.5,
		TopP:        0.8,
		Stop:        []string{"END"},
	})
	if payload.SystemInstruction == nil || payload.GenerationConfig == nil {
		t.Fatal("expected system + generation config")
	}
	if len(payload.Contents) != 3 {
		t.Fatalf("contents=%d", len(payload.Contents))
	}
	if payload.Contents[0].Role != "model" {
		t.Fatalf("role=%q", payload.Contents[0].Role)
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "gkey" {
			t.Fatalf("missing api key header")
		}
		if !strings.Contains(r.URL.Path, "/models/gemini-2.0-flash:generateContent") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["systemInstruction"]; !ok {
			t.Fatal("expected systemInstruction")
		}
		if _, ok := body["generationConfig"]; !ok {
			t.Fatal("expected generationConfig")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"role":  "model",
					"parts": []map[string]string{{"text": "hello "}, {"text": "gemini"}},
				},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]int{
				"promptTokenCount": 1, "candidatesTokenCount": 2, "totalTokenCount": 3,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "gkey", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Chat(context.Background(), *protocol.NewChatRequest(
		"gemini-2.0-flash",
		protocol.SystemMessage("be brief"),
		protocol.UserMessage("hi"),
	).WithMaxTokens(32).WithTemperature(0.2).WithTopP(0.9).WithStop("X"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstContent() != "hello gemini" {
		t.Fatalf("content=%q", resp.FirstContent())
	}
}

func TestChatEmptyCandidatesAndErrors(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{}})
	}))
	defer empty.Close()
	client, _ := NewClient(Config{APIKey: "k", BaseURL: empty.URL})
	resp, err := client.Chat(context.Background(), *protocol.NewChatRequest("m", protocol.UserMessage("hi")))
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstContent() != "" {
		t.Fatalf("want empty content, got %q", resp.FirstContent())
	}

	client, _ = NewClient(Config{APIKey: "k", BaseURL: "https://example"})
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
		if !strings.Contains(r.URL.RawQuery, "alt=sse") {
			t.Fatalf("missing alt=sse")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: ping\n")
		_, _ = io.WriteString(w, "data: \n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\"}]},\"finishReason\":\"\"}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "k", BaseURL: server.URL})
	stream, err := client.StreamChat(context.Background(), *protocol.NewChatRequest("gemini-2.0-flash", protocol.UserMessage("hi")))
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
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("want EOF got %v", err)
	}
	m := stream.Metrics()
	if m.Provider != "gemini" || m.TotalTokens != 2 {
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
	if _, err := client.StreamChat(context.Background(), *protocol.NewChatRequest("m", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected http error")
	}

	s := &geminiStream{body: io.NopCloser(strings.NewReader("data: bad\n\n"))}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected decode error")
	}
	s = &geminiStream{}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = &geminiStream{body: io.NopCloser(strings.NewReader("partial"))}
	line, err := readLine(s.body, &s.bytes)
	if line != "partial" || err != io.EOF {
		t.Fatalf("line=%q err=%v", line, err)
	}
	now := time.Now()
	m := (&geminiStream{startAt: now, model: "m"}).Metrics()
	if m.Provider != "gemini" {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewClient(protocol.ClientConfig{
		Provider: protocol.ProviderGemini,
		APIKey:   "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "gemini" {
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
	req := *protocol.NewChatRequest("m", protocol.UserMessage("hi"))
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
	s := &geminiStream{body: boomReader{}}
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
