package protocol

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CacheEntry represents a cached response with metadata
type CacheEntry struct {
	Response  *ChatResponse
	CreatedAt time.Time
	ExpiresAt time.Time
	HitCount  int
	Size      int
}

// IsExpired checks if the cache entry has expired
func (ce *CacheEntry) IsExpired() bool {
	return time.Now().After(ce.ExpiresAt)
}

// ResponseCache defines the interface for caching chat responses
type ResponseCache interface {
	Get(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	Set(ctx context.Context, req *ChatRequest, resp *ChatResponse) error
	Delete(ctx context.Context, req *ChatRequest) error
	Clear(ctx context.Context) error
	Stats() CacheStats
}

// CacheStats provides cache statistics
type CacheStats struct {
	TotalRequests int64
	CacheHits     int64
	CacheMisses   int64
	TotalSize     int64
	EntryCount    int
	HitRateValue  float64
	EvictionCount int64
}

// HitRate calculates the cache hit rate
func (cs *CacheStats) HitRate() float64 {
	if cs.TotalRequests == 0 {
		return 0
	}
	return float64(cs.CacheHits) / float64(cs.TotalRequests)
}

// MemoryCache is an in-memory implementation of ResponseCache
type MemoryCache struct {
	mu            sync.RWMutex
	cache         map[string]*CacheEntry
	maxSize       int64
	currentSize   int64
	defaultTTL    time.Duration
	totalRequests int64
	cacheHits     int64
	cacheMisses   int64
	evictionCount int64
}

// NewMemoryCache creates a new in-memory cache
func NewMemoryCache(maxSize int64, defaultTTL time.Duration) *MemoryCache {
	mc := &MemoryCache{
		cache:      make(map[string]*CacheEntry),
		maxSize:    maxSize,
		defaultTTL: defaultTTL,
	}
	// Start cleanup goroutine
	go mc.cleanupExpired()
	return mc
}

// Get retrieves a cached response
func (mc *MemoryCache) Get(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	key := mc.hashRequest(req)

	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.totalRequests++

	entry, exists := mc.cache[key]
	if !exists {
		mc.cacheMisses++
		return nil, nil
	}

	if entry.IsExpired() {
		delete(mc.cache, key)
		mc.currentSize -= int64(entry.Size)
		mc.cacheMisses++
		return nil, nil
	}

	entry.HitCount++
	mc.cacheHits++

	return entry.Response, nil
}

// Set stores a response in the cache
func (mc *MemoryCache) Set(ctx context.Context, req *ChatRequest, resp *ChatResponse) error {
	if resp == nil {
		return fmt.Errorf("cannot cache nil response")
	}

	key := mc.hashRequest(req)
	respBytes, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	size := int(int64(len(respBytes)))

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Check if we need to evict entries
	if mc.currentSize+int64(size) > mc.maxSize {
		mc.evictLRU()
	}

	entry := &CacheEntry{
		Response:  resp,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(mc.defaultTTL),
		HitCount:  0,
		Size:      size,
	}

	// Remove old entry if exists
	if oldEntry, exists := mc.cache[key]; exists {
		mc.currentSize -= int64(oldEntry.Size)
	}

	mc.cache[key] = entry
	mc.currentSize += int64(size)

	return nil
}

// Delete removes a cached response
func (mc *MemoryCache) Delete(ctx context.Context, req *ChatRequest) error {
	key := mc.hashRequest(req)

	mc.mu.Lock()
	defer mc.mu.Unlock()

	if entry, exists := mc.cache[key]; exists {
		delete(mc.cache, key)
		mc.currentSize -= int64(entry.Size)
	}

	return nil
}

// Clear removes all cached responses
func (mc *MemoryCache) Clear(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.cache = make(map[string]*CacheEntry)
	mc.currentSize = 0
	mc.totalRequests = 0
	mc.cacheHits = 0
	mc.cacheMisses = 0
	mc.evictionCount = 0

	return nil
}

// Stats returns cache statistics
func (mc *MemoryCache) Stats() CacheStats {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	hitRate := 0.0
	if mc.totalRequests > 0 {
		hitRate = float64(mc.cacheHits) / float64(mc.totalRequests)
	}

	return CacheStats{
		TotalRequests: mc.totalRequests,
		CacheHits:     mc.cacheHits,
		CacheMisses:   mc.cacheMisses,
		TotalSize:     mc.currentSize,
		EntryCount:    len(mc.cache),
		HitRateValue:  hitRate,
		EvictionCount: mc.evictionCount,
	}
}

// hashRequest creates a hash key for a request
func (mc *MemoryCache) hashRequest(req *ChatRequest) string {
	// Create a normalized request for hashing
	normalized := struct {
		Model       string
		Messages    []Message
		Tools       []Tool
		Temperature float32
		TopP        float32
		MaxTokens   int
	}{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
	}

	data, _ := json.Marshal(normalized)
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

// evictLRU removes the least recently used entry
func (mc *MemoryCache) evictLRU() {
	var lruKey string
	var lruEntry *CacheEntry
	var minHits = int(^uint(0) >> 1) // max int

	// Find the entry with the least hits
	for key, entry := range mc.cache {
		if entry.HitCount < minHits {
			minHits = entry.HitCount
			lruKey = key
			lruEntry = entry
		}
	}

	if lruEntry != nil {
		delete(mc.cache, lruKey)
		mc.currentSize -= int64(lruEntry.Size)
		mc.evictionCount++
	}
}

// cleanupExpired periodically removes expired entries
func (mc *MemoryCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		mc.mu.Lock()

		now := time.Now()
		for key, entry := range mc.cache {
			if now.After(entry.ExpiresAt) {
				delete(mc.cache, key)
				mc.currentSize -= int64(entry.Size)
			}
		}

		mc.mu.Unlock()
	}
}

// CachedModel wraps a ChatModel with caching
type CachedModel struct {
	model ChatModel
	cache ResponseCache
}

// NewCachedModel creates a new cached model
func NewCachedModel(model ChatModel, cache ResponseCache) *CachedModel {
	return &CachedModel{
		model: model,
		cache: cache,
	}
}

// Name returns the model name
func (cm *CachedModel) Name() string {
	return cm.model.Name()
}

// Chat executes a chat request with caching
func (cm *CachedModel) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Try to get from cache
	cachedResp, err := cm.cache.Get(ctx, &req)
	if err == nil && cachedResp != nil {
		return cachedResp, nil
	}

	// Call the underlying model
	resp, err := cm.model.Chat(ctx, req)
	if err != nil {
		return resp, err
	}

	// Cache the response
	if resp != nil {
		_ = cm.cache.Set(ctx, &req, resp)
	}

	return resp, nil
}

// StreamChat executes a streaming chat request
func (cm *CachedModel) StreamChat(ctx context.Context, req ChatRequest) (ChatStream, error) {
	// Streaming responses are not cached
	return cm.model.StreamChat(ctx, req)
}
