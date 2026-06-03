package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/LingByte/lingllm/embedder"
)

// Copyright (c) 2026 LingByte
// SPDX-License-Identifier: MIT

// AliyunHandler implements KnowledgeHandler using Alibaba Bailian.
// Alibaba Bailian is a cloud-based RAG service with document management,
// indexing, and semantic search capabilities.
//
// Note: Alibaba Bailian does not have a namespace concept. Instead, it uses:
// - Workspace: Top-level organization unit
// - Index: Knowledge base unit (equivalent to namespace in other systems)
// Each Index is an independent knowledge base within a Workspace.
type AliyunHandler struct {
	AccessKeyID     string
	AccessKeySecret string
	Endpoint        string
	WorkspaceID     string
	CategoryID      string
	HTTPClient      *http.Client
	Embedder        embedder.Embedder
}

func (ah *AliyunHandler) Provider() string { return ProviderAliyun }

// Upsert adds or updates records in Alibaba Bailian knowledge base
// Note: In Alibaba Bailian, Namespace parameter maps to Index ID (knowledge base ID)
func (ah *AliyunHandler) Upsert(ctx context.Context, records []Record, opts *UpsertOptions) error {
	if ah == nil {
		return ErrHandlerNotFound
	}
	if len(records) == 0 {
		return nil
	}

	ns := "default"
	if opts != nil && opts.Namespace != "" {
		ns = opts.Namespace
	}

	// In Alibaba Bailian, each Index is an independent knowledge base
	indexID, err := ah.ensureIndex(ctx, ns)
	if err != nil {
		return err
	}

	// Upload documents to Alibaba Bailian
	for _, record := range records {
		if strings.TrimSpace(record.ID) == "" {
			return fmt.Errorf("record id cannot be empty")
		}

		// Create document content
		docContent := record.Content
		if record.Title != "" {
			docContent = fmt.Sprintf("Title: %s\n\n%s", record.Title, record.Content)
		}

		// Upload document via Alibaba Bailian API
		err := ah.uploadDocument(ctx, indexID, record.ID, docContent, record.Metadata)
		if err != nil {
			return err
		}
	}

	return nil
}

// Query searches documents in Alibaba Bailian knowledge base
// Note: In Alibaba Bailian, Namespace parameter maps to Index ID (knowledge base ID)
func (ah *AliyunHandler) Query(ctx context.Context, text string, opts *QueryOptions) ([]QueryResult, error) {
	if ah == nil {
		return nil, ErrHandlerNotFound
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyQuery
	}

	topK := 10
	minScore := 0.0
	ns := "default"

	if opts != nil {
		if opts.TopK > 0 {
			topK = opts.TopK
		}
		if opts.MinScore > 0 {
			minScore = opts.MinScore
		}
		if opts.Namespace != "" {
			ns = opts.Namespace
		}
	}

	// Get or create Index (knowledge base)
	indexID, err := ah.ensureIndex(ctx, ns)
	if err != nil {
		return nil, err
	}

	// Search in Alibaba Bailian
	results, err := ah.searchDocuments(ctx, indexID, text, topK, minScore)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// Get retrieves records by IDs
func (ah *AliyunHandler) Get(ctx context.Context, ids []string, opts *GetOptions) ([]Record, error) {
	if ah == nil {
		return nil, ErrHandlerNotFound
	}
	if len(ids) == 0 {
		return nil, nil
	}

	// Alibaba Bailian doesn't support direct document retrieval by ID
	// Return empty results
	return []Record{}, nil
}

// List lists all records in a namespace
func (ah *AliyunHandler) List(ctx context.Context, opts *ListOptions) (*ListResult, error) {
	if ah == nil {
		return nil, ErrHandlerNotFound
	}

	// Alibaba Bailian doesn't support listing documents
	// Return empty results
	return &ListResult{
		Records: []Record{},
	}, nil
}

// Delete removes records by IDs
func (ah *AliyunHandler) Delete(ctx context.Context, ids []string, opts *DeleteOptions) error {
	if ah == nil {
		return ErrHandlerNotFound
	}
	if len(ids) == 0 {
		return nil
	}

	// Alibaba Bailian doesn't support deleting individual documents
	// Would need to delete the entire index
	return fmt.Errorf("alibaba bailian does not support deleting individual documents")
}

// Ping checks the health of Alibaba Bailian service
// We verify connectivity by attempting to list indexes in the workspace
func (ah *AliyunHandler) Ping(ctx context.Context) error {
	if ah == nil {
		return ErrHandlerNotFound
	}

	// Verify connectivity by listing indexes
	// This is a simple operation that doesn't require authentication beyond workspace access
	_, err := ah.ListNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("alibaba bailian health check failed: %w", err)
	}

	return nil
}

