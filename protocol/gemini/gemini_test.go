package gemini

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

func TestNewClientRequiresAPIKey(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("expected error")
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"role":  "model",
					"parts": []map[string]string{{"text": "hello gemini"}},
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
	))
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstContent() != "hello gemini" {
		t.Fatalf("content=%q", resp.FirstContent())
	}
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "alt=sse") {
			t.Fatalf("missing alt=sse")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}]}\n\n")
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
