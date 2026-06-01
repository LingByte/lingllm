package protocol_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/anthropic"
	_ "github.com/LingByte/lingllm/protocol/ollama"
	_ "github.com/LingByte/lingllm/protocol/openai"
	_ "github.com/LingByte/lingllm/protocol/response"
)

func TestAllProvidersChatAndStream(t *testing.T) {
	cases := []struct {
		name     string
		provider protocol.ProviderType
		config   protocol.ClientConfig
		handler  http.HandlerFunc
		stream   string
	}{
		{
			name:     "openai",
			provider: protocol.ProviderOpenAI,
			config:   protocol.ClientConfig{Provider: protocol.ProviderOpenAI, APIKey: "k", Model: "gpt-4o"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"id":"1","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			},
			stream: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n",
		},
		{
			name:     "anthropic",
			provider: protocol.ProviderAnthropic,
			config:   protocol.ClientConfig{Provider: protocol.ProviderAnthropic, APIKey: "k", Model: "claude-sonnet-4-6"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"id":"m","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
			},
			stream: "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		},
		{
			name:     "ollama",
			provider: protocol.ProviderOllama,
			config:   protocol.ClientConfig{Provider: protocol.ProviderOllama, Model: "llama3.2"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"model":"llama3.2","message":{"role":"assistant","content":"ok"}}`)
			},
			stream: `{"message":{"role":"assistant","content":"hel"},"done":false}` + "\n" +
				`{"message":{"role":"assistant","content":"hello"},"done":true}` + "\n",
		},
		{
			name:     "openai-response-gateway",
			provider: protocol.ProviderOpenAIResponse,
			config:   protocol.ClientConfig{Provider: protocol.ProviderOpenAIResponse, APIKey: "k", Model: "gpt-4o"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"id":"1","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
			},
			stream: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			cfg := tc.config
			cfg.BaseURL = server.URL

			client, err := protocol.NewChatModel(cfg)
			if err != nil {
				t.Fatalf("NewChatModel: %v", err)
			}

			resp, err := client.Chat(context.Background(), protocol.ChatRequest{
				Model:    cfg.Model,
				Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "ping"}},
			})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if resp.FirstContent() != "ok" {
				t.Errorf("Chat content = %q, want ok", resp.FirstContent())
			}

			streamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(tc.stream, "event:") {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				fmt.Fprint(w, tc.stream)
			}))
			defer streamServer.Close()

			cfg.BaseURL = streamServer.URL
			client, err = protocol.NewChatModel(cfg)
			if err != nil {
				t.Fatalf("NewChatModel stream: %v", err)
			}

			stream, err := client.StreamChat(context.Background(), protocol.ChatRequest{
				Model:    cfg.Model,
				Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "ping"}},
			})
			if err != nil {
				t.Fatalf("StreamChat: %v", err)
			}
			defer stream.Close()

			var got strings.Builder
			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("Recv: %v", err)
				}
				got.WriteString(chunk.Delta)
			}
			if got.String() == "" {
				t.Error("expected non-empty stream output")
			}
		})
	}
}
