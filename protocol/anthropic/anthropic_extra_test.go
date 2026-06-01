package anthropic

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

func TestChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusBadRequest)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "key", BaseURL: server.URL, Model: "claude-3"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model: "claude-3", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStreamChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusUnauthorized)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "key", BaseURL: server.URL, Model: "claude-3"})
	_, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model: "claude-3", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicStreamInvalidJSON(t *testing.T) {
	s := &anthropicStream{body: io.NopCloser(strings.NewReader("data: bad\n\n"))}
	_, err := s.Recv()
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestAnthropicStreamMessageDelta(t *testing.T) {
	body := "event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}\n\n"
	s := &anthropicStream{body: io.NopCloser(strings.NewReader(body)), model: "claude"}
	chunk, err := s.Recv()
	if err != nil || chunk.FinishReason != "end_turn" {
		t.Fatalf("unexpected chunk: %+v err=%v", chunk, err)
	}
}

func TestAnthropicStreamClose(t *testing.T) {
	s := &anthropicStream{}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestToChatResponseSkipsNonTextBlocks(t *testing.T) {
	resp := response{
		Content: []textBlock{{Type: "image", Text: "ignored"}, {Type: "text", Text: "ok"}},
		Usage:   usage{},
	}.toChatResponse()
	if strings.TrimSpace(resp.FirstContent()) != "ok" {
		t.Errorf("unexpected content: %s", resp.FirstContent())
	}
}

func TestChatValidationError(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "key", Model: "claude-3"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestStreamChatValidationError(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "key", Model: "claude-3"})
	_, err := client.StreamChat(context.Background(), protocol.ChatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestBaseURLTrimSuffix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":"1","model":"claude","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "key", BaseURL: server.URL + "/", Model: "claude-3"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model: "claude-3", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
}
