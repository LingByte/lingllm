// Package models defines commonly used LLM model identifier constants.
package models

// OpenAI model identifiers.
const (
	OpenAIGPT4o      = "gpt-4o"
	OpenAIGPT4oMini  = "gpt-4o-mini"
	OpenAIGPT4Turbo  = "gpt-4-turbo"
	OpenAIGPT41      = "gpt-4.1"
	OpenAIGPT41Mini  = "gpt-4.1-mini"
	OpenAIGPT41Nano  = "gpt-4.1-nano"
	OpenAIO3         = "o3"
	OpenAIO3Mini     = "o3-mini"
	OpenAIO4Mini     = "o4-mini"
	OpenAIGPT5       = "gpt-5"
	OpenAIGPT52      = "gpt-5.2"
)

// Anthropic Claude model identifiers.
const (
	AnthropicClaudeOpus46   = "claude-opus-4-6"
	AnthropicClaudeSonnet46 = "claude-sonnet-4-6"
	AnthropicClaudeHaiku45  = "claude-haiku-4-5"
	AnthropicClaudeOpus41   = "claude-opus-4-1"
	AnthropicClaudeSonnet4  = "claude-sonnet-4-0"
	AnthropicClaude35Sonnet = "claude-3-5-sonnet-20241022"
	AnthropicClaude35Haiku  = "claude-3-5-haiku-20241022"
	AnthropicClaude3Opus    = "claude-3-opus-20240229"
	AnthropicClaude3Haiku   = "claude-3-haiku-20240307"
)

// Ollama local model identifiers (tags as pulled via ollama pull).
const (
	OllamaLlama32      = "llama3.2"
	OllamaLlama32_1B   = "llama3.2:1b"
	OllamaLlama32_3B   = "llama3.2:3b"
	OllamaLlama31_8B   = "llama3.1:8b"
	OllamaLlama31_70B  = "llama3.1:70b"
	OllamaQwen25       = "qwen2.5"
	OllamaQwen25_7B    = "qwen2.5:7b"
	OllamaQwen25_14B   = "qwen2.5:14b"
	OllamaMistral      = "mistral"
	OllamaMixtral      = "mixtral"
	OllamaGemma2       = "gemma2"
	OllamaDeepSeekR1   = "deepseek-r1"
	OllamaPhi3         = "phi3"
)

// OpenAI-compatible gateway models (custom providers, e.g. aggregators).
const (
	GatewayGPT4o     = OpenAIGPT4o
	GatewayGPT4oMini = OpenAIGPT4oMini
	GatewayClaudeSonnet = AnthropicClaudeSonnet46
)
