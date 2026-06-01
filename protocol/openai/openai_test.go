package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol"
)

func TestNewClientRequiresAPIKey(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error without api key")
	}
}

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(Config{APIKey: "sk-test", Model: "gpt-4"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c.cfg.BaseURL != defaultBaseURL {
		t.Errorf("unexpected base URL: %s", c.cfg.BaseURL)
	}
	if c.Name() != "gpt-4" {
		t.Errorf("unexpected name: %s", c.Name())
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-test" {
			t.Errorf("unexpected auth: %s", auth)
		}
		fmt.Fprint(w, `{
			"id":"chatcmpl-1",
			"created":1700000000,
			"model":"gpt-4",
			"choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		APIKey:  "sk-test",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.FirstContent() != "Hello" {
		t.Errorf("unexpected content: %s", resp.FirstContent())
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
	if resp.Metrics.HTTPStatus != 200 {
		t.Errorf("unexpected metrics: %+v", resp.Metrics)
	}
}

func TestChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected http error")
	}
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hi\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	stream, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}
	defer stream.Close()

	var content strings.Builder
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
		content.WriteString(chunk.Delta)
	}
	if content.String() != "Hi" {
		t.Errorf("unexpected stream content: %s", content.String())
	}
	if stream.Metrics().Chunks == 0 {
		t.Error("expected chunk metrics")
	}
}

func TestToOpenAIMessages(t *testing.T) {
	msgs := toOpenAIMessages([]protocol.Message{
		{Role: protocol.RoleUser, Content: "hello"},
	})
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestToChatResponse(t *testing.T) {
	raw := openAIResponse{
		ID: "id-1", Created: 1700000000, Model: "gpt-4",
		Choices: []openAIChoice{{
			Index:        0,
			Message:      openAIMessage{Role: "assistant", Content: "ok"},
			FinishReason: "stop",
		}},
		Usage: openAIUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}
	resp := raw.toChatResponse()
	if resp.ID != "id-1" || resp.FirstContent() != "ok" || resp.Usage.TotalTokens != 3 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewChatModel(protocol.ClientConfig{
		Provider: protocol.ProviderOpenAI,
		APIKey:   "sk-test",
		Model:    "gpt-4",
	})
	if err != nil {
		t.Fatalf("factory registration failed: %v", err)
	}
	if client.Name() != "gpt-4" {
		t.Errorf("unexpected client name: %s", client.Name())
	}
}

func TestOpenAIStreamMetrics(t *testing.T) {
	now := time.Now()
	s := &openAIStream{
		startAt: now,
		firstAt: now,
		endAt:   now,
		model:   "gpt-4",
		usage: protocol.TokenUsage{
			PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3,
		},
		chunks: 2, bytes: 100, httpStatus: 200,
	}

	m := s.Metrics()
	if m.Provider != "openai" || m.Model != "gpt-4" || m.TotalTokens != 3 {
		t.Errorf("unexpected metrics: %+v", m)
	}
}

func TestOpenAIStreamRecvUsageAndEmptyChoices(t *testing.T) {
	body := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"y\"}}]}\n\n" +
		"data: [DONE]\n\n"
	s := &openAIStream{body: io.NopCloser(strings.NewReader(body)), model: "gpt-4"}
	chunk, err := s.Recv()
	if err != nil || chunk.Delta != "y" {
		t.Fatalf("Recv failed: chunk=%+v err=%v", chunk, err)
	}
	if s.Metrics().TotalTokens != 3 {
		t.Errorf("expected usage in metrics")
	}
}

func TestOpenAIStreamReadLineError(t *testing.T) {
	s := &openAIStream{body: io.NopCloser(&failReader{})}
	_, err := s.readLine()
	if err == nil {
		t.Fatal("expected read error")
	}
}

type failReader struct{}

func (f *failReader) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestChatValidationError(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "sk-test", Model: "gpt-4"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestChatUsesDefaultModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"1","created":1,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil || resp.FirstContent() != "ok" {
		t.Fatalf("Chat failed: resp=%+v err=%v", resp, err)
	}
}

func TestChatWithOrgAndProjectHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("OpenAI-Organization") != "org" || r.Header.Get("OpenAI-Project") != "proj" {
			t.Errorf("missing org/project headers")
		}
		fmt.Fprint(w, `{"id":"1","created":1,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4",
		Organization: "org", Project: "proj",
	})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model: "gpt-4", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
}

func TestStreamChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusUnauthorized)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	_, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model: "gpt-4", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected stream error")
	}
}

func TestStreamChatInvalidChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: not-json\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	stream, _ := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model: "gpt-4", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestOpenAIStreamClose(t *testing.T) {
	s := &openAIStream{}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestOpenAIStreamReadLineEOFWithPartialLine(t *testing.T) {
	s := &openAIStream{body: io.NopCloser(strings.NewReader("partial"))}
	line, err := s.readLine()
	if line != "partial" || err != io.EOF {
		t.Fatalf("unexpected readLine result: %q err=%v", line, err)
	}
}

func TestOpenAIStreamSkipsNonDataLines(t *testing.T) {
	body := "event: ping\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"
	s := &openAIStream{body: io.NopCloser(strings.NewReader(body))}
	chunk, err := s.Recv()
	if err != nil || chunk.Delta != "x" {
		t.Fatalf("Recv failed: chunk=%+v err=%v", chunk, err)
	}
}
