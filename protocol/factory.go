package protocol

import "fmt"

// ProviderType defines supported LLM providers
type ProviderType string

const (
	ProviderOpenAI         ProviderType = "openai"
	ProviderAnthropic      ProviderType = "anthropic"
	ProviderOllama         ProviderType = "ollama"
	ProviderOpenAIResponse ProviderType = "openai-response"
)

// ClientConfig holds configuration for creating LLM clients
type ClientConfig struct {
	Provider     ProviderType
	APIKey       string
	BaseURL      string
	Model        string
	Organization string // OpenAI only
	Project      string // OpenAI only
}

// ClientFactory is a function that creates ChatModel instances
type ClientFactory func(ClientConfig) (ChatModel, error)

var factories = make(map[ProviderType]ClientFactory)

// RegisterFactory registers a factory function for a provider
func RegisterFactory(provider ProviderType, factory ClientFactory) {
	factories[provider] = factory
}

// NewChatModel creates a ChatModel instance based on provider type.
// Providers must be registered via RegisterFactory or by importing their init packages.
//
// Example usage:
//
//	import (
//		"github.com/LingByte/LingVoice/pkg/protocol/llm"
//		_ "github.com/LingByte/LingVoice/pkg/protocol/llm/openai"  // auto-registers
//	)
//
//	cfg := llm.ClientConfig{
//		Provider: llm.ProviderOpenAI,
//		APIKey:   "sk-...",
//		Model:    "gpt-4",
//	}
//	client, err := llm.NewChatModel(cfg)
func NewChatModel(cfg ClientConfig) (ChatModel, error) {
	factory, ok := factories[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not registered; import the provider package to auto-register", cfg.Provider)
	}
	return factory(cfg)
}
