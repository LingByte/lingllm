package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			Index: 0,
			Message: openAIMessage{Role: "assistant", Content: "ok"},
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
