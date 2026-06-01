package models

import "testing"

func TestOpenAIModelConstants(t *testing.T) {
	if OpenAIGPT4o != "gpt-4o" || OpenAIGPT52 != "gpt-5.2" {
		t.Error("unexpected OpenAI model constants")
	}
}

func TestAnthropicModelConstants(t *testing.T) {
	if AnthropicClaudeOpus46 != "claude-opus-4-6" {
		t.Error("unexpected Anthropic model constants")
	}
}

func TestOllamaModelConstants(t *testing.T) {
	if OllamaLlama32 != "llama3.2" || OllamaDeepSeekR1 != "deepseek-r1" {
		t.Error("unexpected Ollama model constants")
	}
}

func TestGatewayAliases(t *testing.T) {
	if GatewayGPT4o != OpenAIGPT4o || GatewayClaudeSonnet != AnthropicClaudeSonnet46 {
		t.Error("gateway aliases should match source models")
	}
}
