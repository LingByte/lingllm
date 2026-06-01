package models

// Ollama local model tags (ollama pull <tag>).
const (
	// Meta Llama
	OllamaLlama32     = "llama3.2"
	OllamaLlama32_1B  = "llama3.2:1b"
	OllamaLlama32_3B  = "llama3.2:3b"
	OllamaLlama31_8B  = "llama3.1:8b"
	OllamaLlama31_70B = "llama3.1:70b"
	OllamaLlama3_8B   = "llama3:8b"
	OllamaLlama3_70B  = "llama3:70b"

	// Qwen
	OllamaQwen25      = "qwen2.5"
	OllamaQwen25_7B   = "qwen2.5:7b"
	OllamaQwen25_14B  = "qwen2.5:14b"
	OllamaQwen25_32B  = "qwen2.5:32b"
	OllamaQwen25Coder = "qwen2.5-coder"

	// Mistral / Mixtral
	OllamaMistral     = "mistral"
	OllamaMistralNemo = "mistral-nemo"
	OllamaMixtral     = "mixtral"
	OllamaMixtral8x7B = "mixtral:8x7b"

	// Google Gemma
	OllamaGemma2  = "gemma2"
	OllamaGemma2_2B = "gemma2:2b"
	OllamaGemma2_9B = "gemma2:9b"

	// DeepSeek
	OllamaDeepSeekR1  = "deepseek-r1"
	OllamaDeepSeekV3  = "deepseek-v3"
	OllamaDeepSeekCoder = "deepseek-coder"

	// Microsoft Phi
	OllamaPhi3     = "phi3"
	OllamaPhi3Mini = "phi3:mini"

	// Code models
	OllamaCodellama     = "codellama"
	OllamaCodellama_7B  = "codellama:7b"
	OllamaCodellama_13B = "codellama:13b"

	// Cohere / others
	OllamaCommandR   = "command-r"
	OllamaCommandR7B = "command-r7b"
	OllamaSolar      = "solar"
	OllamaNousHermes = "nous-hermes2"
	OllamaYi         = "yi"
)
