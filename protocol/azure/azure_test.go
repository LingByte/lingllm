package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	resp, err := client.Chat(context.Background(), *protocol.NewChatRequest("gpt-4o", protocol.UserMessage("hi")))
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

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
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
	if chunk.Delta != "hi" {
		t.Fatalf("delta=%q", chunk.Delta)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewClient(protocol.ClientConfig{
		Provider: protocol.ProviderAzure,
		APIKey:   "k",
		BaseURL:  "https://example.openai.azure.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "azure" {
		t.Fatalf("name=%q", client.Name())
	}
}
