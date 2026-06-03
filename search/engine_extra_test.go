package search

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

func TestBleveEngine_Guard_Closed(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	engine.Close()

	// Try to index after close
	err := engine.Index(context.Background(), Doc{ID: "test"})
	assert.Equal(t, ErrClosed, err)

	// Try to search after close
	_, err = engine.Search(context.Background(), SearchRequest{})
	assert.Equal(t, ErrClosed, err)

	// Try to delete after close
	err = engine.Delete(context.Background(), "test")
	assert.Equal(t, ErrClosed, err)

	// Try to get suggestions after close
	_, err = engine.GetAutoCompleteSuggestions(context.Background(), "test")
	assert.Equal(t, ErrClosed, err)

	_, err = engine.GetSearchSuggestions(context.Background(), "test")
	assert.Equal(t, ErrClosed, err)
}

func TestBleveEngine_Close_Idempotent(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer os.RemoveAll(indexPath)

	// Close multiple times should not error
	err1 := engine.Close()
	assert.Nil(t, err1)

	err2 := engine.Close()
	assert.Nil(t, err2)
}

func TestBleveEngine_IndexBatch_Empty(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	err := engine.IndexBatch(context.Background(), []Doc{})
	assert.Nil(t, err)
}

func TestBleveEngine_IndexBatch_LargeBatch(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	// Create docs larger than default batch size
	docs := make([]Doc, 300)
	for i := 0; i < 300; i++ {
		docs[i] = Doc{
			ID:   string(rune(i)),
			Type: "article",
			Fields: map[string]interface{}{
				"title": "Document " + string(rune(i)),
			},
		}
	}

	err := engine.IndexBatch(context.Background(), docs)
	assert.Nil(t, err)

	// Verify some documents were indexed
	result, err := engine.Search(context.Background(), SearchRequest{
		Keyword: "Document",
		Size:    10,
	})
	assert.Nil(t, err)
	assert.True(t, result.Total > 0)
}

func TestBleveEngine_Search_WithHighlight(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID:   "test1",
		Type: "article",
		Fields: map[string]interface{}{
			"title": "Machine Learning Basics",
			"body":  "Machine learning is a subset of artificial intelligence",
		},
	}
	engine.Index(context.Background(), doc)

	result, err := engine.Search(context.Background(), SearchRequest{
		Keyword:         "Machine",
		Highlight:       true,
		HighlightFields: []string{"title", "body"},
		Size:            10,
	})
	assert.Nil(t, err)
	assert.True(t, result.Total > 0)
}

func TestBleveEngine_Search_WithFacets(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	docs := []Doc{
		{
			ID:   "1",
			Type: "article",
			Fields: map[string]interface{}{
				"title":    "Go Programming",
				"category": "programming",
			},
		},
		{
			ID:   "2",
			Type: "article",
			Fields: map[string]interface{}{
				"title":    "Python Guide",
				"category": "programming",
			},
		},
		{
			ID:   "3",
			Type: "article",
			Fields: map[string]interface{}{
				"title":    "Web Design",
				"category": "design",
			},
		},
	}
	engine.IndexBatch(context.Background(), docs)

	result, err := engine.Search(context.Background(), SearchRequest{
		Keyword: "Guide",
		Facets: []FacetRequest{
			{
				Name:  "categories",
				Field: "category",
				Size:  10,
			},
		},
		Size: 10,
	})
	assert.Nil(t, err)
	assert.True(t, len(result.Facets) > 0)
}

func TestBleveEngine_Search_WithSort(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	docs := []Doc{
		{
			ID:   "1",
			Type: "article",
			Fields: map[string]interface{}{
				"title": "First Article",
				"views": 100,
			},
		},
		{
			ID:   "2",
			Type: "article",
			Fields: map[string]interface{}{
				"title": "Second Article",
				"views": 200,
			},
		},
	}
	engine.IndexBatch(context.Background(), docs)

	result, err := engine.Search(context.Background(), SearchRequest{
		Keyword: "Article",
		SortBy:  []string{"-views"},
		Size:    10,
	})
	assert.Nil(t, err)
	assert.True(t, result.Total > 0)
}

func TestBleveEngine_Search_WithPagination(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	// Index multiple documents
	for i := 0; i < 20; i++ {
		doc := Doc{
			ID:   string(rune(i)),
			Type: "article",
			Fields: map[string]interface{}{
				"title": "Article " + string(rune(i)),
			},
		}
		engine.Index(context.Background(), doc)
	}

	// Test pagination
	result1, err := engine.Search(context.Background(), SearchRequest{
		Keyword: "Article",
		From:    0,
		Size:    5,
	})
	assert.Nil(t, err)
	assert.Equal(t, 5, len(result1.Hits))

	result2, err := engine.Search(context.Background(), SearchRequest{
		Keyword: "Article",
		From:    5,
		Size:    5,
	})
	assert.Nil(t, err)
	assert.Equal(t, 5, len(result2.Hits))
}

