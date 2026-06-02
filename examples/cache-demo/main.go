package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/LingByte/lingllm/metrics"
	"github.com/LingByte/lingllm/protocol"
)

// MockChatModel is a mock implementation for demonstration
type MockChatModel struct {
	callCount int
}

func (m *MockChatModel) Name() string {
	return "mock-model"
}

func (m *MockChatModel) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	m.callCount++
	time.Sleep(100 * time.Millisecond) // Simulate API latency

	return &protocol.ChatResponse{
		Choices: []protocol.Choice{
			{
				Message: protocol.Message{
					Role:    protocol.RoleAssistant,
					Content: "This is a mock response",
				},
			},
		},
		Usage: protocol.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *MockChatModel) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	return nil, fmt.Errorf("streaming not supported in this mock")
}

func main() {
	// Create a mock model
	mockModel := &MockChatModel{}

	// Create a memory cache with 10MB size and 1 hour TTL
	cache := protocol.NewMemoryCache(10*1024*1024, 1*time.Hour)

	// Wrap the model with caching
	cachedModel := protocol.NewCachedModel(mockModel, cache)

	// Create a sample request
	req := protocol.ChatRequest{
		Model: "gpt-4",
		Messages: []protocol.Message{
			{
				Role:    protocol.RoleUser,
				Content: "What is the capital of France?",
			},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	ctx := context.Background()

	fmt.Println("=== Cache Demo ===")

	// First request - cache miss
	fmt.Println("1. First request (cache miss):")
	start := time.Now()
	resp1, err := cachedModel.Chat(ctx, req)
	elapsed1 := time.Since(start)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Printf("   Response: %s\n", resp1.FirstContent())
	fmt.Printf("   Latency: %v\n", elapsed1)
	fmt.Printf("   Model calls: %d\n\n", mockModel.callCount)

	// Second request - cache hit
	fmt.Println("2. Second request (cache hit):")
	start = time.Now()
	resp2, err := cachedModel.Chat(ctx, req)
	elapsed2 := time.Since(start)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Printf("   Response: %s\n", resp2.FirstContent())
	fmt.Printf("   Latency: %v\n", elapsed2)
	fmt.Printf("   Model calls: %d (unchanged)\n\n", mockModel.callCount)

	// Print cache statistics
	fmt.Println("3. Cache Statistics:")
	stats := cache.Stats()
	fmt.Printf("   Total Requests: %d\n", stats.TotalRequests)
	fmt.Printf("   Cache Hits: %d\n", stats.CacheHits)
	fmt.Printf("   Cache Misses: %d\n", stats.CacheMisses)
	fmt.Printf("   Hit Rate: %.2f%%\n", stats.HitRate()*100)
	fmt.Printf("   Entries: %d\n", stats.EntryCount)
	fmt.Printf("   Total Size: %d bytes\n", stats.TotalSize)
	fmt.Printf("   Evictions: %d\n\n", stats.EvictionCount)

	// Convert to CacheMetrics
	fmt.Println("4. Cache Metrics:")
	cacheMetrics := &metrics.CacheMetrics{
		TotalRequests: stats.TotalRequests,
		CacheHits:     stats.CacheHits,
		CacheMisses:   stats.CacheMisses,
		HitRate:       stats.HitRate(),
		TotalSize:     stats.TotalSize,
		EntryCount:    stats.EntryCount,
		EvictionCount: stats.EvictionCount,
		MaxSize:       10 * 1024 * 1024,
		CurrentSize:   stats.TotalSize,
	}
	cacheMetrics.CalculateUtilizationRate()
	cacheMetrics.CalculateAvgEntrySize()

	fmt.Printf("   Utilization Rate: %.2f%%\n", cacheMetrics.UtilizationRate*100)
	fmt.Printf("   Avg Entry Size: %d bytes\n", cacheMetrics.AvgEntrySize)
	fmt.Printf("   Time Saved: ~%v (cached vs uncached)\n\n", elapsed1-elapsed2)

	// Test different requests
	fmt.Println("5. Testing different requests:")
	req2 := protocol.ChatRequest{
		Model: "gpt-4",
		Messages: []protocol.Message{
			{
				Role:    protocol.RoleUser,
				Content: "What is the capital of Germany?",
			},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	start = time.Now()
	resp3, _ := cachedModel.Chat(ctx, req2)
	elapsed3 := time.Since(start)
	fmt.Printf("   New request latency: %v\n", elapsed3)
	fmt.Printf("   Response: %s\n", resp3.FirstContent())
	fmt.Printf("   Model calls: %d\n\n", mockModel.callCount)

	// Final statistics
	fmt.Println("6. Final Cache Statistics:")
	finalStats := cache.Stats()
	fmt.Printf("   Total Requests: %d\n", finalStats.TotalRequests)
	fmt.Printf("   Cache Hits: %d\n", finalStats.CacheHits)
	fmt.Printf("   Cache Misses: %d\n", finalStats.CacheMisses)
	fmt.Printf("   Hit Rate: %.2f%%\n", finalStats.HitRate()*100)
	fmt.Printf("   Entries: %d\n", finalStats.EntryCount)

	// Demonstrate cache deletion
	fmt.Println("\n7. Deleting first request from cache:")
	cache.Delete(ctx, &req)
	afterDeleteStats := cache.Stats()
	fmt.Printf("   Entries after delete: %d\n", afterDeleteStats.EntryCount)

	// Test cache hit after deletion
	start = time.Now()
	_, _ = cachedModel.Chat(ctx, req)
	elapsed4 := time.Since(start)
	fmt.Printf("   Request latency after delete: %v (cache miss)\n", elapsed4)
	fmt.Printf("   Model calls: %d\n", mockModel.callCount)
}
