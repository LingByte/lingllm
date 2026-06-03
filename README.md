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
- **Embeddings**: Multi-provider text embedding (OpenAI, Ollama, Nvidia, DashScope, Local)
- **Full-Text Search**: Bleve-powered search with facets, highlighting, and suggestions
- **Document Retrieval**: Multi-strategy retrieval (vector, keyword, hybrid) with reranking
- **Chunking**: Intelligent document chunking with multiple strategies
- **Knowledge Base**: Integrated knowledge management with vector databases (Qdrant, Milvus)

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

### Embeddings

```go
package main

import (
	"context"
	"github.com/LingByte/lingllm/embedder"
)

func main() {
	// Create embedder with OpenAI
	cfg := &embedder.Config{
		Provider: "openai",
		Model:    "text-embedding-3-small",
		APIKey:   os.Getenv("OPENAI_API_KEY"),
	}
	
	emb, err := embedder.Create(context.Background(), cfg)
	if err != nil {
		panic(err)
	}
	defer emb.Close()

	// Single embedding
	vec, err := emb.EmbedSingle(context.Background(), "Hello world")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Vector dimension: %d\n", len(vec))

	// Batch embedding
	vecs, err := emb.Embed(context.Background(), []string{
		"Hello world",
		"Goodbye world",
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Embedded %d texts\n", len(vecs))
}
```

### Full-Text Search

```go
package main

import (
	"context"
	"github.com/LingByte/lingllm/search"
)

func main() {
	// Create search engine
	cfg := search.Config{
		IndexPath:           "./search_index",
		DefaultAnalyzer:     "standard",
		DefaultSearchFields: []string{"title", "body"},
	}
	
	m := search.BuildIndexMapping("standard")
	engine, err := search.New(cfg, m)
	if err != nil {
		panic(err)
	}
	defer engine.Close()

	// Index documents
	docs := []search.Doc{
		{
			ID:   "1",
			Type: "article",
			Fields: map[string]interface{}{
				"title": "Go Programming",
				"body":  "Go is a fast and efficient language",
			},
		},
	}
	engine.IndexBatch(context.Background(), docs)

	// Search
	result, err := engine.Search(context.Background(), search.SearchRequest{
		Keyword: "Go",
		Size:    10,
	})
	if err != nil {
		panic(err)
	}
	
	fmt.Printf("Found %d results\n", result.Total)
	for _, hit := range result.Hits {
		fmt.Printf("- %s (score: %.2f)\n", hit.Fields["title"], hit.Score)
	}
}
```

### Document Retrieval

```go
package main

import (
	"context"
	"github.com/LingByte/lingllm/retrieve"
)

func main() {
	// Create hybrid retriever
	retriever, err := retrieve.New(retrieve.Config{
		Strategy:     retrieve.StrategyHybrid,
		Vector:       vectorStore,
		Search:       searchEngine,
		TopK:         10,
		VectorWeight: 0.65,
	})
	if err != nil {
		panic(err)
	}

	// Retrieve documents
	docs, err := retriever.Retrieve(context.Background(), "machine learning", 10)
	if err != nil {
		panic(err)
	}
	
	for i, doc := range docs {
		fmt.Printf("%d. %s (score: %.2f)\n", i+1, doc.Content, doc.Score)
	}
}
```

### Knowledge Base

