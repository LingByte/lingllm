// Basic Ollama local chat demo (sync + stream).
//
// Usage:
//
//	ollama pull llama3.2
//	go run ./examples/ollama
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/ollama"
	"github.com/LingByte/lingllm/shared/models"
)

func main() {
	client, err := protocol.NewChatModel(protocol.ClientConfig{
		Provider: protocol.ProviderOllama,
		Model:    envOr("OLLAMA_MODEL", models.OllamaLlama32),
		BaseURL:  envOr("OLLAMA_BASE_URL", "http://localhost:11434"),
		APIKey:   os.Getenv("OLLAMA_API_KEY"),
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
	fmt.Printf("latency: %v\n", resp.Metrics.Latency())

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
