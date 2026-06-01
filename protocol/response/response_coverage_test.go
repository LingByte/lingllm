package response

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol"
)

func TestResponsesStreamMetrics(t *testing.T) {
	now := time.Now()
	s := &responsesStream{
		startAt: now, firstAt: now, endAt: now, model: "gpt-4",
		usage: protocol.TokenUsage{TotalTokens: 5},
		chunks: 1, bytes: 20, httpStatus: 200,
	}
	m := s.Metrics()
	if m.Provider != "openai-responses" || m.TotalTokens != 5 {
		t.Errorf("unexpected metrics: %+v", m)
	}
}

func TestResponsesStreamDoneAndSkipEmptyDelta(t *testing.T) {
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"z\"}}]}\n\n" +
		"data: [DONE]\n\n"
	s := &responsesStream{body: io.NopCloser(strings.NewReader(body)), model: "gpt-4"}
	chunk, err := s.Recv()
	if err != nil || chunk.Delta != "z" {
		t.Fatalf("Recv failed: chunk=%+v err=%v", chunk, err)
	}
}

func TestResponsesStreamReadLinePartialEOF(t *testing.T) {
	s := &responsesStream{body: io.NopCloser(strings.NewReader("line"))}
	line, err := s.readLine()
	if line != "line" || err != io.EOF {
		t.Fatalf("unexpected: %q err=%v", line, err)
	}
}
