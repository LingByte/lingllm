package response

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LingByte/lingllm/protocol"
)

func TestNewClientRequiresAPIKey(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error without api key")
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"id":"resp-1",
			"model":"gpt-4",
			"choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
		}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model:       "gpt-4",
		Messages:    []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
		MaxTokens:   50,
		Temperature: 0.2,
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.FirstContent() != "Hello" || resp.Usage.TotalTokens != 8 {
		t.Errorf("unexpected response: %+v", resp)
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

	chunk, err := stream.Recv()
	if err != nil && err != io.EOF {
		t.Fatalf("Recv failed: %v", err)
	}
	if chunk != nil && chunk.Delta != "Hi" {
		t.Errorf("unexpected delta: %s", chunk.Delta)
	}
	if stream.Metrics().Provider != "openai-responses" {
		t.Errorf("unexpected provider metric: %s", stream.Metrics().Provider)
	}
}

func TestName(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "sk-test", Model: "gpt-4"})
	if client.Name() != "openai-responses" {
		t.Errorf("unexpected name: %s", client.Name())
	}
}

func TestToResponsesMessages(t *testing.T) {
	msgs := toResponsesMessages([]protocol.Message{{Role: protocol.RoleUser, Content: "hi"}})
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewChatModel(protocol.ClientConfig{
		Provider: protocol.ProviderOpenAIResponse,
		APIKey:   "sk-test",
		Model:    "gpt-4",
	})
	if err != nil {
		t.Fatalf("factory registration failed: %v", err)
	}
	if client.Name() != "openai-responses" {
		t.Errorf("unexpected client name: %s", client.Name())
	}
}
