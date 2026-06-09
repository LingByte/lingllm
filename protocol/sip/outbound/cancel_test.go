package outbound

import (
	"net"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestBuildCANCEL_MatchesInviteTransaction(t *testing.T) {
	inv := inviteParams{
		RequestURI:   "sip:bob@example.com",
		SIPHost:      "10.0.0.1",
		SIPPort:      5060,
		CallID:       "cid@host",
		FromUser:     "alice",
		FromTag:      "ft",
		Branch:       "deadbeef",
		CSeq:         7,
		ViaTransport: TransportUDP,
	}
	msg := buildCANCEL(inv)
	if msg.Method != stack.MethodCancel {
		t.Fatalf("method %q", msg.Method)
	}
	if msg.GetHeader(stack.HeaderCSeq) != "7 CANCEL" {
		t.Fatalf("cseq %q", msg.GetHeader(stack.HeaderCSeq))
	}
	via := msg.GetHeader(stack.HeaderVia)
	if !strings.Contains(via, "z9hG4bKdeadbeef") {
		t.Fatalf("branch not preserved: %q", via)
	}
}

func TestSendCANCEL_Errors(t *testing.T) {
	var m *Manager
	if err := m.SendCANCEL("x"); err == nil {
		t.Fatal("nil manager")
	}
	m = NewManager(ManagerConfig{})
	if err := m.SendCANCEL(""); err == nil {
		t.Fatal("empty call-id")
	}
	if err := m.SendCANCEL("missing"); err == nil {
		t.Fatal("unknown leg")
	}
}

func TestSendCANCEL_EstablishedRejected(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{
		m:      m,
		params: inviteParams{CallID: "cid@host"},
	}
	leg.mu.Lock()
	leg.established = true
	leg.mu.Unlock()
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.mu.Unlock()
	if err := m.SendCANCEL(leg.params.CallID); err == nil {
		t.Fatal("established leg must reject CANCEL")
	}
}

func TestSendCANCEL_ImmediateWhenProvisional(t *testing.T) {
	var sent int
	m := NewManager(ManagerConfig{SIPHost: "127.0.0.1", SIPPort: 5060})
	m.BindSender(mockSenderFunc(func(msg *stack.Message, _ *net.UDPAddr) error {
		if msg.Method == stack.MethodCancel {
			sent++
		}
		return nil
	}))
	leg := &outLeg{
		m: m,
		params: inviteParams{
			CallID: "cid@host", RequestURI: "sip:b@127.0.0.1",
			SIPHost: "127.0.0.1", SIPPort: 5060,
			FromUser: "a", FromTag: "t", Branch: "br", CSeq: 1,
		},
		dst: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
	}
	leg.gotProvisional.Store(true)
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.mu.Unlock()

	if err := m.SendCANCEL(leg.params.CallID); err != nil {
		t.Fatalf("SendCANCEL: %v", err)
	}
	if sent != 1 {
		t.Fatalf("expected 1 CANCEL, got %d", sent)
	}
}

func TestStopCANCELRetransmit_Idempotent(t *testing.T) {
	leg := &outLeg{}
	leg.stopCANCELRetransmit()
	leg.cancelStopMu.Lock()
	leg.cancelStop = make(chan struct{})
	leg.cancelStopMu.Unlock()
	leg.stopCANCELRetransmit()
	leg.stopCANCELRetransmit()
}

func TestBuildAndSendCANCEL(t *testing.T) {
	var sent int
	m := NewManager(ManagerConfig{SIPHost: "127.0.0.1", SIPPort: 5060})
	m.BindSender(mockSenderFunc(func(msg *stack.Message, _ *net.UDPAddr) error {
		if msg.Method == stack.MethodCancel {
			sent++
		}
		return nil
	}))
	leg := &outLeg{
		m: m,
		params: inviteParams{
			CallID: "cid@host", RequestURI: "sip:b@127.0.0.1",
			SIPHost: "127.0.0.1", SIPPort: 5060,
			FromUser: "a", FromTag: "t", Branch: "br", CSeq: 1,
		},
		dst: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
	}
	leg.gotProvisional.Store(true)
	if err := buildAndSendCANCEL(leg); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("sent=%d", sent)
	}
}

func TestFireDeferredCANCEL_Delegates(t *testing.T) {
	m := NewManager(ManagerConfig{})
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { return nil }))
	leg := &outLeg{
		m: m,
		params: inviteParams{CallID: "c@h", Branch: "b", CSeq: 1},
		dst: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
	}
	leg.gotProvisional.Store(true)
	leg.fireDeferredCANCEL()
	if !leg.cancelSent.Load() {
		t.Fatal("cancel sent")
	}
}
