# Knowledge Base Module

Comprehensive knowledge base management system that integrates embeddings, full-text search, vector databases, and document retrieval.

## Features

### 🎯 Core Capabilities

- **Multi-Backend Support**: Qdrant and Milvus vector databases
- **Intelligent Chunking**: Automatic document type detection and optimal chunking strategies
- **Semantic Search**: Vector-based similarity search with embeddings
- **Full-Text Search**: Keyword-based search with advanced query support
- **Hybrid Retrieval**: Combine vector and keyword search for better results
- **Document Management**: Add, query, delete, and list documents
- **Metadata Support**: Flexible metadata storage and filtering

### 📊 Document Type Detection

Automatically detects and handles different document types:

- **Structured**: Manuals, papers, contracts, reports, markdown
- **Table/KV**: Resumes, forms, questionnaires, financial documents
- **Unstructured**: OCR text, novels, garbled webpages, chat logs

### 🔧 Vector Database Backends

#### Qdrant
- REST API based
- High-performance vector search
- Configurable timeout and API key
- Namespace support for multi-tenancy

#### Milvus
- Distributed vector database
- Scalable to billions of vectors
- Support for complex filtering
- Multiple connection options

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   KnowledgeBase                         │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │  Embedder    │  │  Search      │  │  Retriever   │ │
│  │  (OpenAI,    │  │  Engine      │  │  (Vector,    │ │
│  │   Ollama,    │  │  (Bleve)     │  │   Keyword,   │ │
│  │   Nvidia)    │  │              │  │   Hybrid)    │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
│         │                 │                  │         │
│         └─────────────────┼──────────────────┘         │
│                           │                            │
│  ┌────────────────────────┴─────────────────────────┐  │
│  │        KnowledgeHandler (Qdrant/Milvus)         │  │
│  └────────────────────────┬─────────────────────────┘  │
│                           │                            │
│  ┌────────────────────────┴─────────────────────────┐  │
│  │      Vector Database (Qdrant or Milvus)         │  │
│  └────────────────────────────────────────────────┘  │
│                                                       │
└─────────────────────────────────────────────────────────┘
```

## Usage

### Basic Setup

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
		APIKey:   "sk-...",
	})

	// Create search engine
	searchCfg := search.Config{
		IndexPath:           "./search_index",
		DefaultAnalyzer:     "standard",
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

	// Add documents
	kb.AddDocument(context.Background(), "doc1", "Title", "Content...", nil)

	// Query
	results, _ := kb.Query(context.Background(), "search query", 10)
	for _, result := range results {
		println(result.Record.Title, result.Score)
	}
}
```

### Add Document

```go
err := kb.AddDocument(ctx, "doc-id", "Document Title", "Document content...", map[string]any{
	"author": "John Doe",
	"year":   2024,
	"tags":   []string{"golang", "knowledge-base"},
})
```

### Query Documents

```go
results, err := kb.Query(ctx, "machine learning", 10)
for _, result := range results {
	fmt.Printf("%s (score: %.2f)\n", result.Record.Title, result.Score)
}
```

### Delete Document

```go
err := kb.DeleteDocument(ctx, "doc-id")
```

### Health Check

```go
err := kb.Health(ctx)
```

## Configuration

### Qdrant Configuration

```go
handler, err := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
	Provider: knowledge.ProviderQdrant,
	QdrantConfig: &knowledge.QdrantConfig{
		BaseURL: "http://localhost:6333",
		APIKey:  "your-api-key",
		Timeout: 30 * time.Second,
	},
})
```

### Milvus Configuration

```go
handler, err := knowledge.NewKnowledgeHandler(knowledge.HandlerFactoryParams{
	Provider: knowledge.ProviderMilvus,
	MilvusConfig: &knowledge.MilvusConfig{
		Address:  "localhost:19530",
		Username: "root",
		Password: "Milvus",
		DBName:   "default",
	},
})
```

## Data Types

### Record

```go
type Record struct {
	ID        string         // Unique identifier
	Source    string         // Document source ID
	Title     string         // Chunk title
	Content   string         // Chunk text
	Vector    []float32      // Embedding vector
	Tags      []string       // Tags for filtering
	Metadata  map[string]any // Custom metadata
	CreatedAt time.Time      // Creation timestamp
	UpdatedAt time.Time      // Update timestamp
}
```

### Chunk

```go
type Chunk struct {
	Index    int            // Chunk index
	Title    string         // Chunk title
	Text     string         // Chunk text
	Metadata map[string]any // Chunk metadata
}
```

