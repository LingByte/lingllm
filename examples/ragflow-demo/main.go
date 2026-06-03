package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LingByte/lingllm/embedder"
	"github.com/LingByte/lingllm/knowledge"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	// Step 1: Get configuration (hardcoded for now)
	fmt.Println("📋 Step 1: Configuration")
	fmt.Println("─────────────────────────")

	// Hardcoded configuration for quick testing
	ragflowConfig := RagflowConfig{
		BaseURL:   "http://3.230.3.163",
		APIKey:    "ragflow-pfQrfKWmOQAYXe_my6hRLrV8bTQON57Cg6f_YB9UFV4",
		Namespace: "Default",
	}

	embdConfig := EmbedderConfig{
		Provider: "openai",
		APIKey:   "32QKNUANTPLLAW0OM5BE8URXDXVC1L8PCU82UIWW",
		Model:    "Qwen3-Embedding-8B",
		BaseURL:  "https://ai.gitee.com/v1",
	}

	fmt.Printf("RAGFlow BaseURL: %s\n", ragflowConfig.BaseURL)
	fmt.Printf("Dataset: %s\n", ragflowConfig.Namespace)
	fmt.Printf("Embedder: %s (%s)\n", embdConfig.Provider, embdConfig.Model)
	fmt.Printf("Embedder BaseURL: %s\n", embdConfig.BaseURL)

	// Step 2: Initialize handlers
	fmt.Println("\n🔧 Step 2: Initializing handlers...")
	ctx := context.Background()

	// Create embedder
	cfg := &embedder.Config{
		Provider: embdConfig.Provider,
		APIKey:   embdConfig.APIKey,
		Model:    embdConfig.Model,
	}
	
	// Set custom BaseURL if provided
	if embdConfig.BaseURL != "" {
		cfg.BaseURL = embdConfig.BaseURL
	}
	
	embdr, err := embedder.Create(ctx, cfg)
	if err != nil {
		fmt.Printf("Failed to create embedder: %v\n", err)
		return
	}
	defer embdr.Close()

	// Create RAGFlow handler
	ragflowHandler, err := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
		Provider:  knowledge.ProviderRAGFlow,
		Namespace: ragflowConfig.Namespace,
		RAGFlowConfig: &knowledge.RAGFlowConfig{
			BaseURL: ragflowConfig.BaseURL,
			APIKey:  ragflowConfig.APIKey,
		},
	})
	if err != nil {
		fmt.Printf("❌ Failed to create RAGFlow handler: %v\n", err)
		return
	}

	// Test connection
	if err := ragflowHandler.Ping(ctx); err != nil {
		fmt.Printf("Failed to connect to RAGFlow: %v\n", err)
		return
	}
	fmt.Println("✓ Connected to RAGFlow")

	// Step 3: Data cleaning and preparation
	fmt.Println("\nStep 3: Data Cleaning & Preparation")
	fmt.Println("─────────────────────────────────────")

	sampleData := getSampleData()
	cleanedData := cleanData(sampleData)

	fmt.Printf("✓ Loaded %d documents\n", len(cleanedData))
	fmt.Printf("✓ Data cleaned and prepared\n")

	// Step 4: Embedding
	fmt.Println("\nStep 4: Embedding Documents")
	fmt.Println("──────────────────────────────")

	records := prepareRecords(cleanedData)
	fmt.Printf("✓ Prepared %d records for embedding\n", len(records))

	// Embed documents
	fmt.Println("Embedding documents with OpenAI...")
	texts := make([]string, len(records))
	for i, record := range records {
		texts[i] = record.Content
	}

	vectors, err := embdr.Embed(ctx, texts)
	if err != nil {
		fmt.Printf("Failed to embed documents: %v\n", err)
		return
	}

	// Attach vectors to records
	for i, vector := range vectors {
		records[i].Vector = vector
	}
	fmt.Printf("✓ Embedded %d documents\n", len(records))

	// Step 5: Insert into RAGFlow
	fmt.Println("\nStep 5: Inserting Documents into RAGFlow")
	fmt.Println("──────────────────────────────────────────")

	startInsert := time.Now()
	fmt.Printf("Inserting into dataset: %s\n", ragflowConfig.Namespace)
	fmt.Println("Uploading documents...")
	
	// Debug: print first record info
	if len(records) > 0 {
		fmt.Printf("First document: ID=%s, Title=%s, Content length=%d\n", 
			records[0].ID, records[0].Title, len(records[0].Content))
	}
	
	err = ragflowHandler.Upsert(ctx, records, &knowledge.UpsertOptions{
		Namespace: ragflowConfig.Namespace,
	})
	if err != nil {
		fmt.Printf("Failed to insert documents: %v\n", err)
		fmt.Println("\nNote: Make sure the dataset exists in RAGFlow and is accessible with the provided API key.")
		fmt.Println("You may need to create the dataset manually in RAGFlow first.")
		fmt.Println("\nDebug info:")
		fmt.Printf("- RAGFlow BaseURL: %s\n", ragflowConfig.BaseURL)
		fmt.Printf("- Dataset name: %s\n", ragflowConfig.Namespace)
		return
	}
	insertTime := time.Since(startInsert)
	fmt.Printf("✓ Inserted %d documents in %.2fs\n", len(records), insertTime.Seconds())
	
	// Wait a moment for RAGFlow to process
	fmt.Println("Waiting for RAGFlow to process documents...")
	time.Sleep(2 * time.Second)

	// Step 5.5: Verify dataset
	fmt.Println("\nVerifying dataset...")
	namespaces, err := ragflowHandler.ListNamespaces(ctx)
	if err != nil {
		fmt.Printf("Warning: Could not list namespaces: %v\n", err)
	} else {
		fmt.Printf("Available datasets: %v\n", namespaces)
		found := false
		for _, ns := range namespaces {
			if strings.EqualFold(ns, ragflowConfig.Namespace) {
				found = true
				fmt.Printf("✓ Dataset '%s' found\n", ns)
				break
			}
		}
		if !found {
			fmt.Printf("Warning: Dataset '%s' not found in RAGFlow\n", ragflowConfig.Namespace)
			fmt.Println("Available datasets:", namespaces)
		}
	}
	
	fmt.Println("\n📌 Important:")
	fmt.Printf("- If documents are not found in queries, please check RAGFlow Web UI:\n")
	fmt.Printf("  http://%s/\n", strings.TrimPrefix(ragflowConfig.BaseURL, "http://"))
	fmt.Printf("- Make sure documents are visible in the '%s' dataset\n", ragflowConfig.Namespace)
	fmt.Println("- Documents may need to be indexed/processed by RAGFlow before they appear in search results")

	// Step 6: Query
	fmt.Println("\nStep 6: Query Documents")
	fmt.Println("──────────────────────────")
	fmt.Println("Enter your queries (type 'exit' to quit):")
	fmt.Println()

	for {
		fmt.Print("Query: ")
		query, _ := reader.ReadString('\n')
		query = strings.TrimSpace(query)

		if query == "exit" {
			break
		}

		if query == "" {
			continue
		}

		startQuery := time.Now()
		
		// Embed the query
		queryVector, err := embdr.EmbedSingle(ctx, query)
		if err != nil {
			fmt.Printf("Failed to embed query: %v\n", err)
			continue
		}
		fmt.Printf("Query vector dimension: %d\n", len(queryVector))

		results, err := ragflowHandler.Query(ctx, query, &knowledge.QueryOptions{
			Namespace: ragflowConfig.Namespace,
			TopK:      5,
			MinScore:  0.0,
		})
		queryTime := time.Since(startQuery)

		if err != nil {
			fmt.Printf("Query failed: %v\n", err)
			fmt.Println("Tip: Make sure documents were successfully inserted into the dataset.")
			continue
		}

		if len(results) == 0 {
			fmt.Printf("No results found (took %.2fms)\n", queryTime.Seconds()*1000)
			fmt.Println("Tip: Try a different query or check if documents were inserted correctly.")
			fmt.Println()
			continue
		}

		fmt.Printf("Found %d results (took %.2fms):\n", len(results), queryTime.Seconds()*1000)
		for i, result := range results {
			fmt.Printf("\n[%d] Score: %.4f\n", i+1, result.Score)
			fmt.Printf("    Content: %s\n", truncateString(result.Record.Content, 100))
		}
		fmt.Println()
	}

	fmt.Println("\n✓ Demo completed!")
}

