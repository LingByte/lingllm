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
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║        LingLLM Knowledge Base Query Demo                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝\n")

	reader := bufio.NewReader(os.Stdin)

	// Select provider
	fmt.Println("Select knowledge base provider:")
	fmt.Println("1. Qdrant")
	fmt.Println("2. Milvus")
	fmt.Println("3. RAGFlow")
	fmt.Print("\nEnter choice (1-3): ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	var handler knowledge.KnowledgeHandler
	var err error

	switch choice {
	case "1":
		handler, err = setupQdrant(reader)
	case "2":
		handler, err = setupMilvus(reader)
	case "3":
		handler, err = setupRAGFlow(reader)
	default:
		fmt.Println("Invalid choice")
		return
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

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

func setupQdrant(reader *bufio.Reader) (knowledge.KnowledgeHandler, error) {
	fmt.Print("Enter Qdrant BaseURL (e.g., http://localhost:6333): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("BaseURL is required")
	}

	fmt.Print("Enter Qdrant API Key (optional, press Enter to skip): ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	fmt.Print("Enter Collection/Namespace name: ")
	namespace, _ := reader.ReadString('\n')
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("Namespace is required")
	}

	fmt.Println("\nConnecting to Qdrant...")

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
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = handler.Ping(ctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("error connecting to Qdrant: %v", err)
	}

	fmt.Println("✓ Connected to Qdrant\n")
	return handler, nil
}

func setupMilvus(reader *bufio.Reader) (knowledge.KnowledgeHandler, error) {
	fmt.Print("Enter Milvus Address (e.g., localhost:19530): ")
	address, _ := reader.ReadString('\n')
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("Address is required")
	}

	fmt.Print("Enter Milvus Username (optional): ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Enter Milvus Password (optional): ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)

	fmt.Print("Enter Collection/Namespace name: ")
	namespace, _ := reader.ReadString('\n')
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("Namespace is required")
	}

	fmt.Println("\nConnecting to Milvus...")

	handler, err := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
		Provider:  knowledge.ProviderMilvus,
		Namespace: namespace,
		MilvusConfig: &knowledge.MilvusConfig{
			Address:  address,
			Username: username,
			Password: password,
			DBName:   "default",
		},
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = handler.Ping(ctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("error connecting to Milvus: %v", err)
	}

	fmt.Println("✓ Connected to Milvus\n")
	return handler, nil
}

func setupRAGFlow(reader *bufio.Reader) (knowledge.KnowledgeHandler, error) {
	fmt.Print("Enter RAGFlow BaseURL (e.g., http://localhost:9380): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("BaseURL is required")
	}

	fmt.Print("Enter RAGFlow API Key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API Key is required")
	}

	fmt.Print("Enter Dataset/Namespace name: ")
	namespace, _ := reader.ReadString('\n')
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("Namespace is required")
	}

	fmt.Println("\nConnecting to RAGFlow...")

	handler, err := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
		Provider:  knowledge.ProviderRAGFlow,
		Namespace: namespace,
		RAGFlowConfig: &knowledge.RAGFlowConfig{
			BaseURL: baseURL,
			APIKey:  apiKey,
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = handler.Ping(ctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("error connecting to RAGFlow: %v", err)
	}

	fmt.Println("✓ Connected to RAGFlow\n")
	return handler, nil
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
