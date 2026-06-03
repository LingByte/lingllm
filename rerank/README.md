# Rerank Module

Document reranking module for improving RAG recall rate by reordering search results based on relevance.

## Features

- **Multiple Providers**: Support for SiliconFlow, Jina AI, Cohere AI, and local reranking
- **Flexible Response Parsing**: Handles different API response formats
- **Configurable**: Support for custom HTTP clients and timeouts
- **Local Fallback**: Built-in local reranker for offline use
- **Easy Integration**: Simple factory pattern for creating rerankers

## Supported Providers

### 1. SiliconFlow

High-performance reranking service with flexible API.

```go
reranker, err := rerank.Create(&rerank.RerankConfig{
    Provider: rerank.ProviderSiliconFlow,
    BaseURL:  "https://api.siliconflow.cn",
    APIKey:   "your-api-key",
    Model:    "bge-reranker-v2-m3",
})
```

**Features**:
- Fast reranking with multiple model options
- Supports both `results` and `data` response formats
- Flexible score field names (`score`, `relevance_score`)

### 2. Jina AI

Jina AI's reranking service with document content support.

```go
reranker, err := rerank.Create(&rerank.RerankConfig{
    Provider: rerank.ProviderJinaAI,
    BaseURL:  "https://api.jina.ai",
    APIKey:   "your-api-key",
    Model:    "jina-reranker-v1-base-en",
})
```

**Features**:
- Optimized for semantic understanding
- Supports document objects with content field
- High-quality reranking results

### 3. Cohere AI

Cohere's enterprise reranking service.

```go
reranker, err := rerank.Create(&rerank.RerankConfig{
    Provider: rerank.ProviderCohereAI,
    BaseURL:  "https://api.cohere.ai",
    APIKey:   "your-api-key",
    Model:    "rerank-english-v2.0",
})
```

**Features**:
- Enterprise-grade reranking
- Supports multiple languages
- Customizable ranking parameters

### 4. Local Reranker

Built-in local reranker using string similarity.

```go
reranker, err := rerank.Create(&rerank.RerankConfig{
    Provider: rerank.ProviderLocal,
})
```

**Features**:
- No API calls required
- Offline operation
- Fast for small document sets
- Uses word overlap and substring matching

## Usage

### Basic Usage

```go
import "github.com/LingByte/lingllm/rerank"

// Create a reranker
reranker, err := rerank.Create(&rerank.RerankConfig{
    Provider: rerank.ProviderSiliconFlow,
    BaseURL:  "https://api.siliconflow.cn",
    APIKey:   "your-api-key",
    Model:    "bge-reranker-v2-m3",
})
if err != nil {
    log.Fatal(err)
}

// Rerank documents
query := "what is machine learning"
documents := []string{
    "Machine learning is a subset of artificial intelligence",
    "Deep learning uses neural networks",
    "Natural language processing with transformers",
}

results, err := reranker.Rerank(context.Background(), query, documents, 2)
if err != nil {
    log.Fatal(err)
}

// Print results
for _, result := range results {
    fmt.Printf("Document %d: Score %.4f\n", result.Index, result.Score)
    fmt.Printf("Content: %s\n", documents[result.Index])
}
```

### Integration with Knowledge Base

```go
// Get initial search results
searchResults, err := kb.Query(ctx, query, 10)

// Extract documents
documents := make([]string, len(searchResults))
for i, result := range searchResults {
    documents[i] = result.Record.Content
}

// Rerank results
rerankResults, err := reranker.Rerank(ctx, query, documents, 5)

// Build final results
finalResults := make([]QueryResult, len(rerankResults))
for i, rr := range rerankResults {
    finalResults[i] = searchResults[rr.Index]
}
```

## Configuration

### RerankConfig

```go
type RerankConfig struct {
    Provider   string         // Provider name (required)
    BaseURL    string         // API endpoint (required for API providers)
    APIKey     string         // API key (required for API providers)
    Model      string         // Model name (required)
    Timeout    int            // Request timeout in seconds (optional)
    HTTPClient *http.Client   // Custom HTTP client (optional)
}
```

### RerankClientConfig

```go
type RerankClientConfig struct {
    BaseURL    string
    APIKey     string
    Model      string
    Timeout    time.Duration
    HTTPClient *http.Client
}
```

## Performance Benchmarks

| Provider | Latency | Cost | Quality |
|----------|---------|------|---------|
| SiliconFlow | 100-200ms | Low | High |
| Jina AI | 150-300ms | Medium | Very High |
| Cohere AI | 200-400ms | Medium | Very High |
| Local | 1-10ms | Free | Medium |

## Error Handling

```go
results, err := reranker.Rerank(ctx, query, documents, topN)
if err != nil {
    switch err.Error() {
    case "query is empty":
        // Handle empty query
    case "documents is empty":
        // Handle empty documents
    case "BaseURL is required":
        // Handle missing configuration
    default:
        // Handle other errors
    }
}
```

## Best Practices

### 1. Use Appropriate Provider

- **High Quality**: Jina AI, Cohere AI (for production)
- **Fast & Cheap**: SiliconFlow (for high volume)
- **Offline**: Local (for development/testing)

### 2. Optimize Top-N

```go
// Don't rerank too many documents
// Typical: rerank top 10-20 from initial search
results, err := reranker.Rerank(ctx, query, documents[:20], 5)
```

### 3. Cache Rerank Results

```go
// Cache rerank results to avoid repeated calls
cacheKey := fmt.Sprintf("%s:%v", query, documents)
if cached, ok := cache.Get(cacheKey); ok {
    return cached
}

results, err := reranker.Rerank(ctx, query, documents, topN)
cache.Set(cacheKey, results)
return results
```

### 4. Handle Failures Gracefully

```go
// Fallback to original order if reranking fails
results, err := reranker.Rerank(ctx, query, documents, topN)
if err != nil {
    log.Warn("Reranking failed, using original order", err)
    // Return original results
    return originalResults
}
```

## Testing

Run tests with:

```bash
go test ./rerank -v
```

Test coverage includes:
- Factory function tests
- Provider-specific tests
- Error handling tests
- Local reranker algorithm tests

## API Response Formats

### SiliconFlow

```json
{
  "results": [
    {
      "index": 0,
      "score": 0.95,
      "relevance_score": 0.95
    }
  ]
}
```

### Jina AI

```json
{
  "results": [
    {
      "index": 0,
      "relevance_score": 0.95
    }
  ]
}
```

### Cohere AI

```json
{
  "results": [
    {
      "index": 0,
      "relevance_score": 0.95
    }
  ]
}
```

## Related Modules

- `knowledge/` - Knowledge base with vector search
- `embedder/` - Text embedding module
- `search/` - Full-text search module

## License

AGPL-3.0
