# Search Demo

Comprehensive demonstration of LingLLM's full-text search engine powered by Bleve.

## Features

### 6 Demo Scenarios

1. **Index Documents**
   - Batch indexing
   - Document structure
   - Field mapping

2. **Basic Search**
   - Keyword search
   - Multiple queries
   - Score ranking

3. **Advanced Search**
   - Must/must-not terms
   - Match clauses
   - Complex queries

4. **Faceted Search**
   - Category aggregation
   - Term counting
   - Facet results

5. **Search Suggestions**
   - Autocomplete
   - Search suggestions
   - Prefix matching

6. **Highlighting**
   - Query highlighting
   - Fragment extraction
   - HTML formatting

## Usage

### Basic Usage

```bash
go run examples/search-demo/main.go
```

### Custom Index Path

```bash
go run examples/search-demo/main.go -index /path/to/index
```

## Demo Output

### Demo 1: Index Documents
```
✓ Indexed 5 documents
  - 1: Introduction to Go Programming
  - 2: Python for Data Science
  - 3: Web Development with Go
  - 4: Machine Learning Basics
  - 5: Rust Systems Programming
```

### Demo 2: Basic Search
```
Query: "Go"
Results: 2 hits (took 5ms)
  1. [0.95] Introduction to Go Programming
  2. [0.87] Web Development with Go
```

### Demo 3: Advanced Search
```
Search: Programming articles with high views
Results: 3 hits
  1. [0.92] Introduction to Go Programming (Category: programming)
  2. [0.88] Web Development with Go (Category: programming)
  3. [0.85] Rust Systems Programming (Category: programming)
```

### Demo 4: Faceted Search
```
Query: "programming"
Results: 4 hits

Categories:
  - programming: 3
  - data-science: 1
```

### Demo 5: Search Suggestions
```
Autocomplete for "Go":
  1. Introduction to Go Programming
  2. Web Development with Go

Autocomplete for "Py":
  1. Python for Data Science
```

### Demo 6: Highlighting
```
Query: "programming" (with highlighting)
Results: 3 hits

1. Introduction to Go Programming
   Highlights:
   - title:
     Introduction to Go <em>Programming</em>
```

## Document Structure

```go
type Doc struct {
    ID     string                 // Document ID
    Type   string                 // Document type (article, blog, etc.)
    Fields map[string]interface{} // Document fields
}
```

### Example Document

```go
Doc{
    ID:   "1",
    Type: "article",
    Fields: map[string]interface{}{
        "title":    "Introduction to Go Programming",
        "body":     "Go is a statically typed, compiled programming language...",
        "tags":     "golang,programming,tutorial",
        "author":   "Alice",
        "category": "programming",
        "views":    1500,
    },
}
```

## Search Request

```go
type SearchRequest struct {
    // Basic search
    Keyword      string
    SearchFields []string
    
    // Structured queries
    MustTerms    map[string][]string
    MustNotTerms map[string][]string
    ShouldTerms  map[string][]string
    
    // Advanced clauses
    Matches     []ClauseMatch
    Phrases     []ClausePhrase
    Prefixes    []ClausePrefix
    Wildcards   []ClauseWildcard
    Regexps     []ClauseRegex
    Fuzzies     []ClauseFuzzy
    
    // Facets
    Facets []FacetRequest
    
    // Pagination & sorting
    From   int
    Size   int
    SortBy []string
    
    // Highlighting
    Highlight       bool
    HighlightFields []string
}
```

## Search Examples

### Simple Keyword Search

```go
result, _ := engine.Search(ctx, search.SearchRequest{
    Keyword: "golang",
    Size:    10,
})
```

### Structured Query

```go
result, _ := engine.Search(ctx, search.SearchRequest{
    MustTerms: map[string][]string{
        "category": {"programming"},
    },
    ShouldTerms: map[string][]string{
        "tags": {"golang", "python"},
    },
    Size: 10,
})
```

### Advanced Search with Matches

```go
result, _ := engine.Search(ctx, search.SearchRequest{
    Matches: []search.ClauseMatch{
        {
            Field:    "title",
            Query:    "programming",
            Operator: "and",
        },
    },
    Phrases: []search.ClausePhrase{
        {
            Field:  "body",
            Phrase: "machine learning",
        },
    },
    Size: 10,
})
```

### Faceted Search

```go
result, _ := engine.Search(ctx, search.SearchRequest{
    Keyword: "programming",
    Facets: []search.FacetRequest{
        {
            Name:  "categories",
            Field: "category",
            Size:  10,
        },
    },
    Size: 10,
})
```

### Pagination

```go
// Get first page
result1, _ := engine.Search(ctx, search.SearchRequest{
    Keyword: "golang",
    From:    0,
    Size:    10,
})

// Get second page
result2, _ := engine.Search(ctx, search.SearchRequest{
    Keyword: "golang",
    From:    10,
    Size:    10,
})
```

### Sorting

```go
result, _ := engine.Search(ctx, search.SearchRequest{
    Keyword: "golang",
    SortBy:  []string{"-views", "title"},
    Size:    10,
})
```

### Highlighting

```go
result, _ := engine.Search(ctx, search.SearchRequest{
    Keyword:         "golang",
    Highlight:       true,
    HighlightFields: []string{"title", "body"},
    Size:            10,
})
```

## Search Results

```go
type SearchResult struct {
    Total  uint64                 // Total hits
    Took   time.Duration          // Query time
    Hits   []Hit                  // Result hits
    Facets map[string]FacetResult // Facet results
}

type Hit struct {
    ID        string              // Document ID
    Score     float64             // Relevance score
    Fields    map[string]any      // Document fields
    Fragments map[string][]string // Highlighted fragments
}
```

## Performance Tips

1. **Batch Indexing**: Use `IndexBatch` for better performance
2. **Field Selection**: Use `IncludeFields` to limit returned fields
3. **Pagination**: Use `From` and `Size` for large result sets
4. **Facets**: Limit facet size to reduce computation
5. **Highlighting**: Only highlight necessary fields

## Index Configuration

### Default Analyzer

- `standard`: Standard analyzer (default)
- `keyword`: Keyword analyzer (exact matching)

### Field Types

- **Text**: Full-text searchable, analyzed
- **Keyword**: Exact matching, not analyzed
- **Numeric**: Numeric range queries
- **DateTime**: Date range queries

## Advanced Features

### Query String Syntax

```go
search.SearchRequest{
    QueryString: &search.ClauseQueryString{
        Query: "golang AND (web OR backend)",
    },
}
```

### Fuzzy Matching

```go
search.SearchRequest{
    Fuzzies: []search.ClauseFuzzy{
        {
            Field:     "title",
            Term:      "programing", // typo
            Fuzziness: 1,
        },
    },
}
```

### Wildcard Queries

```go
search.SearchRequest{
    Wildcards: []search.ClauseWildcard{
        {
            Field:   "title",
            Pattern: "go*",
        },
    },
}
```

### Regular Expressions

```go
search.SearchRequest{
    Regexps: []search.ClauseRegex{
        {
            Field:   "title",
            Pattern: "go[a-z]*",
        },
    },
}
```

## Cleanup

The demo automatically cleans up the index directory after completion.

To manually clean up:

```bash
rm -rf /tmp/search_demo_index
```

## Next Steps

1. Integrate search into your application
2. Customize field mappings for your data
3. Implement search UI with suggestions
4. Add analytics and logging
5. Optimize index size and performance
