package chain

import (
	"context"
	"fmt"
	"io"

	"github.com/LingByte/lingllm/metrics"
	"github.com/LingByte/lingllm/protocol"
)

// Runnable represents a composable unit that can be invoked or streamed.
type Runnable interface {
	Invoke(ctx context.Context, input protocol.ChatRequest) (*protocol.ChatResponse, error)
	Stream(ctx context.Context, input protocol.ChatRequest) (protocol.ChatStream, error)
	Collect(ctx context.Context, reader protocol.StreamReader) (*protocol.ChatResponse, error)
	Transform(ctx context.Context, reader protocol.StreamReader) (protocol.StreamReader, error)
}

// Node represents a single node in a processing chain.
type Node interface {
	Runnable
	Name() string
}

// Chain represents a sequence of operations that can be chained together.
type Chain struct {
	nodes []Node
	name  string
}

// New creates a new chain with the given nodes.
func New(name string, nodes ...Node) *Chain {
	return &Chain{
		nodes: nodes,
		name:  name,
	}
}

// Name returns the chain's name.
func (c *Chain) Name() string {
	return c.name
}

// Invoke executes all nodes in sequence synchronously.
func (c *Chain) Invoke(ctx context.Context, input protocol.ChatRequest) (*protocol.ChatResponse, error) {
	if len(c.nodes) == 0 {
		return nil, fmt.Errorf("chain has no nodes")
	}

	var result *protocol.ChatResponse
	var err error

	for i, node := range c.nodes {
		if i == 0 {
			result, err = node.Invoke(ctx, input)
		} else {
			nextReq := followUpRequest(input, result)
			result, err = node.Invoke(ctx, nextReq)
		}
		if err != nil {
			return nil, fmt.Errorf("chain node %d (%s) failed: %w", i, node.Name(), err)
		}
	}

	return result, nil
}

// Stream executes all nodes in sequence with streaming on the final node.
func (c *Chain) Stream(ctx context.Context, input protocol.ChatRequest) (protocol.ChatStream, error) {
	if len(c.nodes) == 0 {
		return nil, fmt.Errorf("chain has no nodes")
	}

	var result *protocol.ChatResponse
	var err error

	for i, node := range c.nodes {
		if i < len(c.nodes)-1 {
			if i == 0 {
				result, err = node.Invoke(ctx, input)
			} else {
				result, err = node.Invoke(ctx, followUpRequest(input, result))
			}
			if err != nil {
				return nil, fmt.Errorf("chain node %d (%s) failed: %w", i, node.Name(), err)
			}
		} else {
			streamInput := input
			if len(c.nodes) > 1 {
				streamInput = followUpRequest(input, result)
			}
			return node.Stream(ctx, streamInput)
		}
	}

	return nil, fmt.Errorf("chain has no final node")
}

// Collect reads all chunks from a stream into a response.
func (c *Chain) Collect(ctx context.Context, reader protocol.StreamReader) (*protocol.ChatResponse, error) {
	if len(c.nodes) == 0 {
		return nil, fmt.Errorf("chain has no nodes")
	}
	return c.nodes[len(c.nodes)-1].Collect(ctx, reader)
}

// Transform applies a transformation to each chunk in a stream.
func (c *Chain) Transform(ctx context.Context, reader protocol.StreamReader) (protocol.StreamReader, error) {
	if len(c.nodes) == 0 {
		return nil, fmt.Errorf("chain has no nodes")
	}
	return c.nodes[len(c.nodes)-1].Transform(ctx, reader)
}

func followUpRequest(input protocol.ChatRequest, result *protocol.ChatResponse) protocol.ChatRequest {
	return protocol.ChatRequest{
		Model:       input.Model,
		Messages:    []protocol.Message{{Role: protocol.RoleAssistant, Content: result.FirstContent()}},
		MaxTokens:   input.MaxTokens,
		Temperature: input.Temperature,
		TopP:        input.TopP,
		Stop:        input.Stop,
		Metadata:    input.Metadata,
	}
}

// ModelNode wraps a ChatModel as a Node.
type ModelNode struct {
	model protocol.ChatModel
	name  string
}

// NewModelNode creates a new ModelNode.
func NewModelNode(name string, model protocol.ChatModel) *ModelNode {
	return &ModelNode{
		model: model,
		name:  name,
	}
}

// Name returns the node's name.
func (n *ModelNode) Name() string {
	return n.name
}

// Invoke calls the model synchronously.
func (n *ModelNode) Invoke(ctx context.Context, input protocol.ChatRequest) (*protocol.ChatResponse, error) {
	return n.model.Chat(ctx, input)
}

// Stream calls the model with streaming.
func (n *ModelNode) Stream(ctx context.Context, input protocol.ChatRequest) (protocol.ChatStream, error) {
	return n.model.StreamChat(ctx, input)
}

// Collect reads all chunks from a stream into a response.
func (n *ModelNode) Collect(ctx context.Context, reader protocol.StreamReader) (*protocol.ChatResponse, error) {
	return protocol.CollectStream(ctx, reader)
}

// Transform applies a pass-through transformation to each chunk.
func (n *ModelNode) Transform(ctx context.Context, reader protocol.StreamReader) (protocol.StreamReader, error) {
	return protocol.TransformStream(ctx, reader, protocol.StreamTransformerFunc(func(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error) {
		return chunk, nil
	})), nil
}

// ProcessorFunc processes a ChatResponse.
type ProcessorFunc func(ctx context.Context, resp *protocol.ChatResponse) (*protocol.ChatResponse, error)

// ProcessorNode wraps a processor function as a Node.
type ProcessorNode struct {
	fn   ProcessorFunc
	name string
}

