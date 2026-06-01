package protocol

import (
	"context"
	"io"
	"testing"

	"github.com/LingByte/lingllm/metrics"
)

func TestMergedReaderMetrics(t *testing.T) {
	merged := MergeStreamReaders(StreamReaderFromArray([]*ChatStreamChunk{{Delta: "a"}}))
	_, _ = merged.Recv()
	m := merged.Metrics()
	if m.Chunks != 1 {
		t.Errorf("expected 1 chunk, got %d", m.Chunks)
	}
}

func TestConvertedReaderCloseAndMetrics(t *testing.T) {
	upstream := StreamReaderFromArray([]*ChatStreamChunk{{Delta: "a"}})
	reader := NewConvertedReader(context.Background(), upstream, func(ctx context.Context, chunk *ChatStreamChunk) (*ChatStreamChunk, error) {
		return chunk, nil
	})
	_, _ = reader.Recv()
	if err := reader.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if reader.Metrics().Chunks != 1 {
		t.Errorf("expected chunk count 1")
	}
}

func TestNamedMergedReaderMetrics(t *testing.T) {
	merged := MergeNamedStreamReaders(map[string]StreamReader{
		"a": StreamReaderFromArray([]*ChatStreamChunk{{Delta: "1"}}),
	})
	_, _ = merged.Recv()
	if merged.Metrics().Chunks != 1 {
		t.Errorf("expected 1 chunk")
	}
}

func TestTransformedReaderCloseAndMetrics(t *testing.T) {
	reader := StreamReaderFromArray([]*ChatStreamChunk{{Delta: "a"}})
	out := TransformStream(context.Background(), reader, StreamTransformerFunc(func(ctx context.Context, chunk *ChatStreamChunk) (*ChatStreamChunk, error) {
		return chunk, nil
	}))
	_, _ = out.Recv()
	if err := out.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if out.Metrics().Chunks != 1 {
		t.Errorf("expected 1 chunk")
	}
}

func TestPipeWriterCloseTwice(t *testing.T) {
	_, writer := Pipe(1)
	if err := writer.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestPipeReaderRecvAfterClose(t *testing.T) {
	reader, writer := Pipe(1)
	reader.Close()
	writer.Close()
	_, err := reader.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestCollectStreamSetsEndAt(t *testing.T) {
	stream := &metricsStreamWithModel{chunks: []*ChatStreamChunk{{Delta: "x"}}}
	resp, err := CollectStream(context.Background(), stream)
	if err != nil || resp.FirstContent() != "x" {
		t.Fatalf("CollectStream failed: resp=%+v err=%v", resp, err)
	}
}

type metricsStreamWithModel struct {
	chunks []*ChatStreamChunk
	idx    int
}

func (s *metricsStreamWithModel) Recv() (*ChatStreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}
func (s *metricsStreamWithModel) Close() error { return nil }
func (s *metricsStreamWithModel) Metrics() metrics.CallMetrics {
	return metrics.CallMetrics{Model: "test-model", Provider: "test"}
}
