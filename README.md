# LingLLM

A universal Go library for building LLM applications with support for multiple providers, tools, chains, and streaming.

## Features

- **Multi-Provider Support**: Unified interface for OpenAI, Anthropic, and other LLM providers
- **Tool/Function Calling**: Built-in support for tool execution and management
- **Streaming**: Full support for streaming responses with event-based processing
- **Chain Architecture**: Composable chain-based processing pipeline for complex workflows
- **Tool Chains**: Automatic tool calling and result collection with configurable rounds
- **Metrics**: Built-in metrics collection for monitoring API calls and performance
- **Type-Safe**: Strongly typed Go interfaces and implementations

## Installation

```bash
go get github.com/LingByte/lingllm
```

## Quick Start

### Basic Chat

```go
package main

import (
	"context"
	"fmt"
	"github.com/LingByte/lingllm/protocol"
)

func main() {
	// Create a chat request
	req := protocol.NewChatRequest(
		"gpt-4",
		protocol.UserMessage("What is the capital of France?"),
	)

	// Call your model implementation
	// resp, err := model.Chat(context.Background(), *req)
	// if err != nil {
	//     panic(err)
	// }
	// fmt.Println(resp.FirstContent())
}
```

### Tool Calling

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/LingByte/lingllm/protocol"
	"github.com/LingByte/lingllm/tools"
)

func main() {
	// Create a tool executor
	executor := tools.NewSimpleToolExecutor()

	// Register a tool
	weatherTool := tools.WeatherTool()
	executor.RegisterTool(weatherTool, func(args json.RawMessage) (string, error) {
		// Implement weather lookup
		return "Sunny, 72°F", nil
	})

	// Create a tool chain
	toolChain := tools.NewToolChain(model, executor)
	toolChain.WithMaxRounds(5)

	// Execute with tools
	req := protocol.NewChatRequest(
		"gpt-4",
		protocol.UserMessage("What's the weather in San Francisco?"),
	)
	resp, err := toolChain.ExecuteWithTools(context.Background(), *req)
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.FirstContent())
}
```

### Streaming

```go
package main

import (
	"context"
	"fmt"
	"io"
	"github.com/LingByte/lingllm/protocol"
)

func main() {
	req := protocol.NewChatRequest(
		"gpt-4",
		protocol.UserMessage("Write a poem about Go programming"),
	)

	stream, err := model.StreamChat(context.Background(), *req)
	if err != nil {
		panic(err)
	}
	defer stream.Close()

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		fmt.Print(chunk.Delta)
	}
}
```

### Chains

```go
package main

import (
	"context"
	"github.com/LingByte/lingllm/chain"
	"github.com/LingByte/lingllm/protocol"
)

func main() {
	// Build a chain with multiple models/processors
	c := chain.NewBuilder("my-chain").
		AddModel("model1", model1).
		AddProcessor("processor1", func(ctx context.Context, resp *protocol.ChatResponse) (*protocol.ChatResponse, error) {
			// Process response
			return resp, nil
		}).
		AddModel("model2", model2).
		Build()

	req := protocol.NewChatRequest(
		"gpt-4",
		protocol.UserMessage("Hello"),
	)

	resp, err := c.Invoke(context.Background(), *req)
	if err != nil {
		panic(err)
	}
	println(resp.FirstContent())
}
```

## Project Structure

```
lingllm/
├── protocol/          # Core protocol definitions and interfaces
│   ├── types.go       # ChatRequest, ChatResponse, Message, Tool definitions
│   ├── factory.go     # Provider factory for creating clients
│   ├── stream.go      # Streaming utilities and transformers
│   └── ...
├── tools/             # Tool execution and management
│   ├── tools.go       # Tool definitions, executors, and chains
│   └── ...
├── chain/             # Chain-based processing pipeline
│   ├── chain.go       # Chain composition and execution
│   └── ...
├── metrics/           # Metrics collection
│   └── metrics.go     # Call metrics and monitoring
├── shared/            # Shared utilities
├── examples/          # Example implementations
└── go.mod
```

## Core Concepts

### Protocol

The `protocol` package defines the core interfaces and types:

- **ChatModel**: Interface for language models
- **ChatRequest**: Unified request format
- **ChatResponse**: Normalized response format
- **ChatStream**: Streaming interface
- **Tool**: Tool/function definitions

### Tools

The `tools` package provides:

- **ToolExecutor**: Interface for executing tools
- **SimpleToolExecutor**: Basic implementation using function maps
- **ToolChain**: Automatic tool calling with conversation management
- **ToolCallParser**: Parse tool calls from model responses (ReAct, JSON formats)

### Chains

The `chain` package enables:

- **Chain**: Sequential composition of nodes
- **Node**: Composable units (models, processors, stream processors)
- **Builder**: Fluent API for building chains
- **ProcessingStream**: Stream transformation pipeline

### Metrics

The `metrics` package tracks:

- API call latency
- Token usage
- Error rates
- Provider-specific metrics

## Supported Providers

The library provides a unified interface for:

- OpenAI (GPT-4, GPT-3.5, etc.)
- Anthropic (Claude)
- Local models (via compatible APIs)
- Custom implementations

## Message Types

```go
// Create messages
msg := protocol.UserMessage("Hello")
msg := protocol.SystemMessage("You are a helpful assistant")
msg := protocol.AssistantMessage("Hi there!")
msg := protocol.ToolMessage("Result", "tool_call_id")

// Build requests
req := protocol.NewChatRequest("gpt-4", msg1, msg2, msg3)
req.WithMaxTokens(1000).
    WithTemperature(0.7).
    WithTopP(0.9).
    WithStop("END")
```

## Error Handling

```go
resp, err := model.Chat(ctx, req)
if err != nil {
	// Handle error
	fmt.Printf("Error: %v\n", err)
}
```

## Configuration

Configure clients using provider-specific implementations:

```go
// Example with environment variables
apiKey := os.Getenv("OPENAI_API_KEY")
// Create your provider-specific client
```

## Testing

Run tests:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -cover ./...
```

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Submit a pull request

## License

MIT License - see LICENSE file for details

## Roadmap

- [ ] Official OpenAI provider implementation
- [ ] Official Anthropic provider implementation
- [ ] MCP (Model Context Protocol) integration
- [ ] Caching layer for responses
- [ ] Advanced prompt engineering utilities
- [ ] Evaluation framework
- [ ] More tool examples and templates

## Support

For issues, questions, or suggestions, please open an issue on GitHub.