### QueryResult

```go
type QueryResult struct {
	Record Record  // Retrieved record
	Score  float64 // Relevance score
}
```

## Document Type Detection

The module automatically detects document types:

```go
detector := &knowledge.RuleBasedDocumentTypeDetector{}
docType, err := detector.DetectDocumentType(ctx, text)

switch docType {
case knowledge.DocumentTypeStructured:
	// Handle structured documents (manuals, papers)
case knowledge.DocumentTypeTableKV:
	// Handle table/KV documents (forms, resumes)
case knowledge.DocumentTypeUnstructured:
	// Handle unstructured documents (OCR, novels)
}
```

## Chunking Strategies

Different chunking strategies for different document types:

```go
chunkers := map[knowledge.DocumentType]knowledge.Chunker{
	knowledge.DocumentTypeStructured: structuredChunker,
	knowledge.DocumentTypeTableKV:    kvChunker,
	knowledge.DocumentTypeUnstructured: unstructuredChunker,
}

kb, _ := knowledge.NewKnowledgeBase(knowledge.KnowledgeBaseConfig{
	Handler:  handler,
	Chunkers: chunkers,
})
```

## Filtering

### Query with Filters

```go
results, err := kb.handler.Query(ctx, "query", &knowledge.QueryOptions{
	TopK: 10,
	Filters: []knowledge.Filter{
		{
			Field:    "author",
			Operator: knowledge.FilterOpEqual,
			Value:    []any{"John Doe"},
		},
	},
})
```

### Filter Operators

- `$eq`: Equal
- `$ne`: Not equal
- `$in`: In list
- `$nin`: Not in list
- `$gt`: Greater than
- `$gte`: Greater than or equal
- `$lt`: Less than
- `$lte`: Less than or equal
- `$all`: Contains all
- `$any`: Contains any

## Performance Considerations

### Batch Operations

For better performance with large documents:

```go
// Chunk document
chunks, _ := chunker.Chunk(ctx, largeContent, opts)

// Batch embed
vectors, _ := embedder.Embed(ctx, texts)

// Batch upsert
handler.Upsert(ctx, records, &knowledge.UpsertOptions{
	BatchSize: 100,
})
```

### Indexing

- Use appropriate vector dimensions (256-1536)
- Enable metadata filtering for better performance
- Create namespaces for multi-tenancy

### Search

- Use full-text search for keyword queries
- Use vector search for semantic queries
- Combine both for hybrid search

## Error Handling

```go
err := kb.AddDocument(ctx, "doc1", "Title", "Content", nil)
if err != nil {
	switch err {
	case knowledge.ErrEmptyText:
		// Handle empty content
	case knowledge.ErrNoChunks:
		// Handle chunking failure
	case knowledge.ErrEmbedderNotFound:
		// Handle missing embedder
	default:
		// Handle other errors
	}
}
```

## Testing

Run tests:

```bash
go test ./knowledge -v
```

Run with coverage:

```bash
go test ./knowledge -cover
```

## Interfaces

### KnowledgeHandler

```go
type KnowledgeHandler interface {
	Provider() string
	Upsert(ctx context.Context, records []Record, opts *UpsertOptions) error
	Query(ctx context.Context, text string, opts *QueryOptions) ([]QueryResult, error)
	Get(ctx context.Context, ids []string, opts *GetOptions) ([]Record, error)
	List(ctx context.Context, opts *ListOptions) (*ListResult, error)
	Delete(ctx context.Context, ids []string, opts *DeleteOptions) error
	Ping(ctx context.Context) error
	CreateNamespace(ctx context.Context, name string) error
	DeleteNamespace(ctx context.Context, name string) error
	ListNamespaces(ctx context.Context) ([]string, error)
}
```

### Chunker

```go
type Chunker interface {
	Provider() string
	Chunk(ctx context.Context, text string, opts *ChunkOptions) ([]Chunk, error)
}
```

### DocumentTypeDetector

```go
type DocumentTypeDetector interface {
	DetectDocumentType(ctx context.Context, text string) (DocumentType, error)
}
```

## Roadmap

- [ ] Support for more vector databases (Pinecone, Weaviate)
- [ ] Advanced chunking strategies (semantic, hierarchical)
- [ ] Caching layer for embeddings
- [ ] Batch processing optimization
- [ ] Multi-language support
- [ ] Vector dimension auto-tuning
- [ ] Distributed knowledge base support

## License

AGPL-3.0 - See LICENSE file for details
