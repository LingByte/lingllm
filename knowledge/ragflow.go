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

// RAGFlowHandler implements KnowledgeHandler using RAGFlow.
// RAGFlow is an open-source RAG engine that provides document management,
// chunking, and vector search capabilities.
type RAGFlowHandler struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Embedder   embedder.Embedder
}

func (rh *RAGFlowHandler) Provider() string { return ProviderRAGFlow }

// Upsert adds or updates records in RAGFlow knowledge base
func (rh *RAGFlowHandler) Upsert(ctx context.Context, records []Record, opts *UpsertOptions) error {
	if rh == nil {
		return ErrHandlerNotFound
	}
	if len(records) == 0 {
		return nil
	}

	ns := "default"
	if opts != nil && opts.Namespace != "" {
		ns = opts.Namespace
	}

	// RAGFlow uses datasets as namespaces
	datasetID, err := rh.ensureDataset(ctx, ns)
	if err != nil {
		return err
	}

	// Upload documents to RAGFlow
	for _, record := range records {
		if strings.TrimSpace(record.ID) == "" {
			return fmt.Errorf("record id cannot be empty")
		}

		// Create document metadata
		docMeta := map[string]any{
			"title":   record.Title,
			"content": record.Content,
		}
		if len(record.Tags) > 0 {
			docMeta["tags"] = record.Tags
		}
		if record.Metadata != nil {
			for k, v := range record.Metadata {
				docMeta[k] = v
			}
		}

		// Upload document to RAGFlow
		reqBody := map[string]any{
			"name":     record.ID,
			"type":     "doc",
			"content":  record.Content,
			"metadata": docMeta,
		}

		reqURL := fmt.Sprintf("%s/api/v1/datasets/%s/documents", rh.BaseURL, url.PathEscape(datasetID))
		body, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

		resp, err := rh.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("ragflow upload failed: status=%d body=%s", resp.StatusCode, string(respBody))
		}
	}

	return nil
}

