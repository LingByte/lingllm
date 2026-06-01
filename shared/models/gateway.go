package models

// OpenAI-compatible gateway aliases (aggregators, proxies, private deployments).
const (
	GatewayGPT4o        = OpenAIGPT4o
	GatewayGPT4oMini    = OpenAIGPT4oMini
	GatewayGPT41        = OpenAIGPT41
	GatewayClaudeSonnet = AnthropicClaudeSonnet46
	GatewayClaudeHaiku  = AnthropicClaudeHaiku45
	GatewayClaudeOpus   = AnthropicClaudeOpus46
	GatewayDeepSeekChat = DeepSeekChat
	GatewayGeminiFlash  = Gemini20Flash
	GatewayMistralLarge = MistralLarge
)
