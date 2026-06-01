package protocol

import (
	"context"
	"testing"
)

type mockFactoryModel struct{}

func (m *mockFactoryModel) Name() string { return "mock" }
func (m *mockFactoryModel) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{}, nil
}
func (m *mockFactoryModel) StreamChat(ctx context.Context, req ChatRequest) (ChatStream, error) {
	return nil, nil
}

func TestRegisterFactoryAndNewChatModel(t *testing.T) {
	const testProvider ProviderType = "test-provider-register"
	RegisterFactory(testProvider, func(cfg ClientConfig) (ChatModel, error) {
		return &mockFactoryModel{}, nil
	})

	client, err := NewChatModel(ClientConfig{Provider: testProvider})
	if err != nil {
		t.Fatalf("NewChatModel failed: %v", err)
	}
	if client.Name() != "mock" {
		t.Errorf("unexpected client name: %s", client.Name())
	}
}

func TestNewChatModelUnregistered(t *testing.T) {
	_, err := NewChatModel(ClientConfig{Provider: ProviderType("unknown-provider-xyz")})
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}