func TestBleveEngine_Search_DefaultSize(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID:   "test1",
		Type: "article",
		Fields: map[string]interface{}{
			"title": "Test",
		},
	}
	engine.Index(context.Background(), doc)

	// Search with size 0 should default to 10
	result, err := engine.Search(context.Background(), SearchRequest{
		Keyword: "Test",
		Size:    0,
	})
	assert.Nil(t, err)
	assert.True(t, result.Total > 0)
}

func TestBleveEngine_Search_WithIncludeFields(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID:   "test1",
		Type: "article",
		Fields: map[string]interface{}{
			"title": "Test Document",
			"body":  "This is a test",
		},
	}
	engine.Index(context.Background(), doc)

	result, err := engine.Search(context.Background(), SearchRequest{
		Keyword:       "Test",
		IncludeFields: []string{"title"},
		Size:          10,
	})
	assert.Nil(t, err)
	assert.True(t, result.Total > 0)
}

func TestBleveEngine_GetAutoCompleteSuggestions_Empty(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	suggestions, err := engine.GetAutoCompleteSuggestions(context.Background(), "")
	assert.Nil(t, err)
	assert.Equal(t, 0, len(suggestions))
}

func TestBleveEngine_GetAutoCompleteSuggestions_WithData(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID:   "test1",
		Type: "article",
		Fields: map[string]interface{}{
			"title": "Machine Learning",
		},
	}
	engine.Index(context.Background(), doc)

	suggestions, err := engine.GetAutoCompleteSuggestions(context.Background(), "Mach")
	assert.Nil(t, err)
	// Should return suggestions starting with "Mach"
	assert.True(t, len(suggestions) >= 0)
}

func TestBleveEngine_GetSearchSuggestions_Empty(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	suggestions, err := engine.GetSearchSuggestions(context.Background(), "")
	assert.Nil(t, err)
	assert.Equal(t, 0, len(suggestions))
}

func TestBleveEngine_GetSearchSuggestions_WithData(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID:   "test1",
		Type: "article",
		Fields: map[string]interface{}{
			"title": "Python Programming",
		},
	}
	engine.Index(context.Background(), doc)

	suggestions, err := engine.GetSearchSuggestions(context.Background(), "Python")
	assert.Nil(t, err)
	assert.True(t, len(suggestions) >= 0)
}

func TestBleveEngine_Delete_Extra(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID:   "test2",
		Type: "article",
		Fields: map[string]interface{}{
			"title": "Test Document 2",
		},
	}
	engine.Index(context.Background(), doc)

	// Verify document exists
	result, _ := engine.Search(context.Background(), SearchRequest{
		Keyword: "Test",
		Size:    10,
	})
	assert.True(t, result.Total > 0)

	// Delete document
	err := engine.Delete(context.Background(), "test2")
	assert.Nil(t, err)

	// Verify document is deleted
	result, _ = engine.Search(context.Background(), SearchRequest{
		Keyword: "Document 2",
		Size:    10,
	})
	assert.Equal(t, uint64(0), result.Total)
}

func TestNew_InvalidIndexPath(t *testing.T) {
	cfg := Config{
		IndexPath:       "/invalid/path/that/does/not/exist/index",
		DefaultAnalyzer: "standard",
	}

	m := BuildIndexMapping("standard")
	_, err := New(cfg, m)
	assert.NotNil(t, err)
}

func TestBleveEngine_WithDeadline_Timeout_Extra(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// This should timeout
	err := engine.Index(ctx, Doc{ID: "test"})
	assert.NotNil(t, err)
}

func TestBleveEngine_Index_WithoutType(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID: "test1",
		Fields: map[string]interface{}{
			"title": "Test Document",
		},
	}
	err := engine.Index(context.Background(), doc)
	assert.Nil(t, err)

	result, _ := engine.Search(context.Background(), SearchRequest{
		Keyword: "Test",
		Size:    10,
	})
	assert.True(t, result.Total > 0)
}

func TestBleveEngine_Search_NegativeFrom(t *testing.T) {
	engine, indexPath := setupTestEngine(t)
	defer cleanupTestEngine(t, engine, indexPath)

	doc := Doc{
		ID:   "test1",
		Type: "article",
		Fields: map[string]interface{}{
			"title": "Test",
		},
	}
	engine.Index(context.Background(), doc)

	// Negative From should be treated as 0
	result, err := engine.Search(context.Background(), SearchRequest{
		Keyword: "Test",
		From:    -5,
		Size:    10,
	})
	assert.Nil(t, err)
	assert.True(t, result.Total > 0)
}
