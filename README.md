# LingLLM

A Go framework for building LLM applications. It combines a provider-agnostic LLM core
and a full RAG stack behind unified, strongly-typed interfaces.

Version: 1.4.5 · Go 1.26

## Features

**LLM core**
- Multi-provider chat behind one interface — OpenAI, Anthropic, Ollama, Azure OpenAI,
  Gemini, DashScope, Cohere, plus OpenAI-compatible presets (xAI, Mistral, Volcengine Ark,
  Hunyuan, ERNIE, vLLM, LM Studio, LocalAI)
- Tool / function calling with automatic multi-round execution
- Streaming responses with event-based processing
- Composable chain pipeline for multi-step workflows
- Conversation memory and prompt management
- Built-in metrics for latency, token usage, and error rates

**Retrieval (RAG)**
- Multi-provider embeddings — OpenAI, Ollama, Nvidia, DashScope, Local
- Bleve-powered full-text search with facets, highlighting, suggestions
- Multi-strategy retrieval — vector, keyword, hybrid — with reranking
- Document chunking with multiple strategies
- Knowledge base over Qdrant and Milvus
- Document parsing (PDF, Office, OCR via gosseract)

## Installation

```bash
go get github.com/LingByte/lingllm
```

## Project Structure

```
lingllm/
├── protocol/        # Core LLM types and provider clients
│   ├── types.go     # ChatRequest, ChatResponse, Message, Tool, ChatStream
│   ├── factory.go   # Provider factory
│   ├── stream.go    # Streaming utilities and transformers
│   ├── openai/      # OpenAI + OpenAI-compatible client
│   ├── anthropic/   # Anthropic client
│   ├── ollama/      # Ollama client
│   ├── azure/       # Azure OpenAI client
│   ├── gemini/      # Google Gemini client
│   ├── dashscope/   # Alibaba DashScope native client
│   ├── cohere/      # Cohere Chat v2 client
│   └── compat/      # OpenAI-compatible provider presets
├── chain/           # Chain-based processing pipeline
├── tools/           # Tool definitions, executors, and tool chains
├── prompt/          # Prompt templates and management
├── memory/          # Conversation memory (single and layered)
├── metrics/         # Call metrics and monitoring
│
├── embedder/        # Text embedding providers (OpenAI, Ollama, Nvidia, DashScope, Local)
├── search/          # Bleve full-text search engine
├── retrieve/        # Multi-strategy retrieval (vector, keyword, hybrid)
├── rerank/          # Document reranking
├── chunk/           # Document chunking strategies
├── knowledge/       # Knowledge base over Qdrant / Milvus
├── parser/          # Document parsing (PDF, Office, OCR)
├── cache/           # Caching layer
│
├── utils/           # Shared text utilities
├── shared/          # Shared helpers
├── examples/        # Runnable demos for each module
└── version/         # Build version info
```

## LLM Core

### Basic Chat

```go
package main

import (
	"context"
	"fmt"

	"github.com/LingByte/lingllm/protocol"
)

func main() {
	req := protocol.NewChatRequest(
		"gpt-4",
		protocol.UserMessage("What is the capital of France?"),
	)

	// Call your provider implementation:
	// resp, err := model.Chat(context.Background(), *req)
	// fmt.Println(resp.FirstContent())
	_ = req
	_ = context.Background
	_ = fmt.Println
}
```

### Providers

Blank-import the provider package to register it, then create a client via the factory:

```go
import (
	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/gemini"
	_ "github.com/LingByte/lingllm/protocol/compat" // xai, mistral, volcengine, hunyuan, ernie, vllm, ...
)

client, err := protocol.NewClient(protocol.ClientConfig{
	Provider: protocol.ProviderGemini,
	APIKey:   os.Getenv("GEMINI_API_KEY"),
})

// Azure OpenAI
azure, err := protocol.NewClient(protocol.ClientConfig{
	Provider:   protocol.ProviderAzure,
	APIKey:     os.Getenv("AZURE_OPENAI_API_KEY"),
	BaseURL:    "https://YOUR_RESOURCE.openai.azure.com",
	APIVersion: "2024-10-21",
	Deployment: "gpt-4o",
})

// DashScope native (enable thinking via metadata)
ds, err := protocol.NewClient(protocol.ClientConfig{
	Provider: protocol.ProviderDashScope,
	APIKey:   os.Getenv("DASHSCOPE_API_KEY"),
})
req := protocol.NewChatRequest("qwen-plus", protocol.UserMessage("hi")).
	WithMetadata("enable_thinking", "true")
```

OpenAI-compatible presets use default BaseURLs (override with `BaseURL`):
`xai`, `mistral`, `volcengine`, `hunyuan`, `ernie`, `vllm`, `lmstudio`, `localai`.

Build requests fluently:

```go
req := protocol.NewChatRequest("gpt-4",
	protocol.SystemMessage("You are a helpful assistant"),
	protocol.UserMessage("Hello"),
).
	WithMaxTokens(1000).
	WithTemperature(0.7).
	WithTopP(0.9).
	WithStop("END")
```

### Tool Calling