```go
package main

import (
	"context"
	"github.com/LingByte/lingllm/knowledge"
	"github.com/LingByte/lingllm/embedder"
	"github.com/LingByte/lingllm/search"
)

func main() {
	// Create embedder
	emb, _ := embedder.Create(context.Background(), &embedder.Config{
		Provider: "openai",
		Model:    "text-embedding-3-small",
		APIKey:   os.Getenv("OPENAI_API_KEY"),
	})

	// Create search engine
	searchCfg := search.Config{
		IndexPath:           "./search_index",
		DefaultSearchFields: []string{"title", "content"},
	}
	m := search.BuildIndexMapping("standard")
	searcher, _ := search.New(searchCfg, m)

	// Create vector database handler
	handler, _ := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
		Provider: knowledge.ProviderQdrant,
		QdrantConfig: &knowledge.QdrantConfig{
			BaseURL: "http://localhost:6333",
			APIKey:  "your-api-key",
		},
	})

	// Create knowledge base
	kb, _ := knowledge.NewKnowledgeBase(knowledge.KnowledgeBaseConfig{
		Handler:  handler,
		Embedder: emb,
		Searcher: searcher,
	})
	defer kb.Close()

	// Add document
	kb.AddDocument(context.Background(), "doc1", "Title", "Content...", nil)

	// Query
	results, _ := kb.Query(context.Background(), "search query", 10)
	for _, result := range results {
		fmt.Printf("%s (score: %.2f)\n", result.Record.Title, result.Score)
	}
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
├── embedder/          # Text embedding providers
│   ├── types.go       # Core types and interfaces
│   ├── local.go       # Local MD5-based embedder
│   ├── openai.go      # OpenAI embeddings
│   ├── ollama.go      # Ollama local embeddings
│   ├── nvidia.go      # Nvidia enterprise embeddings
│   ├── dashscope.go   # Alibaba DashScope embeddings
│   ├── factory.go     # Embedder factory pattern
│   └── *_test.go      # Comprehensive tests (80%+ coverage)
├── search/            # Full-text search engine
│   ├── types.go       # Search types and interfaces
│   ├── engine.go      # Bleve-based search engine
│   ├── mapping.go     # Index mapping configuration
│   ├── query_builder.go # Query construction
│   └── *_test.go      # Tests (96.1% coverage)
├── retrieve/          # Multi-strategy document retrieval
│   ├── types.go       # Retrieval types
│   ├── config.go      # Configuration and factory
│   ├── retriever.go   # Retrieval logic
│   └── *_test.go      # Tests (80.6% coverage)
├── chunk/             # Document chunking strategies
│   └── ...
├── knowledge/         # Knowledge base management
│   ├── types.go       # Core types and interfaces
│   ├── integration.go # KnowledgeBase integration
│   ├── qdrant.go      # Qdrant vector database handler
│   ├── milvus.go      # Milvus vector database handler
│   ├── embedding.go   # Embedding client (Nvidia)
│   ├── doc_type_detector.go # Document type detection
│   ├── README.md      # Knowledge base documentation
│   └── *_test.go      # Tests (40+ tests)
├── utils/             # Shared utilities
│   ├── clean.go       # Text cleaning utilities
│   └── *_test.go      # Tests
├── shared/            # Shared utilities
├── examples/          # Example implementations
│   ├── embedder-demo/ # Multi-provider embedding demo
│   ├── search-demo/   # Full-text search demo
│   └── ...
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

### Embeddings

The `embedder` package provides multi-provider text embeddings:

- **OpenAI**: High-quality embeddings (1536 dimensions)
- **Ollama**: Local deployment support (384 dimensions)
- **Nvidia**: Enterprise-grade embeddings (1024+ dimensions)
- **DashScope**: Alibaba's native API (64-2048 dimensions)
- **Local**: MD5-based deterministic embeddings (384 dimensions)

Features:
- Unified interface across providers
- Batch processing support
- Configurable dimensions
- Vector normalization
- Factory pattern for easy provider switching

### Search

The `search` package provides full-text search powered by Bleve:

- **Full-Text Search**: Keyword and phrase matching
- **Advanced Queries**: Match, phrase, prefix, wildcard, regex, fuzzy
- **Faceted Search**: Category aggregation and counting
- **Highlighting**: Query term highlighting with HTML formatting
- **Suggestions**: Autocomplete and search suggestions
- **Pagination**: Offset-based result pagination
- **Sorting**: Multi-field sorting support

### Retrieval

The `retrieve` package implements multi-strategy document retrieval:

- **Vector Strategy**: Dense vector similarity search
- **Keyword Strategy**: Full-text search
- **Hybrid Strategy**: Combines vector and keyword with configurable weights
- **Reranking**: Optional document re-scoring
- **Min Score Filtering**: Quality control for results

### Knowledge Base

The `knowledge` package provides integrated knowledge management:

- **Multi-Backend Support**: Qdrant and Milvus vector databases
- **Intelligent Chunking**: Automatic document type detection and optimal chunking
- **Semantic Search**: Vector-based similarity with embeddings
- **Full-Text Search**: Keyword-based search integration
- **Hybrid Retrieval**: Combine vector and keyword search
- **Document Management**: Add, query, delete, and list documents
- **Metadata Support**: Flexible metadata storage and filtering
- **Document Type Detection**: Structured, Table/KV, Unstructured

Features:
- Automatic document chunking based on type
- Embedding generation for semantic search
- Integration with search engines
- Multi-tenancy with namespaces
- Health checks and monitoring

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

## Modules Status

### ✅ Completed

- **Embedder Module** (80%+ coverage)
  - 5 providers: OpenAI, Ollama, Nvidia, DashScope, Local
  - Batch processing, vector normalization
  - Comprehensive tests and demo

- **Search Module** (96.1% coverage)
  - Bleve-powered full-text search
  - Advanced query types
  - Facets, highlighting, suggestions
  - 60+ tests

- **Retrieve Module** (80.6% coverage)
  - 3 strategies: Vector, Keyword, Hybrid
  - Reranking support
  - 26 tests

- **Knowledge Base Module** (40+ tests)
  - Multi-backend support (Qdrant, Milvus)
  - Intelligent document chunking
  - Semantic and keyword search
  - Document type detection
  - Metadata support
  - Health checks

- **Chunk Module**
  - Multiple chunking strategies
  - Configurable chunk sizes
  - Overlap support

### 📋 Roadmap

- [ ] Official OpenAI provider implementation
- [ ] Official Anthropic provider implementation
- [ ] MCP (Model Context Protocol) integration
- [ ] Caching layer for responses and embeddings
- [ ] Advanced prompt engineering utilities
- [ ] Evaluation framework
- [ ] More tool examples and templates
- [ ] Vector database integration (Qdrant, Milvus)
- [ ] Knowledge base management utilities
- [ ] RAG (Retrieval-Augmented Generation) pipeline

## Support

For issues, questions, or suggestions, please open an issue on GitHub.
