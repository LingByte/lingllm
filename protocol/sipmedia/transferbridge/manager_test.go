package transferbridge

import "testing"

func TestManager_ActiveAndMigrate(t *testing.T) {
	m := NewManager()
	m.mu.Lock()
	m.bridges["in@a"] = &activeBridge{inboundID: "in@a", outboundID: "out@b", stop: func() {}}
	m.bridges["out@b"] = m.bridges["in@a"]
	m.mu.Unlock()

	if !m.Active("out@b") || !m.Active("in@a") {
		t.Fatal("expected active bridge")
	}
	m.MigrateOutboundCallID("in@a", "out@b", "leg2@newhost")
	if !m.Active("leg2@newhost") {
		t.Fatal("expected new outbound key")
	}
	if _, ok := m.bridges["out@b"]; ok {
		t.Fatal("old outbound key should be removed")
	}
	m.StopBridge("in@a")
	if m.Active("in@a") {
		t.Fatal("expected stopped")
	}
}

func TestNormCallIDAndLocalPart(t *testing.T) {
	if normCallID("  x@y ") != "x@y" {
		t.Fatal("norm")
	}
	if loc, ok := callLocalPart("abc@host"); !ok || loc != "abc" {
		t.Fatalf("local: %q %v", loc, ok)
	}
}
