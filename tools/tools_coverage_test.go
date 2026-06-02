package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LingByte/lingllm/protocol"
)

func TestExecuteWithToolsToolError(t *testing.T) {
	executor := NewSimpleToolExecutor()
	executor.RegisterTool(CalculatorTool(), func(json.RawMessage) (string, error) {
		return "", context.Canceled
	})

	model := &testChatModel{
		responses: []*protocol.ChatResponse{
			{
				Choices: []protocol.Choice{{
					Message: protocol.Message{
						Role: protocol.RoleAssistant,
						ToolCalls: []protocol.ToolCall{
							{ID: "call_1", Type: "function", Function: protocol.FunctionCall{Name: "calculate", Arguments: []byte("{}")}},
						},
					},
				}},
			},
			{Choices: []protocol.Choice{{Message: protocol.Message{Content: "done"}}}},
		},
	}

	tc := NewToolChain(model, executor)
	resp, err := tc.ExecuteWithTools(context.Background(), protocol.ChatRequest{
		Model: "test", Messages: []protocol.Message{{Role: protocol.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FirstContent() != "done" {
		t.Errorf("unexpected content: %s", resp.FirstContent())
	}
}

func TestJSONToolCallParserSkipsInvalidEntries(t *testing.T) {
	parser := &JSONToolCallParser{}
	response := &protocol.ChatResponse{
		Choices: []protocol.Choice{{Message: protocol.Message{
			Content: `{"tool_calls": ["bad", {"name": "calculate", "input": {"expression": "1"}}]}`,
		}}},
	}
	calls, err := parser.Parse(context.Background(), response)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(calls) != 1 || calls[0].Function.Name != "calculate" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}
