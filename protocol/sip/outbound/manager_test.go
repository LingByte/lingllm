package outbound

import (
	"context"
	"testing"
)

func TestNewManager_Defaults(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if m.cfg.FromUser != "lingllm" || m.cfg.DefaultRTPPort != 10000 {
		t.Fatalf("defaults: %+v", m.cfg)
	}
}

func TestManager_DialWithoutSender(t *testing.T) {
	m := NewManager(ManagerConfig{})
	_, err := m.Dial(context.Background(), DialRequest{
		Target: DialTarget{RequestURI: "sip:a@b", SignalingAddr: "127.0.0.1:5060"},
	})
	if err != ErrNoSignalingSender {
		t.Fatalf("got %v want ErrNoSignalingSender", err)
	}
}
