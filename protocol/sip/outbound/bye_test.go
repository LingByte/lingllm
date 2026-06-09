package outbound

import (
	"net"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestBuildBYE_Headers(t *testing.T) {
	inv := inviteParams{
		SIPHost:    "127.0.0.1",
		SIPPort:    5060,
		RequestURI: "sip:bob@127.0.0.1",
		CallID:     "c@h",
		FromUser:   "alice",
		FromTag:    "ft",
		ViaTransport: TransportTCP,
	}
	msg := buildBYE(inv, "<sip:bob@127.0.0.1>;tag=bt", "sip:bob@127.0.0.1", 2, "br1")
	if msg.Method != stack.MethodBye {
		t.Fatalf("method %q", msg.Method)
	}
	if msg.GetHeader(stack.HeaderCSeq) != "2 BYE" {
		t.Fatalf("cseq %q", msg.GetHeader(stack.HeaderCSeq))
	}
	if !strings.Contains(msg.GetHeader(stack.HeaderVia), "TCP") {
		t.Fatalf("via %q", msg.GetHeader(stack.HeaderVia))
	}
}

func TestSendBYE_Errors(t *testing.T) {
	var m *Manager
	if err := m.SendBYE("x"); err == nil {
		t.Fatal("nil manager")
	}
	m = NewManager(ManagerConfig{})
	if err := m.SendBYE(""); err == nil {
		t.Fatal("empty call-id")
	}
	if err := m.SendBYE("missing"); err == nil {
		t.Fatal("unknown leg")
	}
}

func TestSendBYE_Success(t *testing.T) {
	var sent *stack.Message
	m := NewManager(ManagerConfig{SIPHost: "127.0.0.1", SIPPort: 5060})
	m.BindSender(mockSenderFunc(func(msg *stack.Message, _ *net.UDPAddr) error {
		sent = msg
		return nil
	}))
	leg := &outLeg{
		m: m,
		params: inviteParams{
			CallID: "cid@host", FromUser: "a", FromTag: "t",
			SIPHost: "127.0.0.1", SIPPort: 5060,
			RequestURI: "sip:b@127.0.0.1", CSeq: 1,
		},
		dst:           &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
		byeToHeader:   "<sip:b@127.0.0.1>;tag=rt",
		byeRequestURI: "sip:b@127.0.0.1",
		byeRemote:     &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
		byeCSeqNext:   2,
	}
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.mu.Unlock()

	if err := m.SendBYE(leg.params.CallID); err != nil {
		t.Fatalf("SendBYE: %v", err)
	}
	if sent == nil || sent.Method != stack.MethodBye {
		t.Fatalf("BYE not sent: %+v", sent)
	}
}

func TestSendBYE_DialogNotReady(t *testing.T) {
	m := NewManager(ManagerConfig{})
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { return nil }))
	leg := &outLeg{m: m, params: inviteParams{CallID: "c@h"}}
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.mu.Unlock()
	if err := m.SendBYE(leg.params.CallID); err == nil {
		t.Fatal("expected dialog not ready")
	}
}

func TestCleanupLegIfPresent_RemoteBye(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{
		m:      m,
		params: inviteParams{CallID: "cid@host"},
	}
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.mu.Unlock()

	m.CleanupLegIfPresent(leg.params.CallID, "")
	m.mu.Lock()
	_, ok := m.legs[leg.params.CallID]
	m.mu.Unlock()
	if ok {
		t.Fatal("leg should be removed")
	}
}

func TestCloneUDPAddr(t *testing.T) {
	if cloneUDPAddr(nil) != nil {
		t.Fatal("nil")
	}
	src := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 9}
	cp := cloneUDPAddr(src)
	if cp == src || cp.String() != src.String() {
		t.Fatalf("clone: %v vs %v", cp, src)
	}
}
