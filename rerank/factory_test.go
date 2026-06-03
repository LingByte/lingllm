package rerank

import (
	"testing"
)

func TestCreate_SiliconFlow(t *testing.T) {
	cfg := &RerankConfig{
		Provider: ProviderSiliconFlow,
		BaseURL:  "https://api.siliconflow.cn",
		APIKey:   "test-key",
		Model:    "test-model",
	}

	reranker, err := Create(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if reranker == nil {
		t.Fatal("expected reranker, got nil")
	}

	if reranker.Provider() != ProviderSiliconFlow {
		t.Errorf("expected provider %s, got %s", ProviderSiliconFlow, reranker.Provider())
	}
}

func TestCreate_JinaAI(t *testing.T) {
	cfg := &RerankConfig{
		Provider: ProviderJinaAI,
		BaseURL:  "https://api.jina.ai",
		APIKey:   "test-key",
		Model:    "test-model",
	}

	reranker, err := Create(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if reranker == nil {
		t.Fatal("expected reranker, got nil")
	}

	if reranker.Provider() != ProviderJinaAI {
		t.Errorf("expected provider %s, got %s", ProviderJinaAI, reranker.Provider())
	}
}

func TestCreate_CohereAI(t *testing.T) {
	cfg := &RerankConfig{
		Provider: ProviderCohereAI,
		BaseURL:  "https://api.cohere.ai",
		APIKey:   "test-key",
		Model:    "test-model",
	}

	reranker, err := Create(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if reranker == nil {
		t.Fatal("expected reranker, got nil")
	}

	if reranker.Provider() != ProviderCohereAI {
		t.Errorf("expected provider %s, got %s", ProviderCohereAI, reranker.Provider())
	}
}

func TestCreate_Local(t *testing.T) {
	cfg := &RerankConfig{
		Provider: ProviderLocal,
	}

	reranker, err := Create(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if reranker == nil {
		t.Fatal("expected reranker, got nil")
	}

	if reranker.Provider() != ProviderLocal {
		t.Errorf("expected provider %s, got %s", ProviderLocal, reranker.Provider())
	}
}

func TestCreate_MissingProvider(t *testing.T) {
	cfg := &RerankConfig{}

	_, err := Create(cfg)
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestCreate_UnsupportedProvider(t *testing.T) {
	cfg := &RerankConfig{
		Provider: "unsupported",
	}

	_, err := Create(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestCreate_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *RerankConfig
		hasErr bool
	}{
		{
			name: "SiliconFlow missing BaseURL",
			cfg: &RerankConfig{
				Provider: ProviderSiliconFlow,
				APIKey:   "test",
				Model:    "test",
			},
			hasErr: true,
		},
		{
			name: "SiliconFlow missing APIKey",
			cfg: &RerankConfig{
				Provider: ProviderSiliconFlow,
				BaseURL:  "https://api.test.com",
				Model:    "test",
			},
			hasErr: true,
		},
		{
			name: "SiliconFlow missing Model",
			cfg: &RerankConfig{
				Provider: ProviderSiliconFlow,
				BaseURL:  "https://api.test.com",
				APIKey:   "test",
			},
			hasErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Create(tt.cfg)
			if (err != nil) != tt.hasErr {
				t.Errorf("expected error=%v, got %v", tt.hasErr, err)
			}
		})
	}
}
