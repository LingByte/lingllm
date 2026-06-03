package rerank

import (
	"fmt"
	"strings"
)

// Create creates a new reranker based on the provider
func Create(cfg *RerankConfig) (Reranker, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return nil, fmt.Errorf("provider is required")
	}

	clientCfg := &RerankClientConfig{
		BaseURL:    cfg.BaseURL,
		APIKey:     cfg.APIKey,
		Model:      cfg.Model,
		HTTPClient: cfg.HTTPClient,
	}

	if cfg.Timeout > 0 {
		clientCfg.Timeout = 0
	}

	switch provider {
	case ProviderSiliconFlow:
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("BaseURL is required for SiliconFlow")
		}
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("APIKey is required for SiliconFlow")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("Model is required for SiliconFlow")
		}
		return NewSiliconFlowRerankClient(clientCfg), nil

	case ProviderJinaAI:
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("BaseURL is required for Jina AI")
		}
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("APIKey is required for Jina AI")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("Model is required for Jina AI")
		}
		return NewJinaAIRerankClient(clientCfg), nil

	case ProviderCohereAI:
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("BaseURL is required for Cohere AI")
		}
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("APIKey is required for Cohere AI")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("Model is required for Cohere AI")
		}
		return NewCohereAIRerankClient(clientCfg), nil

	case ProviderLocal:
		return NewLocalRerankClient(clientCfg), nil

	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}
