package bridge

import (
	"testing"

	"github.com/LingByte/lingllm/media"
)

func TestBridgeMidPCM_WebSeatNarrowbandAgentUses8k(t *testing.T) {
	caller := media.CodecConfig{Codec: "g722", SampleRate: 16000}
	agent := media.CodecConfig{Codec: "pcma", SampleRate: 8000}
	mid := bridgeMidPCM(caller, agent)
	if mid.SampleRate != 8000 {
		t.Fatalf("mid SR=%d want 8000 (avoid upsampling agent speech)", mid.SampleRate)
	}
}

func TestBridgeMidPCM_DualG711Still8k(t *testing.T) {
	caller := media.CodecConfig{Codec: "pcmu", SampleRate: 8000}
	agent := media.CodecConfig{Codec: "pcma", SampleRate: 8000}
	mid := bridgeMidPCM(caller, agent)
	if mid.SampleRate != 8000 {
		t.Fatalf("mid SR=%d want 8000", mid.SampleRate)
	}
}

func TestBridgeMidPCM_WidebandBoth16k(t *testing.T) {
	caller := media.CodecConfig{Codec: "g722", SampleRate: 16000}
	agent := media.CodecConfig{Codec: "opus", SampleRate: 48000}
	mid := bridgeMidPCM(caller, agent)
	if mid.SampleRate != 16000 {
		t.Fatalf("mid SR=%d want 16000", mid.SampleRate)
	}
}
