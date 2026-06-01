package anthropic

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol"
)

func TestAnthropicStreamMetrics(t *testing.T) {
	now := time.Now()
	s := &anthropicStream{
		startAt: now, firstAt: now, endAt: now, model: "claude",
		usage: protocol.TokenUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		chunks: 1, bytes: 10,
	}
	m := s.Metrics()
	if m.Provider != "anthropic" || m.TotalTokens != 3 {
		t.Errorf("unexpected metrics: %+v", m)
	}
}

func TestAnthropicStreamDone(t *testing.T) {
	s := &anthropicStream{body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")), model: "claude"}
	_, err := s.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestAnthropicStreamReadLinePartialEOF(t *testing.T) {
	s := &anthropicStream{body: io.NopCloser(strings.NewReader("partial"))}
	line, err := s.readLine()
	if line != "partial" || err != io.EOF {
		t.Fatalf("unexpected: %q err=%v", line, err)
	}
}

func TestAnthropicStreamRecvEOFOnBody(t *testing.T) {
	s := &anthropicStream{body: io.NopCloser(strings.NewReader(""))}
	_, err := s.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}