// NewProcessorNode creates a new ProcessorNode.
func NewProcessorNode(name string, fn ProcessorFunc) *ProcessorNode {
	return &ProcessorNode{
		fn:   fn,
		name: name,
	}
}

// Name returns the node's name.
func (n *ProcessorNode) Name() string {
	return n.name
}

// Invoke executes the processor function.
func (n *ProcessorNode) Invoke(ctx context.Context, input protocol.ChatRequest) (*protocol.ChatResponse, error) {
	resp := &protocol.ChatResponse{
		Model:   input.Model,
		Choices: []protocol.Choice{{Message: protocol.Message{Role: protocol.RoleUser, Content: ""}}},
	}
	return n.fn(ctx, resp)
}

// Stream is not supported for processor nodes.
func (n *ProcessorNode) Stream(ctx context.Context, input protocol.ChatRequest) (protocol.ChatStream, error) {
	return nil, fmt.Errorf("processor node %s does not support streaming", n.name)
}

// Collect reads all chunks from a stream and applies the processor.
func (n *ProcessorNode) Collect(ctx context.Context, reader protocol.StreamReader) (*protocol.ChatResponse, error) {
	resp, err := protocol.CollectStream(ctx, reader)
	if err != nil {
		return nil, err
	}
	return n.fn(ctx, resp)
}

// Transform is not supported for processor nodes.
func (n *ProcessorNode) Transform(ctx context.Context, reader protocol.StreamReader) (protocol.StreamReader, error) {
	return nil, fmt.Errorf("processor node %s does not support streaming transform", n.name)
}

// StreamProcessor processes chunks from a stream.
type StreamProcessor interface {
	ProcessChunk(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error)
}

// StreamProcessorFunc is a function that processes stream chunks.
type StreamProcessorFunc func(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error)

// ProcessChunk implements StreamProcessor.
func (f StreamProcessorFunc) ProcessChunk(ctx context.Context, chunk *protocol.ChatStreamChunk) (*protocol.ChatStreamChunk, error) {
	return f(ctx, chunk)
}

// StreamProcessorNode wraps a stream processor as a Node.
type StreamProcessorNode struct {
	processor StreamProcessor
	name      string
}

// NewStreamProcessorNode creates a new StreamProcessorNode.
func NewStreamProcessorNode(name string, processor StreamProcessor) *StreamProcessorNode {
	return &StreamProcessorNode{
		processor: processor,
		name:      name,
	}
}

// Name returns the node's name.
func (n *StreamProcessorNode) Name() string {
	return n.name
}

// Invoke is not supported for stream processor nodes.
func (n *StreamProcessorNode) Invoke(ctx context.Context, input protocol.ChatRequest) (*protocol.ChatResponse, error) {
	return nil, fmt.Errorf("stream processor node %s does not support sync invoke", n.name)
}

// Stream is not supported without an upstream stream.
func (n *StreamProcessorNode) Stream(ctx context.Context, input protocol.ChatRequest) (protocol.ChatStream, error) {
	return nil, fmt.Errorf("stream processor node %s requires an upstream stream", n.name)
}

// Collect is not supported for stream processor nodes.
func (n *StreamProcessorNode) Collect(ctx context.Context, reader protocol.StreamReader) (*protocol.ChatResponse, error) {
	return nil, fmt.Errorf("stream processor node %s does not support collect", n.name)
}

// Transform applies the stream processor to each chunk.
func (n *StreamProcessorNode) Transform(ctx context.Context, reader protocol.StreamReader) (protocol.StreamReader, error) {
	return NewProcessingStream(reader, n.processor), nil
}

// ProcessingStream wraps a stream and applies a processor to each chunk.
type ProcessingStream struct {
	upstream  protocol.StreamReader
	processor StreamProcessor
	closed    bool
}

// NewProcessingStream creates a new ProcessingStream.
func NewProcessingStream(upstream protocol.StreamReader, processor StreamProcessor) *ProcessingStream {
	return &ProcessingStream{
		upstream:  upstream,
		processor: processor,
	}
}

// Recv receives and processes the next chunk.
func (s *ProcessingStream) Recv() (*protocol.ChatStreamChunk, error) {
	if s.closed {
		return nil, io.EOF
	}

	chunk, err := s.upstream.Recv()
	if err != nil {
		if err == io.EOF {
			s.closed = true
		}
		return nil, err
	}

	processed, err := s.processor.ProcessChunk(context.Background(), chunk)
	if err != nil {
		return nil, err
	}

	return processed, nil
}

// Close closes the stream.
func (s *ProcessingStream) Close() error {
	s.closed = true
	return s.upstream.Close()
}

// Metrics returns the upstream metrics.
func (s *ProcessingStream) Metrics() metrics.CallMetrics {
	return s.upstream.Metrics()
}

// Builder provides a fluent API for building chains.
type Builder struct {
	nodes []Node
	name  string
}

// NewBuilder creates a new Builder.
func NewBuilder(name string) *Builder {
	return &Builder{
		name:  name,
		nodes: make([]Node, 0),
	}
}

// AddModel adds a ChatModel node to the chain.
func (b *Builder) AddModel(name string, model protocol.ChatModel) *Builder {
	b.nodes = append(b.nodes, NewModelNode(name, model))
	return b
}

// AddProcessor adds a processor node to the chain.
func (b *Builder) AddProcessor(name string, fn ProcessorFunc) *Builder {
	b.nodes = append(b.nodes, NewProcessorNode(name, fn))
	return b
}

// AddNode adds a custom node to the chain.
func (b *Builder) AddNode(node Node) *Builder {
	b.nodes = append(b.nodes, node)
	return b
}

// Build creates the chain.
func (b *Builder) Build() *Chain {
	return New(b.name, b.nodes...)
}
