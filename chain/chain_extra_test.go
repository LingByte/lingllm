package chain

import (
	"context"
	"testing"

	"github.com/LingByte/lingllm/metrics"
	"github.com/LingByte/lingllm/protocol"
)

func TestBuilderAddProcessor(t *testing.T) {
	model := &stubModel{chatResp: &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Content: "built"}}},
	}}
	c := NewBuilder("proc-chain").
		AddModel("m", model).
		AddProcessor("p", func(ctx context.Context, resp *protocol.ChatResponse) (*protocol.ChatResponse, error) {
			resp.Choices[0].Message.Content = "processed"
			return resp, nil
		}).
		Build()
	if c.Name() != "proc-chain" {
		t.Errorf("unexpected name: %s", c.Name())
	}
}

func TestNodeNames(t *testing.T) {
	if NewProcessorNode("p", nil).Name() != "p" {
		t.Error("processor name mismatch")
	}
	if NewStreamProcessorNode("s", nil).Name() != "s" {
		t.Error("stream processor name mismatch")
	}
}

func TestProcessorNodeCollectError(t *testing.T) {
	node := NewProcessorNode("p", func(ctx context.Context, resp *protocol.ChatResponse) (*protocol.ChatResponse, error) {
		return resp, nil
	})
	_, err := node.Collect(context.Background(), &errorStreamReader{})
	if err == nil {
		t.Fatal("expected collect error")
	}
}

type errorStreamReader struct{}

func (e *errorStreamReader) Recv() (*protocol.ChatStreamChunk, error) { return nil, context.Canceled }
func (e *errorStreamReader) Close() error                           { return nil }
func (e *errorStreamReader) Metrics() metrics.CallMetrics           { return metrics.CallMetrics{} }

func TestChainStreamEmpty(t *testing.T) {
	c := New("empty")
	_, err := c.Stream(context.Background(), protocol.ChatRequest{
		Model: "m", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChainStreamNodeError(t *testing.T) {
	model := &stubModel{chatErr: context.Canceled}
	c := New("err", NewModelNode("m", model), NewModelNode("m2", &stubModel{stream: &stubStream{}}))
	_, err := c.Stream(context.Background(), protocol.ChatRequest{
		Model: "m", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessingStreamProcessorError(t *testing.T) {
	upstream := &stubStream{chunks: []*protocol.ChatStreamChunk{{Delta: "a"}}}
	ps := NewProcessingStream(upstream, StreamProcessorFunc(func(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error) {
		return nil, context.Canceled
	}))
	_, err := ps.Recv()
	if err != context.Canceled {
		t.Fatalf("expected canceled, got %v", err)
	}
}

func TestProcessingStreamClosed(t *testing.T) {
	ps := &ProcessingStream{closed: true}
	_, err := ps.Recv()
	if err == nil {
		t.Fatal("expected EOF on closed stream")
	}
}

func TestStreamProcessorFunc(t *testing.T) {
	fn := StreamProcessorFunc(func(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error) {
		return chunk, nil
	})
	chunk, err := fn.ProcessChunk(context.Background(), &protocol.ChatStreamChunk{Delta: "x"})
	if err != nil || chunk.Delta != "x" {
		t.Fatalf("ProcessChunk failed: %+v err=%v", chunk, err)
	}
}
