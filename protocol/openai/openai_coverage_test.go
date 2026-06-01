package openai

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol"
)

func TestOpenAIStreamMetrics(t *testing.T) {
	now := time.Now()
	s := &openAIStream{
		startAt: now,
		firstAt: now,
		endAt:   now,
		model:   "gpt-4",
		usage: protocol.TokenUsage{
			PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3,
		},
		chunks: 2, bytes: 100, httpStatus: 200,
	}

	m := s.Metrics()
	if m.Provider != "openai" || m.Model != "gpt-4" || m.TotalTokens != 3 {
		t.Errorf("unexpected metrics: %+v", m)
	}
}

func TestOpenAIStreamRecvUsageAndEmptyChoices(t *testing.T) {
	body := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"y\"}}]}\n\n" +
		"data: [DONE]\n\n"
	s := &openAIStream{body: io.NopCloser(strings.NewReader(body)), model: "gpt-4"}
	chunk, err := s.Recv()
	if err != nil || chunk.Delta != "y" {
		t.Fatalf("Recv failed: chunk=%+v err=%v", chunk, err)
	}
	if s.Metrics().TotalTokens != 3 {
		t.Errorf("expected usage in metrics")
	}
}

func TestOpenAIStreamReadLineError(t *testing.T) {
	s := &openAIStream{body: io.NopCloser(&failReader{})}
	_, err := s.readLine()
	if err == nil {
		t.Fatal("expected read error")
	}
}

type failReader struct{}

func (f *failReader) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
