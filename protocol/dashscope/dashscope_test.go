package dashscope

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

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-DashScope-SSE") != "enable" {
			t.Fatal("missing SSE header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"output\":{\"choices\":[{\"message\":{\"content\":\"hi\"}}]}}\n\n")
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
	if chunk.Delta != "hi" {
		t.Fatalf("delta=%q", chunk.Delta)
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
