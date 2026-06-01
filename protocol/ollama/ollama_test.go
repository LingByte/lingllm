package ollama

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LingByte/lingllm/protocol"
)

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(Config{Model: "llama3"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c.cfg.BaseURL != "http://localhost:11434" {
		t.Errorf("unexpected base URL: %s", c.cfg.BaseURL)
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"model":"llama3","message":{"role":"assistant","content":"Hello from Ollama"}}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, Model: "llama3"})
	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model:       "llama3",
		Messages:    []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.FirstContent() != "Hello from Ollama" {
		t.Errorf("unexpected content: %s", resp.FirstContent())
	}
}

func TestChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, Model: "llama3"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model:    "llama3",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"par"},"done":false}`+"\n")
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"partial"},"done":true}`+"\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, Model: "llama3"})
	stream, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model:    "llama3",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if chunk.Delta != "par" {
		t.Errorf("unexpected delta: %s", chunk.Delta)
	}

	chunk, err = stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if chunk.Delta != "tial" {
		t.Errorf("unexpected delta: %s", chunk.Delta)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestToOllamaMessagesAndResponse(t *testing.T) {
	msgs := toOllamaMessages([]protocol.Message{{Role: protocol.RoleUser, Content: "hi"}})
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Errorf("unexpected ollama messages: %+v", msgs)
	}

	raw := ollamaResponse{}
	raw.Message.Role = "assistant"
	raw.Message.Content = "ok"
	raw.Model = "llama3"
	resp := raw.toChatResponse()
	if resp.FirstContent() != "ok" || resp.Model != "llama3" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewChatModel(protocol.ClientConfig{
		Provider: protocol.ProviderOllama,
		Model:    "llama3",
	})
	if err != nil {
		t.Fatalf("factory registration failed: %v", err)
	}
	if client.Name() != "llama3" {
		t.Errorf("unexpected client name: %s", client.Name())
	}
}
