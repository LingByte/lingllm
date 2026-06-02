package protocol

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCacheGet(t *testing.T) {
	cache := NewMemoryCache(1024*1024, 1*time.Hour)
	defer cache.Clear(context.Background())

	req := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	}

	resp := &ChatResponse{
		Choices: []Choice{
			{Message: Message{Role: RoleAssistant, Content: "hi"}},
		},
	}

	// Cache miss
	cachedResp, err := cache.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if cachedResp != nil {
		t.Fatal("expected nil for cache miss")
	}

	// Set cache
	err = cache.Set(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Cache hit
	cachedResp, err = cache.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if cachedResp == nil {
		t.Fatal("expected cached response")
	}
	if cachedResp.FirstContent() != "hi" {
		t.Errorf("unexpected content: %s", cachedResp.FirstContent())
	}
}

func TestMemoryCacheExpiration(t *testing.T) {
	cache := NewMemoryCache(1024*1024, 100*time.Millisecond)
	defer cache.Clear(context.Background())

	req := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	}

	resp := &ChatResponse{
		Choices: []Choice{
			{Message: Message{Role: RoleAssistant, Content: "hi"}},
		},
	}

	// Set cache
	err := cache.Set(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Cache hit immediately
	cachedResp, _ := cache.Get(context.Background(), req)
	if cachedResp == nil {
		t.Fatal("expected cached response")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Cache miss after expiration
	cachedResp, _ = cache.Get(context.Background(), req)
	if cachedResp != nil {
		t.Fatal("expected nil after expiration")
	}
}

func TestMemoryCacheDelete(t *testing.T) {
	cache := NewMemoryCache(1024*1024, 1*time.Hour)
	defer cache.Clear(context.Background())

	req := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	}

	resp := &ChatResponse{
		Choices: []Choice{
			{Message: Message{Role: RoleAssistant, Content: "hi"}},
		},
	}

	// Set cache
	cache.Set(context.Background(), req, resp)

	// Verify cache hit
	cachedResp, _ := cache.Get(context.Background(), req)
	if cachedResp == nil {
		t.Fatal("expected cached response")
	}

	// Delete
	err := cache.Delete(context.Background(), req)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify cache miss
	cachedResp, _ = cache.Get(context.Background(), req)
	if cachedResp != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestMemoryCacheStats(t *testing.T) {
	cache := NewMemoryCache(1024*1024, 1*time.Hour)
	defer cache.Clear(context.Background())

	req := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	}

	resp := &ChatResponse{
		Choices: []Choice{
			{Message: Message{Role: RoleAssistant, Content: "hi"}},
		},
	}

	// Set cache
	cache.Set(context.Background(), req, resp)

	// Multiple hits
	cache.Get(context.Background(), req)
	cache.Get(context.Background(), req)
	cache.Get(context.Background(), req)

	// Miss
	cache.Get(context.Background(), &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "different"},
		},
	})

	stats := cache.Stats()

	if stats.TotalRequests != 4 {
		t.Errorf("expected 4 total requests, got %d", stats.TotalRequests)
	}
	if stats.CacheHits != 3 {
		t.Errorf("expected 3 cache hits, got %d", stats.CacheHits)
	}
	if stats.CacheMisses != 1 {
		t.Errorf("expected 1 cache miss, got %d", stats.CacheMisses)
	}
	if stats.EntryCount != 1 {
		t.Errorf("expected 1 entry, got %d", stats.EntryCount)
	}

	expectedHitRate := 3.0 / 4.0
	actualHitRate := stats.HitRate()
	if actualHitRate < expectedHitRate-0.01 || actualHitRate > expectedHitRate+0.01 {
		t.Errorf("expected hit rate ~%.2f, got %.2f", expectedHitRate, actualHitRate)
	}
}

func TestMemoryCacheEviction(t *testing.T) {
	// Small cache size to trigger eviction
	cache := NewMemoryCache(100, 1*time.Hour)
	defer cache.Clear(context.Background())

	// Add entries until eviction occurs
	for i := 0; i < 10; i++ {
		req := &ChatRequest{
			Model: "gpt-4",
			Messages: []Message{
				{Role: RoleUser, Content: "message " + string(rune(i))},
			},
		}

		resp := &ChatResponse{
			Choices: []Choice{
				{Message: Message{Role: RoleAssistant, Content: "response " + string(rune(i))}},
			},
		}

		cache.Set(context.Background(), req, resp)
	}

	stats := cache.Stats()
	if stats.EvictionCount == 0 {
		t.Fatal("expected evictions to occur")
	}
}

func TestMemoryCacheClear(t *testing.T) {
	cache := NewMemoryCache(1024*1024, 1*time.Hour)

	req := &ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	}

	resp := &ChatResponse{
		Choices: []Choice{
			{Message: Message{Role: RoleAssistant, Content: "hi"}},
		},
	}

	// Set cache
	cache.Set(context.Background(), req, resp)

	// Verify cache hit
	cachedResp, _ := cache.Get(context.Background(), req)
	if cachedResp == nil {
		t.Fatal("expected cached response")
	}

	// Clear
	err := cache.Clear(context.Background())
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify stats are reset
	stats := cache.Stats()
	if stats.EntryCount != 0 {
		t.Errorf("expected 0 entries after clear, got %d", stats.EntryCount)
	}
	if stats.TotalSize != 0 {
		t.Errorf("expected 0 size after clear, got %d", stats.TotalSize)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after clear, got %d", stats.TotalRequests)
	}
}

func TestCachedModel(t *testing.T) {
	// Mock model
	mockModel := &StubChatModel{
		resp: &ChatResponse{
			Choices: []Choice{
				{Message: Message{Role: RoleAssistant, Content: "response"}},
			},
		},
	}

	cache := NewMemoryCache(1024*1024, 1*time.Hour)
	cachedModel := NewCachedModel(mockModel, cache)

	req := ChatRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	}

	// First call - should hit the model
	resp1, err := cachedModel.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp1 == nil {
		t.Fatal("expected response")
	}

	callCount1 := mockModel.callCount

	// Second call - should hit the cache
	resp2, err := cachedModel.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp2 == nil {
		t.Fatal("expected response")
	}

	callCount2 := mockModel.callCount

	// Model should not be called again
	if callCount2 != callCount1 {
		t.Errorf("expected model to be called once, but was called %d times", callCount2)
	}

	// Verify cache stats
	stats := cache.Stats()
	if stats.CacheHits != 1 {
		t.Errorf("expected 1 cache hit, got %d", stats.CacheHits)
	}
}

// StubChatModel is a test stub for ChatModel
type StubChatModel struct {
	resp      *ChatResponse
	callCount int
}

func (m *StubChatModel) Name() string {
	return "stub"
}

func (m *StubChatModel) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	m.callCount++
	return m.resp, nil
}

func (m *StubChatModel) StreamChat(ctx context.Context, req ChatRequest) (ChatStream, error) {
	return nil, nil
}