// CreateNamespace creates a new Index (knowledge base) in Alibaba Bailian
// Note: In Alibaba Bailian, namespace maps to Index, which is the knowledge base unit
func (ah *AliyunHandler) CreateNamespace(ctx context.Context, name string) error {
	if ah == nil {
		return ErrHandlerNotFound
	}

	if strings.TrimSpace(name) == "" {
		return ErrNamespaceNotFound
	}

	// Create Index via Alibaba Bailian API
	reqBody := map[string]any{
		"name":             name,
		"structure_type":   "dsl_v2",
		"source_type":      "document",
		"sink_type":        "opensearch",
	}

	reqURL := fmt.Sprintf("%s/openapi/v1/workspaces/%s/indexes", ah.Endpoint, url.PathEscape(ah.WorkspaceID))
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	ah.setAuthHeaders(req)

	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("alibaba bailian create index failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteNamespace deletes an Index (knowledge base) from Alibaba Bailian
// Note: In Alibaba Bailian, namespace maps to Index, which is the knowledge base unit
func (ah *AliyunHandler) DeleteNamespace(ctx context.Context, name string) error {
	if ah == nil {
		return ErrHandlerNotFound
	}

	if strings.TrimSpace(name) == "" {
		return ErrNamespaceNotFound
	}

	indexID, err := ah.getIndexID(ctx, name)
	if err != nil {
		return err
	}

	if indexID == "" {
		return nil
	}

	reqURL := fmt.Sprintf("%s/openapi/v1/workspaces/%s/indexes/%s", ah.Endpoint, url.PathEscape(ah.WorkspaceID), url.PathEscape(indexID))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}

	ah.setAuthHeaders(req)

	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("alibaba bailian delete index failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ListNamespaces lists all Indexes (knowledge bases) in Alibaba Bailian
// Note: In Alibaba Bailian, each Index is an independent knowledge base
func (ah *AliyunHandler) ListNamespaces(ctx context.Context) ([]string, error) {
	if ah == nil {
		return nil, ErrHandlerNotFound
	}

	reqURL := fmt.Sprintf("%s/openapi/v1/workspaces/%s/indexes", ah.Endpoint, url.PathEscape(ah.WorkspaceID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	ah.setAuthHeaders(req)

	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alibaba bailian list indexes failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var listResp struct {
		Data struct {
			Indexes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"indexes"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, err
	}

	namespaces := make([]string, 0, len(listResp.Data.Indexes))
	for _, idx := range listResp.Data.Indexes {
		namespaces = append(namespaces, idx.Name)
	}

	return namespaces, nil
}

// Helper functions

// ensureIndex gets or creates an index
func (ah *AliyunHandler) ensureIndex(ctx context.Context, name string) (string, error) {
	// Try to get existing index
	indexID, err := ah.getIndexID(ctx, name)
	if err == nil && indexID != "" {
		return indexID, nil
	}

	// Create new index if not found
	if err := ah.CreateNamespace(ctx, name); err != nil {
		return "", err
	}

	// Get the newly created index ID
	return ah.getIndexID(ctx, name)
}

// getIndexID retrieves the ID of an index by name
func (ah *AliyunHandler) getIndexID(ctx context.Context, name string) (string, error) {
	indexes, err := ah.ListNamespaces(ctx)
	if err != nil {
		return "", err
	}

	for _, idx := range indexes {
		if idx == name {
			return name, nil
		}
	}

	return "", nil
}

// uploadDocument uploads a document to Alibaba Bailian
func (ah *AliyunHandler) uploadDocument(ctx context.Context, indexID, docID, content string, metadata map[string]any) error {
	reqBody := map[string]any{
		"document_id": docID,
		"title":       docID,
		"content":     content,
	}

	if metadata != nil {
		reqBody["metadata"] = metadata
	}

	reqURL := fmt.Sprintf("%s/openapi/v1/workspaces/%s/indexes/%s/documents", ah.Endpoint, url.PathEscape(ah.WorkspaceID), url.PathEscape(indexID))
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	ah.setAuthHeaders(req)

	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("alibaba bailian upload document failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

// searchDocuments searches documents in Alibaba Bailian
func (ah *AliyunHandler) searchDocuments(ctx context.Context, indexID, query string, topK int, minScore float64) ([]QueryResult, error) {
	reqBody := map[string]any{
		"query": query,
		"top_k": topK,
	}

	reqURL := fmt.Sprintf("%s/openapi/v1/workspaces/%s/indexes/%s/search", ah.Endpoint, url.PathEscape(ah.WorkspaceID), url.PathEscape(indexID))
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	ah.setAuthHeaders(req)

	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alibaba bailian search failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var searchResp struct {
		Data struct {
			Results []struct {
				DocumentID string             `json:"document_id"`
				Content    string             `json:"content"`
				Score      float64            `json:"score"`
				Metadata   map[string]any     `json:"metadata"`
			} `json:"results"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	results := make([]QueryResult, 0, len(searchResp.Data.Results))
	for _, item := range searchResp.Data.Results {
		if item.Score < minScore {
			continue
		}

		record := Record{
			ID:       item.DocumentID,
			Content:  item.Content,
			Metadata: item.Metadata,
		}

		results = append(results, QueryResult{
			Record: record,
			Score:  item.Score,
		})
	}

	return results, nil
}

// setAuthHeaders sets authentication headers for Alibaba Bailian API
// Alibaba Bailian uses Alibaba Cloud SDK for authentication
// The SDK handles signature generation automatically
func (ah *AliyunHandler) setAuthHeaders(req *http.Request) {
	// Note: In a production environment, you should use the official Alibaba Cloud SDK
	// which handles authentication, signature generation, and request signing automatically.
	// This is a simplified implementation for demonstration purposes.
	//
	// The official SDK would be:
	// import bailian "github.com/alibabacloud-go/bailian-20231229/v2/client"
	//
	// For now, we set basic headers that the API expects
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LingLLM-Knowledge-Handler/1.0")
}
