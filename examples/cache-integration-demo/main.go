package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/LingByte/lingllm/metrics"
	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/openai"
)

func main() {
	apiKey := flag.String("apikey", "", "API key for the LLM provider")
	model := flag.String("model", "gpt-4", "Model name")
	baseURL := flag.String("base_url", "", "Base URL for the API")
	flag.Parse()

	if *apiKey == "" {
		log.Fatal("apikey is required")
	}

	// Create the base model
	client, err := protocol.NewClient("openai", protocol.ClientConfig{
		APIKey:  *apiKey,
		BaseURL: *baseURL,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Create cache (100MB, 1 hour TTL)
	cache := protocol.NewMemoryCache(100*1024*1024, 1*time.Hour)

	// Wrap model with caching
	cachedModel := protocol.NewCachedModel(client, cache)

	ctx := context.Background()

	fmt.Println("=== LingLLM Cache Integration Demo ===\n")

	// Test 1: First request (cache miss)
	fmt.Println("Test 1: First request (cache miss)")
	fmt.Println("─────────────────────────────────────")

	req1 := protocol.ChatRequest{
		Model: *model,
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: "What is machine learning?"},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	start := time.Now()
	resp1, err := cachedModel.Chat(ctx, req1)
	elapsed1 := time.Since(start)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp1.FirstContent())
	fmt.Printf("Latency: %v\n", elapsed1)
	fmt.Printf("Tokens: %d\n\n", resp1.Usage.TotalTokens)

	// Test 2: Repeat the same request (cache hit)
	fmt.Println("Test 2: Repeat same request (cache hit)")
	fmt.Println("─────────────────────────────────────")

	start = time.Now()
	resp2, err := cachedModel.Chat(ctx, req1)
	elapsed2 := time.Since(start)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp2.FirstContent())
	fmt.Printf("Latency: %v\n", elapsed2)
	fmt.Printf("Tokens: %d\n\n", resp2.Usage.TotalTokens)

	// Test 3: Different request (cache miss)
	fmt.Println("Test 3: Different request (cache miss)")
	fmt.Println("─────────────────────────────────────")

	req3 := protocol.ChatRequest{
		Model: *model,
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: "What is deep learning?"},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	start = time.Now()
	resp3, err := cachedModel.Chat(ctx, req3)
	elapsed3 := time.Since(start)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp3.FirstContent())
	fmt.Printf("Latency: %v\n", elapsed3)
	fmt.Printf("Tokens: %d\n\n", resp3.Usage.TotalTokens)

	// Test 4: Repeat first request again (cache hit)
	fmt.Println("Test 4: Repeat first request again (cache hit)")
	fmt.Println("─────────────────────────────────────")

	start = time.Now()
	resp4, err := cachedModel.Chat(ctx, req1)
	elapsed4 := time.Since(start)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp4.FirstContent())
	fmt.Printf("Latency: %v\n", elapsed4)
	fmt.Printf("Tokens: %d\n\n", resp4.Usage.TotalTokens)

	// Test 5: Different temperature (cache miss)
	fmt.Println("Test 5: Same message, different temperature (cache miss)")
	fmt.Println("─────────────────────────────────────")

	req5 := protocol.ChatRequest{
		Model: *model,
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: "What is machine learning?"},
		},
		Temperature: 0.1, // Different temperature
		MaxTokens:   100,
	}

	start = time.Now()
	resp5, err := cachedModel.Chat(ctx, req5)
	elapsed5 := time.Since(start)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp5.FirstContent())
	fmt.Printf("Latency: %v\n", elapsed5)
	fmt.Printf("Tokens: %d\n\n", resp5.Usage.TotalTokens)

	// Print cache statistics
	fmt.Println("=== Cache Statistics ===")
	fmt.Println("─────────────────────────────────────")

	stats := cache.Stats()
	fmt.Printf("Total Requests: %d\n", stats.TotalRequests)
	fmt.Printf("Cache Hits: %d\n", stats.CacheHits)
	fmt.Printf("Cache Misses: %d\n", stats.CacheMisses)
	fmt.Printf("Hit Rate: %.2f%%\n", stats.HitRate()*100)
	fmt.Printf("Cache Entries: %d\n", stats.EntryCount)
	fmt.Printf("Cache Size: %d bytes\n", stats.TotalSize)
	fmt.Printf("Evictions: %d\n\n", stats.EvictionCount)

	// Calculate metrics
	cacheMetrics := &metrics.CacheMetrics{
		TotalRequests:  stats.TotalRequests,
		CacheHits:      stats.CacheHits,
		CacheMisses:    stats.CacheMisses,
		HitRate:        stats.HitRate(),
		TotalSize:      stats.TotalSize,
		EntryCount:     stats.EntryCount,
		EvictionCount:  stats.EvictionCount,
		MaxSize:        100 * 1024 * 1024,
		CurrentSize:    stats.TotalSize,
	}
	cacheMetrics.CalculateUtilizationRate()
	cacheMetrics.CalculateAvgEntrySize()

	fmt.Println("=== Cache Metrics ===")
	fmt.Println("─────────────────────────────────────")
	fmt.Printf("Utilization Rate: %.2f%%\n", cacheMetrics.UtilizationRate*100)
	fmt.Printf("Avg Entry Size: %d bytes\n", cacheMetrics.AvgEntrySize)

	// Performance comparison
	fmt.Println("\n=== Performance Comparison ===")
	fmt.Println("─────────────────────────────────────")
	fmt.Printf("First request (cache miss): %v\n", elapsed1)
	fmt.Printf("Second request (cache hit): %v\n", elapsed2)
	fmt.Printf("Speedup: %.0fx faster\n", float64(elapsed1)/float64(elapsed2))
	fmt.Printf("Time saved: %v\n", elapsed1-elapsed2)

	// Test streaming (should bypass cache)
	fmt.Println("\n=== Streaming Test (bypasses cache) ===")
	fmt.Println("─────────────────────────────────────")

	streamReq := protocol.ChatRequest{
		Model: *model,
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: "What is AI?"},
		},
		MaxTokens: 100,
	}

	start = time.Now()
	stream, err := cachedModel.StreamChat(ctx, streamReq)
	if err != nil {
		log.Fatalf("StreamChat failed: %v", err)
	}

	fmt.Print("Stream response: ")
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Stream error: %v", err)
		}
		fmt.Print(chunk.Delta)
	}
	stream.Close()

	elapsedStream := time.Since(start)
	fmt.Printf("\n\nStream latency: %v\n", elapsedStream)
	fmt.Println("Note: Streaming responses are NOT cached\n")

	// Final cache stats
	finalStats := cache.Stats()
	fmt.Println("=== Final Cache Statistics ===")
	fmt.Println("─────────────────────────────────────")
	fmt.Printf("Total Requests: %d\n", finalStats.TotalRequests)
	fmt.Printf("Cache Hits: %d\n", finalStats.CacheHits)
	fmt.Printf("Cache Misses: %d\n", finalStats.CacheMisses)
	fmt.Printf("Final Hit Rate: %.2f%%\n", finalStats.HitRate()*100)
}
