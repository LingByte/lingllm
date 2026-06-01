package protocol

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/LingByte/lingllm/metrics"
)

func TestCollectStreamRecvError(t *testing.T) {
	stream := &errorStream{err: context.Canceled}
	_, err := CollectStream(context.Background(), stream)
	if err != context.Canceled {
		t.Fatalf("expected canceled, got %v", err)
	}
}

type errorStream struct {
	err error
}

func (s *errorStream) Recv() (*ChatStreamChunk, error) { return nil, s.err }
func (s *errorStream) Close() error                    { return nil }
func (s *errorStream) Metrics() metrics.CallMetrics    { return metrics.CallMetrics{} }

func TestPipeReaderClosed(t *testing.T) {
	reader, _ := Pipe(1)
	reader.Close()
	_, err := reader.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestMergeStreamReadersError(t *testing.T) {
	bad := &errorStream{err: errors.New("boom")}
	good := StreamReaderFromArray([]*ChatStreamChunk{{Delta: "a"}})
	merged := MergeStreamReaders(bad, good)
	_, err := merged.Recv()
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestTransformStreamEOF(t *testing.T) {
	reader := StreamReaderFromArray(nil)
	out := TransformStream(context.Background(), reader, StreamTransformerFunc(func(ctx context.Context, chunk *ChatStreamChunk) (*ChatStreamChunk, error) {
		return chunk, nil
	}))
	_, err := out.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestTransformStreamTransformError(t *testing.T) {
	reader := StreamReaderFromArray([]*ChatStreamChunk{{Delta: "x"}})
	out := TransformStream(context.Background(), reader, StreamTransformerFunc(func(ctx context.Context, chunk *ChatStreamChunk) (*ChatStreamChunk, error) {
		return nil, context.Canceled
	}))
	_, err := out.Recv()
	if err != context.Canceled {
		t.Fatalf("expected canceled, got %v", err)
	}
}

func TestNamedMergedReaderCloseError(t *testing.T) {
	merged := MergeNamedStreamReaders(map[string]StreamReader{
		"a": StreamReaderFromArray(nil),
	})
	if err := merged.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
