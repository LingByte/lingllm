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

func TestNewClientRequiresAPIKey(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error without api key")
	}
}

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(Config{APIKey: "key", Model: "claude-3"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c.cfg.BaseURL != defaultBaseURL {
		t.Errorf("unexpected base URL: %s", c.cfg.BaseURL)
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "anthropic-key" {
			t.Errorf("unexpected api key header")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"stream":true`) {
			t.Errorf("chat request must not enable streaming: %s", body)
		}
		fmt.Fprint(w, `{
			"id":"msg_1",
			"model":"claude-3",
			"role":"assistant",
			"content":[{"type":"text","text":"Hello from Claude"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":5}
		}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "anthropic-key", BaseURL: server.URL, Model: "claude-3"})
	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model:    "claude-3",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.FirstContent() != "Hello from Claude" {
		t.Errorf("unexpected content: %s", resp.FirstContent())
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Errorf("stream request must enable streaming: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "anthropic-key", BaseURL: server.URL, Model: "claude-3"})
	stream, err := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model:    "claude-3",
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
}

func TestToMessagesAndSystemPrompt(t *testing.T) {
	msgs := toMessages([]protocol.Message{
		{Role: protocol.RoleSystem, Content: "be helpful"},
		{Role: protocol.RoleUser, Content: "hello"},
	})
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
	system := extractSystemPrompt([]protocol.Message{
		{Role: protocol.RoleSystem, Content: "a"},
		{Role: protocol.RoleSystem, Content: "b"},
	})
	if system != "a\nb" {
		t.Errorf("unexpected system prompt: %s", system)
	}
}

func TestToChatResponse(t *testing.T) {
	resp := response{
		ID: "id", Model: "claude", StopReason: "end_turn",
		Content: []textBlock{{Type: "text", Text: "line1"}, {Type: "text", Text: "line2"}},
		Usage:   usage{InputTokens: 3, OutputTokens: 7},
	}.toChatResponse()
	if !strings.Contains(resp.FirstContent(), "line1") || resp.Usage.TotalTokens != 10 {
		t.Errorf("unexpected chat response: %+v", resp)
	}
}

func TestMax(t *testing.T) {
	if max(5, 10) != 10 || max(10, 5) != 10 {
		t.Error("max failed")
	}
}

func TestFactoryRegistration(t *testing.T) {
	client, err := protocol.NewChatModel(protocol.ClientConfig{
		Provider: protocol.ProviderAnthropic,
		APIKey:   "key",
		Model:    "claude-3",
	})
	if err != nil {
		t.Fatalf("factory registration failed: %v", err)
	}
	if client.Name() != "claude-3" {
		t.Errorf("unexpected client name: %s", client.Name())
	}
}
