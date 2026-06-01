package models

import "testing"

func TestOpenAIModelConstants(t *testing.T) {
	cases := map[string]string{
		"OpenAIGPT4o":      "gpt-4o",
		"OpenAIGPT52":      "gpt-5.2",
		"OpenAIGPT35Turbo": "gpt-3.5-turbo",
		"OpenAIO1":         "o1",
	}
	for name, want := range cases {
		var got string
		switch name {
		case "OpenAIGPT4o":
			got = OpenAIGPT4o
		case "OpenAIGPT52":
			got = OpenAIGPT52
		case "OpenAIGPT35Turbo":
			got = OpenAIGPT35Turbo
		case "OpenAIO1":
			got = OpenAIO1
		}
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestAnthropicModelConstants(t *testing.T) {
	if AnthropicClaudeOpus46 != "claude-opus-4-6" || AnthropicClaude3Sonnet != "claude-3-sonnet-20240229" {
		t.Error("unexpected Anthropic model constants")
	}
}

func TestOllamaModelConstants(t *testing.T) {
	if OllamaLlama32 != "llama3.2" || OllamaDeepSeekV3 != "deepseek-v3" || OllamaCommandR != "command-r" {
		t.Error("unexpected Ollama model constants")
	}
}

func TestThirdPartyModelConstants(t *testing.T) {
	if DeepSeekReasoner != "deepseek-reasoner" {
		t.Error("unexpected DeepSeek constants")
	}
	if Gemini25Flash != "gemini-2.5-flash" {
		t.Error("unexpected Gemini constants")
	}
	if MistralLarge != "mistral-large-latest" {
		t.Error("unexpected Mistral constants")
	}
}

func TestGatewayAliases(t *testing.T) {
	if GatewayGPT4o != OpenAIGPT4o ||
		GatewayClaudeSonnet != AnthropicClaudeSonnet46 ||
		GatewayDeepSeekChat != DeepSeekChat {
		t.Error("gateway aliases should match source models")
	}
}
