package metrics

import "time"

// CallMetrics reports lightweight performance indicators for a single LLM call.
type CallMetrics struct {
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	StartAt          time.Time `json:"start_at"`
	FirstAt          time.Time `json:"first_at,omitempty"`
	EndAt            time.Time `json:"end_at,omitempty"`
	Bytes            int       `json:"bytes"`
	Chunks           int       `json:"chunks"`
	RequestBytes     int       `json:"request_bytes,omitempty"`
	ResponseBytes    int       `json:"response_bytes,omitempty"`
	HTTPStatus       int       `json:"http_status,omitempty"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	Error            string    `json:"error,omitempty"`
	CacheHit         bool      `json:"cache_hit,omitempty"`
	CacheSize        int       `json:"cache_size,omitempty"`
}

// CacheMetrics reports cache performance indicators
type CacheMetrics struct {
	TotalRequests   int64   `json:"total_requests"`
	CacheHits       int64   `json:"cache_hits"`
	CacheMisses     int64   `json:"cache_misses"`
	HitRate         float64 `json:"hit_rate"`
	TotalSize       int64   `json:"total_size"`
	EntryCount      int     `json:"entry_count"`
	EvictionCount   int64   `json:"eviction_count"`
	AvgEntrySize    int64   `json:"avg_entry_size"`
	MaxSize         int64   `json:"max_size"`
	CurrentSize     int64   `json:"current_size"`
	UtilizationRate float64 `json:"utilization_rate"`
}

// CalculateUtilizationRate calculates cache utilization rate
func (cm *CacheMetrics) CalculateUtilizationRate() {
	if cm.MaxSize > 0 {
		cm.UtilizationRate = float64(cm.CurrentSize) / float64(cm.MaxSize)
	}
}

// CalculateAvgEntrySize calculates average entry size
func (cm *CacheMetrics) CalculateAvgEntrySize() {
	if cm.EntryCount > 0 {
		cm.AvgEntrySize = cm.TotalSize / int64(cm.EntryCount)
	}
}

// Latency returns end-to-end latency (EndAt-StartAt) if available.
func (m CallMetrics) Latency() time.Duration {
	if m.StartAt.IsZero() || m.EndAt.IsZero() {
		return 0
	}
	return m.EndAt.Sub(m.StartAt)
}

// FirstTokenLatency returns time to first delta/token if available.
func (m CallMetrics) FirstTokenLatency() time.Duration {
	if m.StartAt.IsZero() || m.FirstAt.IsZero() {
		return 0
	}
	return m.FirstAt.Sub(m.StartAt)
}