```go
executor := tools.NewSimpleToolExecutor()

weatherTool := tools.WeatherTool()
executor.RegisterTool(weatherTool, func(args json.RawMessage) (string, error) {
	return "Sunny, 72°F", nil
})

toolChain := tools.NewToolChain(model, executor)
toolChain.WithMaxRounds(5)

req := protocol.NewChatRequest("gpt-4",
	protocol.UserMessage("What's the weather in San Francisco?"))

resp, err := toolChain.ExecuteWithTools(context.Background(), *req)
if err != nil {
	panic(err)
}
fmt.Println(resp.FirstContent())
```

### Streaming

```go
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
```

### Chains

```go
c := chain.NewBuilder("my-chain").
	AddModel("model1", model1).
	AddProcessor("processor1", func(ctx context.Context, resp *protocol.ChatResponse) (*protocol.ChatResponse, error) {
		return resp, nil
	}).
	AddModel("model2", model2).
	Build()

resp, err := c.Invoke(context.Background(), *protocol.NewChatRequest("gpt-4", protocol.UserMessage("Hello")))
if err != nil {
	panic(err)
}
println(resp.FirstContent())
```

## Retrieval (RAG)

### Embeddings

```go
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

vec, _ := emb.EmbedSingle(context.Background(), "Hello world")
vecs, _ := emb.Embed(context.Background(), []string{"Hello world", "Goodbye world"})
fmt.Printf("dim=%d, batch=%d\n", len(vec), len(vecs))
```

### Full-Text Search

```go
cfg := search.Config{
	IndexPath:           "./search_index",
	DefaultAnalyzer:     "standard",
	DefaultSearchFields: []string{"title", "body"},
}
engine, err := search.New(cfg, search.BuildIndexMapping("standard"))
if err != nil {
	panic(err)
}
defer engine.Close()

engine.IndexBatch(context.Background(), []search.Doc{{
	ID:   "1",
	Type: "article",
	Fields: map[string]interface{}{
		"title": "Go Programming",
		"body":  "Go is a fast and efficient language",
	},
}})

result, _ := engine.Search(context.Background(), search.SearchRequest{Keyword: "Go", Size: 10})
fmt.Printf("Found %d results\n", result.Total)
```

### Hybrid Retrieval

```go
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

docs, _ := retriever.Retrieve(context.Background(), "machine learning", 10)
for i, doc := range docs {
	fmt.Printf("%d. %s (score: %.2f)\n", i+1, doc.Content, doc.Score)
}
```

### Knowledge Base

```go
emb, _ := embedder.Create(context.Background(), &embedder.Config{
	Provider: "openai",
	Model:    "text-embedding-3-small",
	APIKey:   os.Getenv("OPENAI_API_KEY"),
})

searcher, _ := search.New(search.Config{
	IndexPath:           "./search_index",
	DefaultSearchFields: []string{"title", "content"},
}, search.BuildIndexMapping("standard"))

handler, _ := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
	Provider: knowledge.ProviderQdrant,
	QdrantConfig: &knowledge.QdrantConfig{
		BaseURL: "http://localhost:6333",
		APIKey:  "your-api-key",
	},
})

kb, _ := knowledge.NewKnowledgeBase(knowledge.KnowledgeBaseConfig{
	Handler:  handler,
	Embedder: emb,
	Searcher: searcher,
})
defer kb.Close()

kb.AddDocument(context.Background(), "doc1", "Title", "Content...", nil)

results, _ := kb.Query(context.Background(), "search query", 10)
for _, r := range results {
	fmt.Printf("%s (score: %.2f)\n", r.Record.Title, r.Score)
}
```

## Examples

Runnable demos live under [`examples/`](examples/):

| Demo | Covers |
| --- | --- |
| `anthropic-demo`, `openai-demo`, `ollama-demo` | Provider chat clients |
| `tools-demo` | Tool / function calling |
| `chain-demo` | Chain pipelines |
| `prompt-demo` | Prompt templates |
| `memory-demo`, `memory-layers-demo` | Conversation memory |
| `embedder-demo` | Multi-provider embeddings |
| `search-demo` | Full-text search |
| `chunk-demo` | Document chunking |
| `knowledge-demo`, `qdrant-demo` | Knowledge base |
| `response-demo`, `batch-processing-demo` | Response handling / batching |

## Core Interfaces

| Interface | Package | Purpose |
| --- | --- | --- |
| `ChatModel` | `protocol` | Language model abstraction |
| `ChatStream` | `protocol` | Streaming responses |
| `Tool` / `ToolExecutor` | `tools` | Tool definitions and execution |
| `Chain` / `Node` | `chain` | Composable processing pipeline |
| `Embedder` | `embedder` | Text embedding |

## Testing

```bash
go test ./...           # run all tests
go test -cover ./...    # with coverage
```

The `embedder`, `search`, `retrieve`, and `knowledge` modules carry high coverage
(80%+, search at 96%+).

## Contributing

Contributions are welcome:

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Submit a pull request

## License

GNU Affero General Public License v3.0 (AGPL-3.0) — see the [LICENSE](LICENSE) file for details.
