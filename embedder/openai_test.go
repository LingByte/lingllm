package embedder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

func TestNewOpenAIEmbedder(t *testing.T) {
	cfg := &Config{
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test",
		Model:     "text-embedding-3-small",
		Dimension: 1536,
		Timeout:   30,
	}

	embedder := NewOpenAIEmbedder(cfg)

	assert.NotNil(t, embedder)
	assert.Equal(t, "openai", embedder.Name())
	assert.Equal(t, "openai", embedder.Provider())
	assert.Equal(t, 1536, embedder.Dimension())
}

func TestNewOpenAIEmbedder_DefaultValues(t *testing.T) {
	cfg := &Config{
		APIKey: "sk-test",
		Model:  "text-embedding-3-small",
	}

	embedder := NewOpenAIEmbedder(cfg)

	assert.Equal(t, "https://api.openai.com/v1", embedder.baseURL)
	assert.Equal(t, 1536, embedder.Dimension())
}

func TestOpenAIEmbedder_Close(t *testing.T) {
	embedder := NewOpenAIEmbedder(&Config{
		APIKey: "sk-test",
		Model:  "text-embedding-3-small",
	})

	err := embedder.Close()
	assert.Nil(t, err)
}

func TestOpenAIEmbedder_EmbedSingle_Empty(t *testing.T) {
	embedder := NewOpenAIEmbedder(&Config{
		APIKey: "sk-test",
		Model:  "text-embedding-3-small",
	})

	ctx := context.Background()
	_, err := embedder.EmbedSingle(ctx, "")

	assert.Equal(t, ErrEmptyInput, err)
}

func TestOpenAIEmbedder_Embed_Empty(t *testing.T) {
	embedder := NewOpenAIEmbedder(&Config{
		APIKey: "sk-test",
		Model:  "text-embedding-3-small",
	})

	ctx := context.Background()
	_, err := embedder.Embed(ctx, []string{})

	assert.Equal(t, ErrEmptyInput, err)
}

func TestOpenAIEmbedder_BaseURLTrimming(t *testing.T) {
	cfg := &Config{
		BaseURL: "https://api.openai.com/v1/",
		APIKey:  "sk-test",
		Model:   "text-embedding-3-small",
	}

	embedder := NewOpenAIEmbedder(cfg)
	assert.Equal(t, "https://api.openai.com/v1", embedder.baseURL)
}

func TestOpenAIEmbedder_DefaultTimeout(t *testing.T) {
	cfg := &Config{
		APIKey: "sk-test",
		Model:  "text-embedding-3-small",
	}

	embedder := NewOpenAIEmbedder(cfg)
	assert.NotNil(t, embedder.httpClient)
}
