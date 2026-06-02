package metrics

import (
	"testing"
)

func TestCacheMetricsCalculateUtilizationRate(t *testing.T) {
	tests := []struct {
		name                string
		currentSize         int64
		maxSize             int64
		expectedUtilization float64
	}{
		{
			name:                "50% utilization",
			currentSize:         500,
			maxSize:             1000,
			expectedUtilization: 0.5,
		},
		{
			name:                "100% utilization",
			currentSize:         1000,
			maxSize:             1000,
			expectedUtilization: 1.0,
		},
		{
			name:                "0% utilization",
			currentSize:         0,
			maxSize:             1000,
			expectedUtilization: 0.0,
		},
		{
			name:                "zero max size",
			currentSize:         500,
			maxSize:             0,
			expectedUtilization: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &CacheMetrics{
				CurrentSize: tt.currentSize,
				MaxSize:     tt.maxSize,
			}
			cm.CalculateUtilizationRate()

			if cm.UtilizationRate != tt.expectedUtilization {
				t.Errorf("expected %.2f, got %.2f", tt.expectedUtilization, cm.UtilizationRate)
			}
		})
	}
}

func TestCacheMetricsCalculateAvgEntrySize(t *testing.T) {
	tests := []struct {
		name            string
		totalSize       int64
		entryCount      int
		expectedAvgSize int64
	}{
		{
			name:            "average size 100",
			totalSize:       1000,
			entryCount:      10,
			expectedAvgSize: 100,
		},
		{
			name:            "single entry",
			totalSize:       500,
			entryCount:      1,
			expectedAvgSize: 500,
		},
		{
			name:            "zero entries",
			totalSize:       1000,
			entryCount:      0,
			expectedAvgSize: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &CacheMetrics{
				TotalSize:  tt.totalSize,
				EntryCount: tt.entryCount,
			}
			cm.CalculateAvgEntrySize()

			if cm.AvgEntrySize != tt.expectedAvgSize {
				t.Errorf("expected %d, got %d", tt.expectedAvgSize, cm.AvgEntrySize)
			}
		})
	}
}

func TestCallMetricsWithCache(t *testing.T) {
	cm := &CallMetrics{
		Provider:  "openai",
		Model:     "gpt-4",
		CacheHit:  true,
		CacheSize: 512,
	}

	if !cm.CacheHit {
		t.Error("expected cache hit to be true")
	}
	if cm.CacheSize != 512 {
		t.Errorf("expected cache size 512, got %d", cm.CacheSize)
	}
}
