package openai

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

func TestChatValidationError(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "sk-test", Model: "gpt-4"})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestChatUsesDefaultModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"1","created":1,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	resp, err := client.Chat(context.Background(), protocol.ChatRequest{
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil || resp.FirstContent() != "ok" {
		t.Fatalf("Chat failed: resp=%+v err=%v", resp, err)
	}
}

func TestChatWithOrgAndProjectHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("OpenAI-Organization") != "org" || r.Header.Get("OpenAI-Project") != "proj" {
			t.Errorf("missing org/project headers")
		}
		fmt.Fprint(w, `{"id":"1","created":1,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4",
		Organization: "org", Project: "proj",
	})
	_, err := client.Chat(context.Background(), protocol.ChatRequest{
		Model: "gpt-4", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
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
		t.Fatal("expected stream error")
	}
}

func TestStreamChatInvalidChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: not-json\n\n")
	}))
	defer server.Close()

	client, _ := NewClient(Config{APIKey: "sk-test", BaseURL: server.URL, Model: "gpt-4"})
	stream, _ := client.StreamChat(context.Background(), protocol.ChatRequest{
		Model: "gpt-4", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestOpenAIStreamClose(t *testing.T) {
	s := &openAIStream{}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestOpenAIStreamReadLineEOFWithPartialLine(t *testing.T) {
	s := &openAIStream{body: io.NopCloser(strings.NewReader("partial"))}
	line, err := s.readLine()
	if line != "partial" || err != io.EOF {
		t.Fatalf("unexpected readLine result: %q err=%v", line, err)
	}
}

func TestOpenAIStreamSkipsNonDataLines(t *testing.T) {
	body := "event: ping\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"
	s := &openAIStream{body: io.NopCloser(strings.NewReader(body))}
	chunk, err := s.Recv()
	if err != nil || chunk.Delta != "x" {
		t.Fatalf("Recv failed: chunk=%+v err=%v", chunk, err)
	}
}
