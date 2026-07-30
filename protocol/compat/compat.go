// Package compat registers OpenAI-compatible LLM provider presets.
//
// Import for side effects:
//
//	import _ "github.com/LingByte/lingllm/protocol/compat"
package compat

import (
	"github.com/LingByte/lingllm/protocol"
	"github.com/LingByte/lingllm/protocol/openai"
)

func init() {
	openai.RegisterCompatible(protocol.ProviderXAI, "https://api.x.ai/v1", false)
	openai.RegisterCompatible(protocol.ProviderMistral, "https://api.mistral.ai/v1", false)
	openai.RegisterCompatible(protocol.ProviderVolcArk, "https://ark.cn-beijing.volces.com/api/v3", false)
	openai.RegisterCompatible(protocol.ProviderHunyuan, "https://api.hunyuan.cloud.tencent.com/v1", false)
	openai.RegisterCompatible(protocol.ProviderErnie, "https://qianfan.baidubce.com/v2", false)
	openai.RegisterCompatible(protocol.ProviderVLLM, "http://127.0.0.1:8000/v1", true)
	openai.RegisterCompatible(protocol.ProviderLMStudio, "http://127.0.0.1:1234/v1", true)
	openai.RegisterCompatible(protocol.ProviderLocalAI, "http://127.0.0.1:8080/v1", true)
}
