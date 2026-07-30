package protocol

import "fmt"

// ProviderType defines supported LLM providers
type ProviderType string

const (
	ProviderOpenAI         ProviderType = "openai"
	ProviderAnthropic      ProviderType = "anthropic"
	ProviderOllama         ProviderType = "ollama"
	ProviderOpenAIResponse ProviderType = "openai-response"

	// Native protocol clients.
	ProviderAzure     ProviderType = "azure"
	ProviderGemini    ProviderType = "gemini"
	ProviderDashScope ProviderType = "dashscope"
	ProviderCohere    ProviderType = "cohere"

	// OpenAI-compatible presets (reuse protocol/openai with default BaseURL).
	ProviderXAI      ProviderType = "xai"
	ProviderMistral  ProviderType = "mistral"
	ProviderVolcArk  ProviderType = "volcengine"
	ProviderHunyuan  ProviderType = "hunyuan"
	ProviderErnie    ProviderType = "ernie"
	ProviderVLLM     ProviderType = "vllm"
	ProviderLMStudio ProviderType = "lmstudio"
	ProviderLocalAI  ProviderType = "localai"
)

// ClientConfig holds configuration for creating LLM clients
type ClientConfig struct {
	Provider     ProviderType
	APIKey       string
	BaseURL      string
	Organization string // OpenAI only
	Project      string // OpenAI only
	// APIVersion is used by Azure OpenAI (e.g. "2024-10-21").
	APIVersion string
	// Deployment is the Azure OpenAI deployment name. When empty, ChatRequest.Model is used.
	Deployment string
}

// ClientFactory is a function that creates ChatModel instances
type ClientFactory func(ClientConfig) (ChatModel, error)

var factories = make(map[ProviderType]ClientFactory)

// RegisterFactory registers a factory function for a provider
func RegisterFactory(provider ProviderType, factory ClientFactory) {
	factories[provider] = factory
}

// NewClient creates a ChatModel instance based on provider type.
// Providers must be registered via RegisterFactory or by importing their init packages.
//
// Example usage:
//
//	import (
//		"github.com/LingByte/lingllm/protocol"
//		_ "github.com/LingByte/lingllm/protocol/openai"
//	)
//
//	cfg := protocol.ClientConfig{
//		Provider: llm.ProviderOpenAI,
//		APIKey:   "sk-...",
//		BaseURL:  "https://api.openai.com/v1",
//	}
//	client, err := protocol.NewClient(cfg)
func NewClient(cfg ClientConfig) (ChatModel, error) {
	factory, ok := factories[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not registered; import the provider package to auto-register", cfg.Provider)
	}
	return factory(cfg)
}
