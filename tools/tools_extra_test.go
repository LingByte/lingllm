package tools

import (
	"context"
	"testing"

	"github.com/LingByte/lingllm/protocol"
)

func TestExecuteWithToolsModelError(t *testing.T) {
	tc := NewToolChain(&errorChatModel{}, NewSimpleToolExecutor())
	_, err := tc.ExecuteWithTools(context.Background(), protocol.ChatRequest{
		Model: "test", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected model error")
	}
}

type errorChatModel struct{}

func (m *errorChatModel) Name() string { return "err" }
func (m *errorChatModel) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
	return nil, context.Canceled
}
func (m *errorChatModel) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
	return nil, context.Canceled
}

func TestJSONToolCallParserNoChoices(t *testing.T) {
	parser := &JSONToolCallParser{}
	_, err := parser.Parse(context.Background(), &protocol.ChatResponse{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestJSONToolCallParserMissingToolCalls(t *testing.T) {
	parser := &JSONToolCallParser{}
	response := &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Content: `{"other": true}`}}},
	}
	_, err := parser.Parse(context.Background(), response)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReActToolCallParserEmptyActions(t *testing.T) {
	parser := &ReActToolCallParser{}
	response := &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{Content: "Thought: nothing"}}},
	}
	calls, err := parser.Parse(context.Background(), response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no calls, got %+v", calls)
	}
}

func TestExecuteWithToolsEmptyToolResults(t *testing.T) {
	model := &testChatModel{
		responses: []*protocol.ChatResponse{
			{Choices: []protocol.Choice{{Message: protocol.Message{Role: protocol.RoleAssistant, ToolCallID: "x", Content: "has-content"}}}},
		},
	}
	tc := NewToolChain(model, NewSimpleToolExecutor())
	resp, err := tc.ExecuteWithTools(context.Background(), protocol.ChatRequest{
		Model: "test", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FirstContent() != "has-content" {
		t.Errorf("unexpected response: %s", resp.FirstContent())
	}
}
