package ollama

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol"
)

func TestChatValidationError(t *testing.T) {
	client, _ := NewClient(Config{Model: "llama3"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestChatWithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing auth header")
		}
		w.Write([]byte(`{"model":"llama3","message":{"role":"assistant","content":"ok"}}`))
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, Model: "llama3", APIKey: "secret"})
	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model: "llama3", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil || resp.FirstContent() != "ok" {
		t.Fatalf("Chat failed: resp=%+v err=%v", resp, err)
	}
}

func TestStreamChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, Model: "llama3"})
	_, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model: "llama3", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOllamaStreamInvalidJSON(t *testing.T) {
	s := &ollamaStream{body: io.NopCloser(strings.NewReader("not-json\n"))}
	_, err := s.Recv()
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestOllamaStreamClose(t *testing.T) {
	s := &ollamaStream{}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestOllamaStreamEmptyLineEOF(t *testing.T) {
	s := &ollamaStream{body: io.NopCloser(strings.NewReader("\n"))}
	_, err := s.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}