// Configuration structures
type RagflowConfig struct {
	BaseURL   string
	APIKey    string
	Namespace string
}

type EmbedderConfig struct {
	Provider string
	APIKey   string
	Model    string
	BaseURL  string
}

// Helper functions
func getRagflowConfig(reader *bufio.Reader) RagflowConfig {
	fmt.Print("Enter RAGFlow BaseURL (e.g., http://localhost:9380): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)

	fmt.Print("Enter RAGFlow API Key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	fmt.Print("Enter Dataset/Namespace name (default: 'default'): ")
	namespace, _ := reader.ReadString('\n')
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}

	return RagflowConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Namespace: namespace,
	}
}

func getEmbedderConfig(reader *bufio.Reader) EmbedderConfig {
	fmt.Print("Enter Embedder Provider (openai/dashscope/ollama, default: 'openai'): ")
	provider, _ := reader.ReadString('\n')
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "openai"
	}

	fmt.Print("Enter Embedder API Key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	var defaultModel string
	switch provider {
	case "dashscope":
		defaultModel = "text-embedding-v2"
	case "ollama":
		defaultModel = "nomic-embed-text"
	default:
		defaultModel = "text-embedding-3-small"
	}

	fmt.Printf("Enter Embedder Model (default: '%s'): ", defaultModel)
	model, _ := reader.ReadString('\n')
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultModel
	}

	fmt.Print("Enter Embedder BaseURL (optional, e.g., https://ai.gitee.com/v1): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)

	return EmbedderConfig{
		Provider: provider,
		APIKey:   apiKey,
		Model:    model,
		BaseURL:  baseURL,
	}
}

