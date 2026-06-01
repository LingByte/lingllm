package response

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol"
)

func TestChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusBadRequest)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model: "gpt-4", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatValidationError(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "sk-test", Model: "gpt-4"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
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
		t.Fatal("expected error")
	}
}

func TestResponsesStreamInvalidJSON(t *testing.T) {
	s := &responsesStream{body: io.NopCloser(strings.NewReader("data: bad\n\n"))}
	_, err := s.Recv()
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestResponsesStreamClose(t *testing.T) {
	s := &responsesStream{}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestChatWithOrgProjectHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("OpenAI-Organization") != "org" {
			t.Errorf("missing org header")
		}
		w.Write([]byte(`{"id":"1","model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4", Organization: "org", Project: "proj",
	})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model: "gpt-4", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
}
