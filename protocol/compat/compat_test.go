package compat_test

import (
	"testing"

	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/compat"
)

func TestCompatibleProvidersRegistered(t *testing.T) {
	cases := []struct {
		provider protocol.ProviderType
		wantName string
		apiKey   string
	}{
		{protocol.ProviderXAI, "xai", "key"},
		{protocol.ProviderMistral, "mistral", "key"},
		{protocol.ProviderVolcArk, "volcengine", "key"},
		{protocol.ProviderHunyuan, "hunyuan", "key"},
		{protocol.ProviderErnie, "ernie", "key"},
		{protocol.ProviderVLLM, "vllm", ""},
		{protocol.ProviderLMStudio, "lmstudio", ""},
		{protocol.ProviderLocalAI, "localai", ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			client, err := protocol.NewClient(protocol.ClientConfig{
				Provider: tc.provider,
				APIKey:   tc.apiKey,
			})
			if err != nil {
				t.Fatalf("NewClient(%s): %v", tc.provider, err)
			}
			if client.Name() != tc.wantName {
				t.Fatalf("Name()=%q, want %q", client.Name(), tc.wantName)
			}
		})
	}
}

func TestCompatibleRequiresAPIKey(t *testing.T) {
	_, err := protocol.NewClient(protocol.ClientConfig{Provider: protocol.ProviderXAI})
	if err == nil {
		t.Fatal("expected error when API key missing for xai")
	}
}
