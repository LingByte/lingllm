package retrieve

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Retrieve runs the configured strategy.
func (r *StrategyRetriever) Retrieve(ctx context.Context, query string, topK int) ([]*Document, error) {
	if r == nil {
		return nil, fmt.Errorf("retrieve: nil retriever")
	}
	if topK <= 0 {
		topK = r.cfg.TopK
	}

	fetchK := topK
	if r.cfg.Reranker != nil {
		fetchK = r.cfg.RerankCandidates
		if fetchK <= 0 {
			fetchK = topK * 3
		}
		if fetchK < topK {
			fetchK = topK
		}
	}

	var docs []*Document
	var err error

	switch r.cfg.Strategy {
	case StrategyVector:
		docs, err = r.cfg.Vector.Retrieve(ctx, query, fetchK)
	case StrategyKeyword:
		docs, err = r.keywordSearch(ctx, query, fetchK)
	case StrategyHybrid:
		docs, err = r.hybridSearch(ctx, query, fetchK)
	default:
		return nil, fmt.Errorf("retrieve: unsupported strategy")
	}

	if err != nil {
		return nil, err
	}

	return applyRerank(ctx, r.cfg.Reranker, query, docs, topK, fetchK)
}

// keywordSearch performs full-text search.
func (r *StrategyRetriever) keywordSearch(ctx context.Context, query string, topK int) ([]*Document, error) {
	hits, err := r.cfg.Search.Search(ctx, query, r.cfg.KeywordFields, topK)
	if err != nil {
		return nil, err
	}
	return hitsToDocuments(hits, r.cfg.MinScore), nil
}

// hybridSearch merges vector and keyword search results.
func (r *StrategyRetriever) hybridSearch(ctx context.Context, query string, topK int) ([]*Document, error) {
	vecDocs, err := r.cfg.Vector.Retrieve(ctx, query, topK*2)
	if err != nil {
		return nil, err
	}

	kwDocs, err := r.keywordSearch(ctx, query, topK*2)
	if err != nil {
		return nil, err
	}

	wv := r.cfg.VectorWeight
	if wv > 1 {
		wv = 1
	}
	wk := 1 - wv

	type scored struct {
		doc   *Document
		score float64
	}

	merged := map[string]scored{}

	// Merge vector results
	for _, d := range vecDocs {
		if d == nil {
			continue
		}
		id := d.ID
		if id == "" {
			id = d.Content
		}
		s := merged[id]
		s.doc = d
		s.score += d.Score * wv
		merged[id] = s
	}

	// Merge keyword results
	for _, d := range kwDocs {
		if d == nil {
			continue
		}
		id := d.ID
		if id == "" {
			id = d.Content
		}
		s := merged[id]
		if s.doc == nil {
			s.doc = d
		}
		s.score += d.Score * wk
		merged[id] = s
	}

	// Rank by merged score
	var ranked []scored
	for _, s := range merged {
		if s.doc == nil || s.score < r.cfg.MinScore {
			continue
		}
		copy := *s.doc
		copy.Score = s.score
		ranked = append(ranked, scored{doc: &copy, score: s.score})
	}

	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	if len(ranked) > topK {
		ranked = ranked[:topK]
	}

	out := make([]*Document, len(ranked))
	for i, s := range ranked {
		out[i] = s.doc
	}

	return out, nil
}

// applyRerank applies optional reranking to documents.
func applyRerank(ctx context.Context, reranker Reranker, query string, docs []*Document, topK, candidates int) ([]*Document, error) {
	if reranker == nil {
		return docs, nil
	}
	if len(docs) == 0 {
		return docs, nil
	}
	if topK <= 0 {
		topK = len(docs)
	}
	if candidates <= 0 {
		candidates = topK * 3
	}
	if candidates < topK {
		candidates = topK
	}
	if len(docs) > candidates {
		docs = docs[:candidates]
	}

	texts := make([]string, len(docs))
	for i, d := range docs {
		if d == nil {
			continue
		}
		texts[i] = d.Content
	}

	results, err := reranker.Rerank(ctx, query, texts, topK)
	if err != nil {
		return nil, fmt.Errorf("retrieve: rerank: %w", err)
	}

	out := make([]*Document, 0, len(results))
	for _, r := range results {
		if r.Index < 0 || r.Index >= len(docs) {
			continue
		}
		doc := docs[r.Index]
		if doc == nil {
			continue
		}
		copy := *doc
		copy.Score = r.Score
		out = append(out, &copy)
	}

	return out, nil
}

// hitsToDocuments converts search hits to documents.
func hitsToDocuments(hits []SearchHit, minScore float64) []*Document {
	out := make([]*Document, 0, len(hits))
	for _, h := range hits {
		if h.Score < minScore {
			continue
		}

		content := fieldString(h.Fields, "content")
		if content == "" {
			content = fieldString(h.Fields, "body")
		}

		meta := map[string]string{}
		for _, k := range []string{"title", "source", "parent_id", "chunk_strategy"} {
			if v := fieldString(h.Fields, k); v != "" {
				meta[k] = v
			}
		}

		out = append(out, &Document{
			ID:       h.ID,
			Content:  content,
			Score:    h.Score,
			Metadata: meta,
		})
	}

	return out
}

// fieldString extracts string value from fields map.
func fieldString(fields map[string]interface{}, key string) string {
	if fields == nil {
		return ""
	}
	v, ok := fields[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []string:
		if len(t) > 0 {
			return t[0]
		}
	}
	return fmt.Sprint(v)
}
