// Basic OpenAI chat demo (sync + stream).
//
// Usage:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/openai
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/openai"
	"github.com/LingByte/lingllm/shared/models"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	client, err := protocol.NewChatModel(protocol.ClientConfig{
		Provider: protocol.ProviderOpenAI,
		APIKey:   apiKey,
		Model:    envOr("OPENAI_MODEL", models.OpenAIGPT4o),
		BaseURL:  os.Getenv("OPENAI_BASE_URL"),
	})
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	ctx := context.Background()
	prompt := envOr("PROMPT", "Say hello in one short sentence.")

	req := protocol.ChatRequest{
		Model:    client.Name(),
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
