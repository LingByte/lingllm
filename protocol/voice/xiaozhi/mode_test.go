package xiaozhi

import (
	"context"
	"testing"

	"github.com/LingByte/lingllm/protocol/voice/asr"
	"github.com/LingByte/lingllm/protocol/voice/transport"
	"github.com/LingByte/lingllm/protocol/voice/tts"
)

func TestNewServer_RealtimeRequiresFactory(t *testing.T) {
	_, err := NewServer(ServerConfig{
		Mode: ModeRealtime,
	})
	if err == nil {
		t.Fatal("expected error without RealtimeFactory")
	}
}

func TestNewServer_PipelineRequiresDialog(t *testing.T) {
	_, err := NewServer(ServerConfig{
		Mode:           ModePipeline,
		SessionFactory: testFactory{},
	})
	if err == nil {
		t.Fatal("expected error without DialogWSURL")
	}
}

func TestNormalizeMode(t *testing.T) {
	if normalizeMode("realtime") != ModeRealtime {
		t.Fatalf("got %q", normalizeMode("realtime"))
	}
	if normalizeMode("omni") != ModeRealtime {
		t.Fatalf("got %q", normalizeMode("omni"))
	}
	if normalizeMode("") != ModePipeline {
		t.Fatalf("got %q", normalizeMode(""))
	}
}

type testFactory struct{}

func (testFactory) NewASR(_ context.Context, _ string) (asr.Engine, int, error) {
	return nil, 16000, nil
}
func (testFactory) NewTTS(_ context.Context, _ string) (tts.TTSService, int, error) {
	return nil, 16000, nil
}

var _ transport.SessionFactory = testFactory{}
