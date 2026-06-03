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
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║     RAGFlow Complete Workflow Demo                          ║")
	fmt.Println("║  Data Cleaning → Embedding → Insert → Query                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Step 1: Get configuration
	fmt.Println("📋 Step 1: Configuration")
	fmt.Println("─────────────────────────")

	ragflowConfig := getRagflowConfig(reader)
	openaiConfig := getOpenAIConfig(reader)

	// Step 2: Initialize handlers
	fmt.Println("\n🔧 Step 2: Initializing handlers...")
	ctx := context.Background()

	// Create OpenAI embedder
	embdr, err := embedder.Create(ctx, &embedder.Config{
		Provider: "openai",
		APIKey:   openaiConfig.APIKey,
		Model:    openaiConfig.Model,
	})
	if err != nil {
		fmt.Printf("❌ Failed to create embedder: %v\n", err)
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
		fmt.Printf("❌ Failed to connect to RAGFlow: %v\n", err)
		return
	}
	fmt.Println("✓ Connected to RAGFlow")

	// Step 3: Data cleaning and preparation
	fmt.Println("\n📝 Step 3: Data Cleaning & Preparation")
	fmt.Println("─────────────────────────────────────")

	sampleData := getSampleData()
	cleanedData := cleanData(sampleData)

	fmt.Printf("✓ Loaded %d documents\n", len(cleanedData))
	fmt.Printf("✓ Data cleaned and prepared\n")

	// Step 4: Embedding
	fmt.Println("\n🧠 Step 4: Embedding Documents")
	fmt.Println("──────────────────────────────")

	records := prepareRecords(cleanedData)
	fmt.Printf("✓ Prepared %d records for embedding\n", len(records))

	// Step 5: Insert into RAGFlow
	fmt.Println("\n💾 Step 5: Inserting Documents into RAGFlow")
	fmt.Println("──────────────────────────────────────────")

	startInsert := time.Now()
	err = ragflowHandler.Upsert(ctx, records, &knowledge.UpsertOptions{
		Namespace: ragflowConfig.Namespace,
	})
	if err != nil {
		fmt.Printf("❌ Failed to insert documents: %v\n", err)
		return
	}
	insertTime := time.Since(startInsert)
	fmt.Printf("✓ Inserted %d documents in %.2fs\n", len(records), insertTime.Seconds())

	// Step 6: Query
	fmt.Println("\n🔍 Step 6: Query Documents")
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
		results, err := ragflowHandler.Query(ctx, query, &knowledge.QueryOptions{
			Namespace: ragflowConfig.Namespace,
			TopK:      5,
			MinScore:  0.0,
		})
		queryTime := time.Since(startQuery)

		if err != nil {
			fmt.Printf("❌ Query failed: %v\n", err)
			continue
		}

		if len(results) == 0 {
			fmt.Printf("No results found (took %.2fms)\n\n", queryTime.Seconds()*1000)
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

type OpenAIConfig struct {
	APIKey string
	Model  string
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

func getOpenAIConfig(reader *bufio.Reader) OpenAIConfig {
	fmt.Print("Enter OpenAI API Key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	fmt.Print("Enter OpenAI Model (default: 'text-embedding-3-small'): ")
	model, _ := reader.ReadString('\n')
	model = strings.TrimSpace(model)
	if model == "" {
		model = "text-embedding-3-small"
	}

	return OpenAIConfig{
		APIKey: apiKey,
		Model:  model,
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
