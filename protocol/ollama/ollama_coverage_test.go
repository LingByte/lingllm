package ollama

import (
	"testing"
	"time"

	"github.com/LingByte/lingllm/metrics"
)

func TestOllamaStreamMetrics(t *testing.T) {
	now := time.Now()
	s := &ollamaStream{
		startAt: now, firstAt: now, endAt: now, model: "llama3",
		chunks: 2, bytes: 50, httpStatus: 200, requestBytes: 10,
	}
	m := s.Metrics()
	if m.Provider != "ollama" || m.Model != "llama3" || m.Chunks != 2 {
		t.Errorf("unexpected metrics: %+v", m)
	}
	_ = metrics.CallMetrics{}
}