// Query searches documents in RAGFlow knowledge base
func (rh *RAGFlowHandler) Query(ctx context.Context, text string, opts *QueryOptions) ([]QueryResult, error) {
	if rh == nil {
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

	// Get or create dataset
	datasetID, err := rh.ensureDataset(ctx, ns)
	if err != nil {
		return nil, err
	}

	// Search in RAGFlow
	reqBody := map[string]any{
		"query": text,
		"top_k": topK,
	}

	reqURL := fmt.Sprintf("%s/api/v1/datasets/%s/search", rh.BaseURL, url.PathEscape(datasetID))
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

	resp, err := rh.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ragflow search failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var searchResp struct {
		Code int `json:"code"`
		Data struct {
			Chunks []struct {
				ID       string  `json:"id"`
				Content  string  `json:"content"`
				DocID    string  `json:"doc_id"`
				Score    float64 `json:"similarity"`
				Metadata map[string]any `json:"metadata"`
			} `json:"chunks"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	if searchResp.Code != 0 {
		return nil, fmt.Errorf("ragflow search error: code=%d", searchResp.Code)
	}

	results := make([]QueryResult, 0, len(searchResp.Data.Chunks))
	for _, chunk := range searchResp.Data.Chunks {
		if chunk.Score < minScore {
			continue
		}

		record := Record{
			ID:       chunk.DocID,
			Content:  chunk.Content,
			Metadata: chunk.Metadata,
		}

		results = append(results, QueryResult{
			Record: record,
			Score:  chunk.Score,
		})
	}

	return results, nil
}

// Get retrieves records by IDs
func (rh *RAGFlowHandler) Get(ctx context.Context, ids []string, opts *GetOptions) ([]Record, error) {
	if rh == nil {
		return nil, ErrHandlerNotFound
	}
	if len(ids) == 0 {
		return nil, nil
	}

	ns := "default"
	if opts != nil && opts.Namespace != "" {
		ns = opts.Namespace
	}

	datasetID, err := rh.ensureDataset(ctx, ns)
	if err != nil {
		return nil, err
	}

	results := make([]Record, 0, len(ids))

	for _, id := range ids {
		reqURL := fmt.Sprintf("%s/api/v1/datasets/%s/documents/%s", rh.BaseURL, url.PathEscape(datasetID), url.PathEscape(id))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

		resp, err := rh.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("ragflow get failed: status=%d body=%s", resp.StatusCode, string(respBody))
		}

		var docResp struct {
			Code int `json:"code"`
			Data struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Content  string `json:"content"`
				Metadata map[string]any `json:"metadata"`
			} `json:"data"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&docResp); err != nil {
			return nil, err
		}

		if docResp.Code == 0 {
			record := Record{
				ID:       docResp.Data.ID,
				Title:    docResp.Data.Name,
				Content:  docResp.Data.Content,
				Metadata: docResp.Data.Metadata,
			}
			results = append(results, record)
		}
	}

	return results, nil
}

// List lists all records in a namespace
func (rh *RAGFlowHandler) List(ctx context.Context, opts *ListOptions) (*ListResult, error) {
	if rh == nil {
		return nil, ErrHandlerNotFound
	}

	ns := "default"
	if opts != nil && opts.Namespace != "" {
		ns = opts.Namespace
	}

	datasetID, err := rh.ensureDataset(ctx, ns)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/api/v1/datasets/%s/documents", rh.BaseURL, url.PathEscape(datasetID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

	resp, err := rh.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ragflow list failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var listResp struct {
		Code int `json:"code"`
		Data struct {
			Documents []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Content string `json:"content"`
			} `json:"documents"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, err
	}

	if listResp.Code != 0 {
		return nil, fmt.Errorf("ragflow list error: code=%d", listResp.Code)
	}

	records := make([]Record, 0, len(listResp.Data.Documents))
	for _, doc := range listResp.Data.Documents {
		records = append(records, Record{
			ID:      doc.ID,
			Title:   doc.Name,
			Content: doc.Content,
		})
	}

	return &ListResult{
		Records: records,
	}, nil
}

// Delete removes records by IDs
func (rh *RAGFlowHandler) Delete(ctx context.Context, ids []string, opts *DeleteOptions) error {
	if rh == nil {
		return ErrHandlerNotFound
	}
	if len(ids) == 0 {
		return nil
	}

	ns := "default"
	if opts != nil && opts.Namespace != "" {
		ns = opts.Namespace
	}

	datasetID, err := rh.ensureDataset(ctx, ns)
	if err != nil {
		return err
	}

	for _, id := range ids {
		reqURL := fmt.Sprintf("%s/api/v1/datasets/%s/documents/%s", rh.BaseURL, url.PathEscape(datasetID), url.PathEscape(id))

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

		resp, err := rh.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("ragflow delete failed: status=%d body=%s", resp.StatusCode, string(respBody))
		}
	}

	return nil
}

// Ping checks the health of RAGFlow service
func (rh *RAGFlowHandler) Ping(ctx context.Context) error {
	if rh == nil {
		return ErrHandlerNotFound
	}

	reqURL := fmt.Sprintf("%s/api/v1/health", rh.BaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

	resp, err := rh.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ragflow health check failed: status=%d", resp.StatusCode)
	}

	return nil
}

// CreateNamespace creates a new dataset in RAGFlow
func (rh *RAGFlowHandler) CreateNamespace(ctx context.Context, name string) error {
	if rh == nil {
		return ErrHandlerNotFound
	}

	if strings.TrimSpace(name) == "" {
		return ErrNamespaceNotFound
	}

	reqBody := map[string]any{
		"name": name,
	}

	reqURL := fmt.Sprintf("%s/api/v1/datasets", rh.BaseURL)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

	resp, err := rh.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ragflow create dataset failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteNamespace deletes a dataset from RAGFlow
func (rh *RAGFlowHandler) DeleteNamespace(ctx context.Context, name string) error {
	if rh == nil {
		return ErrHandlerNotFound
	}

	if strings.TrimSpace(name) == "" {
		return ErrNamespaceNotFound
	}

	datasetID, err := rh.getDatasetID(ctx, name)
	if err != nil {
		return err
	}

	if datasetID == "" {
		return nil
	}

	reqURL := fmt.Sprintf("%s/api/v1/datasets/%s", rh.BaseURL, url.PathEscape(datasetID))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

	resp, err := rh.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ragflow delete dataset failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ListNamespaces lists all datasets in RAGFlow
func (rh *RAGFlowHandler) ListNamespaces(ctx context.Context) ([]string, error) {
	if rh == nil {
		return nil, ErrHandlerNotFound
	}

	reqURL := fmt.Sprintf("%s/api/v1/datasets", rh.BaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rh.APIKey))

	resp, err := rh.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ragflow list datasets failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var listResp struct {
		Code int `json:"code"`
		Data struct {
			Datasets []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"datasets"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, err
	}

	if listResp.Code != 0 {
		return nil, fmt.Errorf("ragflow list datasets error: code=%d", listResp.Code)
	}

	namespaces := make([]string, 0, len(listResp.Data.Datasets))
	for _, ds := range listResp.Data.Datasets {
		namespaces = append(namespaces, ds.Name)
	}

	return namespaces, nil
}

// Helper functions

// ensureDataset gets or creates a dataset
func (rh *RAGFlowHandler) ensureDataset(ctx context.Context, name string) (string, error) {
	// Try to get existing dataset
	datasetID, err := rh.getDatasetID(ctx, name)
	if err == nil && datasetID != "" {
		return datasetID, nil
	}

	// Create new dataset if not found
	if err := rh.CreateNamespace(ctx, name); err != nil {
		return "", err
	}

	// Get the newly created dataset ID
	return rh.getDatasetID(ctx, name)
}

// getDatasetID retrieves the ID of a dataset by name
func (rh *RAGFlowHandler) getDatasetID(ctx context.Context, name string) (string, error) {
	datasets, err := rh.ListNamespaces(ctx)
	if err != nil {
		return "", err
	}

	for _, ds := range datasets {
		if ds == name {
			// In a real implementation, we would return the actual ID
			// For now, we use the name as ID
			return name, nil
		}
	}

	return "", nil
}
