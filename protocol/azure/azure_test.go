package azure

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

func TestNewClientRequiresFields(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("expected error for missing api key")
	}
	if _, err := NewClient(Config{APIKey: "k"}); err == nil {
		t.Fatal("expected error for missing base url")
	}
}

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(Config{APIKey: "k", BaseURL: "https://example.openai.azure.com/"})
	if err != nil {
		t.Fatal(err)
	}
	if c.cfg.APIVersion != defaultAPIVersion {
		t.Fatalf("apiVersion=%q", c.cfg.APIVersion)
	}
	if strings.HasSuffix(c.cfg.BaseURL, "/") {
		t.Fatalf("baseURL not trimmed: %q", c.cfg.BaseURL)
	}
	if c.Name() != "azure" {
		t.Fatalf("name=%q", c.Name())
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "azure-key" {
			t.Fatalf("missing api-key header")
		}
		if !strings.Contains(r.URL.Path, "/openai/deployments/gpt-4o/chat/completions") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != "2024-10-21" {
			t.Fatalf("unexpected api-version: %s", r.URL.Query().Get("api-version"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["max_tokens"] == nil || body["temperature"] == nil || body["top_p"] == nil || body["stop"] == nil {
			t.Fatalf("missing optional params: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-1",
			"created": 1,
			"model":   "gpt-4o",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": "hello azure",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "azure-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	req := protocol.NewChatRequest("gpt-4o", protocol.UserMessage("hi")).
		WithMaxTokens(16).
		WithTemperature(0.2).
		WithTopP(0.9).
		WithStop("END")
	resp, err := client.Chat(context.Background(), *req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstContent() != "hello azure" {
		t.Fatalf("content=%q", resp.FirstContent())
	}
	if resp.Metrics.Provider != "azure" {
		t.Fatalf("provider=%q", resp.Metrics.Provider)
	}
}

func TestChatUsesConfiguredDeploymentWhenModelEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/openai/deployments/cfg-dep/") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "created": 1, "model": "",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{},
		})
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "k", BaseURL: server.URL, Deployment: "cfg-dep"})
	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Model != "cfg-dep" {
		t.Fatalf("model=%q", resp.Model)
	}
}

func TestStreamChatUsesConfiguredDeployment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/openai/deployments/cfg-dep/") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, _ := NewClient(Config{APIKey: "k", BaseURL: server.URL, Deployment: "cfg-dep"})
	stream, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("want EOF got %v", err)
	}
}

func TestChatHTTPAndDecodeErrors(t *testing.T) {
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer badStatus.Close()
	client, _ := NewClient(Config{APIKey: "k", BaseURL: badStatus.URL})
	_, err := client.Chat(context.Background(), *protocol.NewChatRequest("dep", protocol.UserMessage("hi")))
	if err == nil {
		t.Fatal("expected http error")
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	defer badJSON.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: badJSON.URL})
	_, err = client.Chat(context.Background(), *protocol.NewChatRequest("dep", protocol.UserMessage("hi")))
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Fatal("expected stream=true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": keep-alive\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
		_, _ = io.WriteString(w, "data: not-json\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "k", BaseURL: server.URL})
	req := protocol.NewChatRequest("dep", protocol.UserMessage("hi")).
		WithMaxTokens(8).WithTemperature(0.1).WithTopP(0.5).WithStop("X")
	stream, err := client.StreamChat(context.Background(), *req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected decode error after empty choices")
	}
}

func TestStreamChatSuccessAndMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "k", BaseURL: server.URL, Deployment: "dep"})
	stream, err := client.StreamChat(context.Background(), *protocol.NewChatRequest("dep", protocol.UserMessage("hi")))
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
		t.Fatalf("want EOF, got %v", err)
	}
	m := stream.Metrics()
	if m.Provider != "azure" || m.TotalTokens != 3 {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestStreamChatErrors(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://example.openai.azure.com"})
	_, err := client.StreamChat(context.Background(), protocol.ChatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}

	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	}))
	defer badStatus.Close()
	client, _ = NewClient(Config{APIKey: "k", BaseURL: badStatus.URL})
	_, err = client.StreamChat(context.Background(), *protocol.NewChatRequest("dep", protocol.UserMessage("hi")))
	if err == nil {
		t.Fatal("expected http error")
	}
}

func TestStreamChatMissingDeployment(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "k", BaseURL: "https://example.openai.azure.com"})
	_, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when model and deployment are empty")
	}
	_, err = client.Chat(context.Background(), protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when model and deployment are empty")
	}
	if got := (&Client{cfg: Config{}}).deployment(protocol.ChatRequest{}); got != "" {
		t.Fatalf("want empty deployment, got %q", got)
	}
}

func TestAzureStreamCloseNilAndReadLineEOF(t *testing.T) {
	s := &azureStream{}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = &azureStream{body: io.NopCloser(strings.NewReader("partial"))}
	line, err := readLine(s.body, &s.bytes)
	if line != "partial" || err != io.EOF {
		t.Fatalf("line=%q err=%v", line, err)
	}
	s = &azureStream{body: io.NopCloser(strings.NewReader(""))}
	_, err = s.Recv()
	if err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestAzureStreamMetricsDefaults(t *testing.T) {
	now := time.Now()
	s := &azureStream{startAt: now, firstAt: now, endAt: now, model: "dep", chunks: 1}
	m := s.Metrics()
	if m.Provider != "azure" || m.Model != "dep" {
		t.Fatalf("metrics=%+v", m)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewClient(protocol.ClientConfig{
		Provider:   protocol.ProviderAzure,
		APIKey:     "k",
		BaseURL:    "https://example.openai.azure.com",
		APIVersion: "2024-06-01",
		Deployment: "dep",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "azure" {
		t.Fatalf("name=%q", client.Name())
	}
}

type failRoundTripper struct{}

func (failRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestChatAndStreamTransportErrors(t *testing.T) {
	client, _ := NewClient(Config{
		APIKey: "k", BaseURL: "https://example.openai.azure.com",
		HTTPClient: &http.Client{Transport: failRoundTripper{}},
	})
	req := *protocol.NewChatRequest("dep", protocol.UserMessage("hi"))
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

func (errBodyRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: errBody{}, Header: make(http.Header)}, nil
}

func TestChatReadBodyError(t *testing.T) {
	client, _ := NewClient(Config{
		APIKey: "k", BaseURL: "https://example.openai.azure.com",
		HTTPClient: &http.Client{Transport: errBodyRoundTripper{}},
	})
	if _, err := client.Chat(context.Background(), *protocol.NewChatRequest("dep", protocol.UserMessage("hi"))); err == nil {
		t.Fatal("expected read error")
	}
}

func TestBuildRequestInvalidURL(t *testing.T) {
	client, _ := NewClient(Config{
		APIKey: "k", BaseURL: "http://example.com/" + string([]byte{0x7f}),
	})
	req := *protocol.NewChatRequest("dep", protocol.UserMessage("hi"))
	_, _ = client.Chat(context.Background(), req)
	_, _ = client.StreamChat(context.Background(), req)
}
