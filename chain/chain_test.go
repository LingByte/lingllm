package chain

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/LingByte/lingllm/metrics"
	"github.com/LingByte/lingllm/protocol"
)

type stubModel struct {
	chatResp  *protocol.ChatResponse
	chatErr   error
	stream    protocol.ChatStream
	streamErr error
}

func (m *stubModel) Name() string { return "stub" }

func (m *stubModel) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	if m.chatErr != nil {
		return nil, m.chatErr
	}
	return m.chatResp, nil
}

func (m *stubModel) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	return m.stream, nil
}

type stubStream struct {
	chunks []*protocol.ChatStreamChunk
	idx    int
	closed bool
}

func (s *stubStream) Recv() (*protocol.ChatStreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	chunk := s.chunks[s.idx]
	s.idx++
	return chunk, nil
}

func (s *stubStream) Close() error {
	s.closed = true
	return nil
}

func (s *stubStream) Metrics() metrics.CallMetrics {
	return metrics.CallMetrics{Provider: "stub", Model: "test"}
}

func TestChainInvokeEmpty(t *testing.T) {
	c := New("empty")
	_, err := c.Invoke(context.Background(), protocol.ChatRequest{Model: "m", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error for empty chain")
	}
}

func TestChainInvokeSingleNode(t *testing.T) {
	model := &stubModel{chatResp: &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Role: protocol.RoleAssistant, Content: "hello"}}},
	}}
	c := New("single", NewModelNode("model", model))
	resp, err := c.Invoke(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if resp.FirstContent() != "hello" {
		t.Errorf("unexpected content: %s", resp.FirstContent())
	}
}

func TestChainInvokeMultiNode(t *testing.T) {
	first := &stubModel{chatResp: &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Content: "step1"}}},
	}}
	second := &stubModel{chatResp: &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Content: "step2"}}},
	}}
	c := New("multi", NewModelNode("first", first), NewModelNode("second", second))
	resp, err := c.Invoke(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if resp.FirstContent() != "step2" {
		t.Errorf("expected step2, got %s", resp.FirstContent())
	}
}

func TestChainInvokeNodeError(t *testing.T) {
	model := &stubModel{chatErr: errors.New("boom")}
	c := New("err", NewModelNode("model", model))
	_, err := c.Invoke(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChainStreamSingleNode(t *testing.T) {
	stream := &stubStream{chunks: []*protocol.ChatStreamChunk{{Delta: "hi"}}}
	model := &stubModel{stream: stream}
	c := New("stream", NewModelNode("model", model))
	got, err := c.Stream(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	chunk, err := got.Recv()
	if err != nil || chunk.Delta != "hi" {
		t.Fatalf("unexpected chunk: %+v err=%v", chunk, err)
	}
}

func TestChainStreamMultiNode(t *testing.T) {
	first := &stubModel{chatResp: &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Content: "mid"}}},
	}}
	stream := &stubStream{chunks: []*protocol.ChatStreamChunk{{Delta: "final"}}}
	second := &stubModel{stream: stream}
	c := New("stream-multi", NewModelNode("first", first), NewModelNode("second", second))
	got, err := c.Stream(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	chunk, _ := got.Recv()
	if chunk.Delta != "final" {
		t.Errorf("expected final, got %s", chunk.Delta)
	}
}

func TestChainCollectAndTransform(t *testing.T) {
	reader := protocol.StreamReaderFromArray([]*protocol.ChatStreamChunk{
		{Delta: "hello"},
		{Delta: " world"},
	})
	model := &stubModel{}
	c := New("collect", NewModelNode("model", model))

	resp, err := c.Collect(context.Background(), reader)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if resp.FirstContent() != "hello world" {
		t.Errorf("unexpected content: %s", resp.FirstContent())
	}

	reader2 := protocol.StreamReaderFromArray([]*protocol.ChatStreamChunk{{Delta: "x"}})
	out, err := c.Transform(context.Background(), reader2)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	chunk, _ := out.Recv()
	if chunk.Delta != "x" {
		t.Errorf("unexpected delta: %s", chunk.Delta)
	}
}

func TestProcessorNode(t *testing.T) {
	node := NewProcessorNode("proc", func(ctx context.Context, resp *protocol.ChatResponse) (*protocol.ChatResponse, error) {
		resp.Choices[0].Message.Content = "processed"
		return resp, nil
	})
	resp, err := node.Invoke(context.Background(), protocol.ChatRequest{Model: "m"})
	if err != nil || resp.FirstContent() != "processed" {
		t.Fatalf("Invoke failed: resp=%+v err=%v", resp, err)
	}

	reader := protocol.StreamReaderFromArray([]*protocol.ChatStreamChunk{{Delta: "raw"}})
	collected, err := node.Collect(context.Background(), reader)
	if err != nil || collected.FirstContent() != "processed" {
		t.Fatalf("Collect failed: resp=%+v err=%v", collected, err)
	}

	if _, err := node.Stream(context.Background(), protocol.ChatRequest{}); err == nil {
		t.Fatal("expected stream error")
	}
	if _, err := node.Transform(context.Background(), reader); err == nil {
		t.Fatal("expected transform error")
	}
}

func TestStreamProcessorNode(t *testing.T) {
	node := NewStreamProcessorNode("sp", StreamProcessorFunc(func(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error) {
		chunk.Delta = "!" + chunk.Delta
		return chunk, nil
	}))

	if _, err := node.Invoke(context.Background(), protocol.ChatRequest{}); err == nil {
		t.Fatal("expected invoke error")
	}
	if _, err := node.Stream(context.Background(), protocol.ChatRequest{}); err == nil {
		t.Fatal("expected stream error")
	}
	if _, err := node.Collect(context.Background(), protocol.StreamReaderFromArray(nil)); err == nil {
		t.Fatal("expected collect error")
	}

	reader := protocol.StreamReaderFromArray([]*protocol.ChatStreamChunk{{Delta: "hi"}})
	out, err := node.Transform(context.Background(), reader)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	chunk, _ := out.Recv()
	if chunk.Delta != "!hi" {
		t.Errorf("expected !hi, got %s", chunk.Delta)
	}
}

func TestProcessingStream(t *testing.T) {
	upstream := &stubStream{chunks: []*protocol.ChatStreamChunk{{Delta: "a"}}}
	ps := NewProcessingStream(upstream, StreamProcessorFunc(func(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error) {
		return chunk, nil
	}))
	chunk, err := ps.Recv()
	if err != nil || chunk.Delta != "a" {
		t.Fatalf("Recv failed: chunk=%+v err=%v", chunk, err)
	}
	if _, err := ps.Recv(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
	if err := ps.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if ps.Metrics().Provider != "stub" {
		t.Errorf("unexpected metrics provider: %s", ps.Metrics().Provider)
	}
}

func TestBuilder(t *testing.T) {
	model := &stubModel{chatResp: &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Content: "built"}}},
	}}
	c := NewBuilder("built").AddModel("m", model).Build()

	if c.Name() != "built" {
		t.Errorf("unexpected chain name: %s", c.Name())
	}
	resp, err := c.Invoke(context.Background(), protocol.ChatRequest{
		Model:    "gpt-4",
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil || resp.FirstContent() != "built" {
		t.Fatalf("built chain failed: resp=%+v err=%v", resp, err)
	}
}

func TestChainCollectEmpty(t *testing.T) {
	c := New("empty")
	_, err := c.Collect(context.Background(), protocol.StreamReaderFromArray(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = c.Transform(context.Background(), protocol.StreamReaderFromArray(nil))
	if err == nil {
		t.Fatal("expected error")
	}
}
