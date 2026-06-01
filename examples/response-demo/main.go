// Basic OpenAI-compatible gateway chat demo (sync + stream).
//
// Usage:
//
//	export GATEWAY_API_KEY=...
//	export GATEWAY_BASE_URL=https://your-gateway.example/v1
//	go run ./examples/gateway
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/response"
	"github.com/LingByte/lingllm/shared/models"
)

func main() {
	apiKey := os.Getenv("GATEWAY_API_KEY")
	baseURL := os.Getenv("GATEWAY_BASE_URL")
	if apiKey == "" || baseURL == "" {
		log.Fatal("GATEWAY_API_KEY and GATEWAY_BASE_URL are required")
	}

	client, err := protocol.NewChatModel(protocol.ClientConfig{
		Provider: protocol.ProviderOpenAIResponse,
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Model:    envOr("GATEWAY_MODEL", models.GatewayGPT4o),
	})
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	ctx := context.Background()
	prompt := envOr("PROMPT", "Say hello in one short sentence.")

	req := protocol.ChatRequest{
		Model:    envOr("GATEWAY_MODEL", models.GatewayGPT4o),
		Messages: []protocol.Message{protocol.UserMessage(prompt)},
	}

	resp, err := client.Chat(ctx, req)
	if err != nil {
		log.Fatalf("chat: %v", err)
	}
	fmt.Println("--- Chat ---")
	fmt.Println(resp.FirstContent())
	fmt.Printf("tokens: %d | latency: %v\n", resp.Usage.TotalTokens, resp.Metrics.Latency())

	stream, err := client.StreamChat(ctx, req)
	if err != nil {
		log.Fatalf("stream chat: %v", err)
	}
	defer stream.Close()

	fmt.Println("--- Stream ---")
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("recv: %v", err)
		}
		fmt.Print(chunk.Delta)
	}
	fmt.Println()
	fmt.Printf("stream latency: %v | ttft: %v\n", stream.Metrics().Latency(), stream.Metrics().FirstTokenLatency())
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
