package cohere

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LingByte/lingllm/protocol"
)

func TestNewClientRequiresAPIKey(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("expected error")
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":            "id-1",
			"finish_reason": "COMPLETE",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]string{{
					"type": "text",
					"text": "hello cohere",
				}},
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
	resp, err := client.Chat(context.Background(), *protocol.NewChatRequest("command-r-plus", protocol.UserMessage("hi")))
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

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"content-delta\",\"delta\":{\"message\":{\"content\":{\"text\":\"hi\"}}}}\n\n")
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
