package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LingByte/lingllm/knowledge"
)

func main() {

	reader := bufio.NewReader(os.Stdin)

	// Get configuration from user
	fmt.Print("Enter Qdrant BaseURL (e.g., http://localhost:6333): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		fmt.Println("Error: BaseURL is required")
		return
	}

	fmt.Print("Enter Qdrant API Key (optional, press Enter to skip): ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	fmt.Print("Enter Collection/Namespace name: ")
	namespace, _ := reader.ReadString('\n')
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		fmt.Println("Error: Namespace is required")
		return
	}

	fmt.Println("\nConnecting to Qdrant...")

	// Create Qdrant handler
	handler, err := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
		Provider:  knowledge.ProviderQdrant,
		Namespace: namespace,
		QdrantConfig: &knowledge.QdrantConfig{
			BaseURL: baseURL,
			APIKey:  apiKey,
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		fmt.Printf("Error creating handler: %v\n", err)
		return
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = handler.Ping(ctx)
	cancel()
	if err != nil {
		fmt.Printf("Error connecting to Qdrant: %v\n", err)
		return
	}

	fmt.Println("✓ Connected to Qdrant\n")

	// Create knowledge base
	kb, err := knowledge.NewKnowledgeBase(knowledge.KnowledgeBaseConfig{
		Handler: handler,
	})
	if err != nil {
		fmt.Printf("Error creating knowledge base: %v\n", err)
		return
	}
	defer kb.Close()

	fmt.Println("✓ Knowledge base initialized\n")

	// Interactive query session
	interactiveSession(kb)
}

func interactiveSession(kb *knowledge.KnowledgeBase) {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║           Enter your query (type 'exit' to quit)            ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝\n")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Query: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		if input == "exit" {
			fmt.Println("\nGoodbye!")
			return
		}

		// Query the knowledge base
		results, err := kb.Query(context.Background(), input, 5)
		if err != nil {
			fmt.Printf("Error: %v\n\n", err)
			continue
		}

		if len(results) == 0 {
			fmt.Println("No results found\n")
			continue
		}

		fmt.Printf("\nFound %d results:\n\n", len(results))
		for i, result := range results {
			fmt.Printf("%d. %s\n", i+1, result.Record.Title)
			fmt.Printf("   Content: %s\n", result.Record.Content)
			fmt.Printf("   Score: %.4f\n\n", result.Score)
		}
	}
}