// Sample data for demonstration
func getSampleData() []map[string]string {
	return []map[string]string{
		{
			"id":    "doc1",
			"title": "Go Programming Language",
			"content": "Go is an open source programming language that makes it easy to build simple, reliable, and efficient software. " +
				"Go is expressive, concise, clean, and efficient. Its concurrency mechanisms make it easy to write programs that get the most out of multicore and networked machines, " +
				"while its novel type system enables flexible and modular program construction.",
		},
		{
			"id":    "doc2",
			"title": "Python for Data Science",
			"content": "Python has become the de facto standard for data science and machine learning. " +
				"With libraries like NumPy, Pandas, and Scikit-learn, Python provides a comprehensive ecosystem for data analysis, visualization, and machine learning. " +
				"Its simplicity and readability make it ideal for both beginners and experts.",
		},
		{
			"id":    "doc3",
			"title": "Kubernetes Container Orchestration",
			"content": "Kubernetes is an open-source container orchestration platform that automates many of the manual processes involved in deploying, managing, and scaling containerized applications. " +
				"It groups containers that make up an application into logical units for easy management and discovery. " +
				"Kubernetes builds upon 15 years of experience of running production workloads at Google.",
		},
		{
			"id":    "doc4",
			"title": "Machine Learning Fundamentals",
			"content": "Machine learning is a subset of artificial intelligence that focuses on the development of algorithms and statistical models that enable computers to improve their performance on tasks through experience. " +
				"It involves training models on historical data to make predictions or decisions without being explicitly programmed for the task.",
		},
		{
			"id":    "doc5",
			"title": "Cloud Computing Architecture",
			"content": "Cloud computing delivers computing services including servers, storage, databases, networking, software, analytics, and intelligence over the Internet to offer faster innovation, flexible resources, and economies of scale. " +
				"Major cloud providers include AWS, Google Cloud, and Microsoft Azure, each offering a wide range of services.",
		},
		{
			"id":    "doc6",
			"title": "REST API Design Best Practices",
			"content": "REST (Representational State Transfer) is an architectural style for designing networked applications. " +
				"RESTful APIs use HTTP requests to perform CRUD (Create, Read, Update, Delete) operations on resources. " +
				"Best practices include using proper HTTP methods, status codes, and maintaining statelessness.",
		},
		{
			"id":    "doc7",
			"title": "Database Optimization Techniques",
			"content": "Database optimization involves improving query performance, reducing resource consumption, and enhancing overall system efficiency. " +
				"Key techniques include indexing, query optimization, connection pooling, and proper schema design. " +
				"Regular monitoring and profiling are essential for maintaining optimal database performance.",
		},
		{
			"id":    "doc8",
			"title": "DevOps and CI/CD Pipelines",
			"content": "DevOps is a set of practices that combines software development and IT operations to shorten the systems development life cycle. " +
				"CI/CD pipelines automate the process of building, testing, and deploying applications. " +
				"Tools like Jenkins, GitLab CI, and GitHub Actions enable continuous integration and continuous deployment.",
		},
	}
}

// Clean and normalize data
func cleanData(data []map[string]string) []map[string]string {
	cleaned := make([]map[string]string, 0, len(data))

	for _, item := range data {
		// Remove extra whitespace
		content := strings.Join(strings.Fields(item["content"]), " ")
		title := strings.TrimSpace(item["title"])
		id := strings.TrimSpace(item["id"])

		// Skip empty documents
		if content == "" || title == "" {
			continue
		}

		cleaned = append(cleaned, map[string]string{
			"id":      id,
			"title":   title,
			"content": content,
		})
	}

	return cleaned
}

// Prepare records for insertion
func prepareRecords(data []map[string]string) []knowledge.Record {
	records := make([]knowledge.Record, 0, len(data))

	for _, item := range data {
		record := knowledge.Record{
			ID:      item["id"],
			Title:   item["title"],
			Content: item["content"],
			Source:  "demo",
			Tags:    []string{"sample", "demo"},
		}
		records = append(records, record)
	}

	return records
}

// Utility function to truncate strings
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
