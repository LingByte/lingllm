package outbound

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

type mockSenderFunc func(*stack.Message, *net.UDPAddr) error

func (f mockSenderFunc) SendSIP(msg *stack.Message, addr *net.UDPAddr) error {
	return f(msg, addr)
}

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

func TestManager_Dial_Validation(t *testing.T) {
	m := NewManager(ManagerConfig{})
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { return nil }))
	if _, err := m.Dial(context.Background(), DialRequest{
		Target: DialTarget{SignalingAddr: "127.0.0.1:5060"},
	}); err == nil {
		t.Fatal("empty request URI")
	}
	if _, err := m.Dial(context.Background(), DialRequest{
		Target: DialTarget{RequestURI: "sip:a@b"},
	}); err == nil {
		t.Fatal("empty signaling addr")
	}
}

func TestManager_Dial_SendsInvite(t *testing.T) {
	var mu sync.Mutex
	var invites []*stack.Message
	m := NewManager(ManagerConfig{
		LocalIP: "127.0.0.1", SIPHost: "127.0.0.1", SIPPort: 5060,
		FromUser: "campaign",
	})
	m.BindSender(mockSenderFunc(func(msg *stack.Message, _ *net.UDPAddr) error {
		mu.Lock()
		invites = append(invites, msg)
		mu.Unlock()
		return nil
	}))
	cid, err := m.Dial(context.Background(), DialRequest{
		Scenario: ScenarioCampaign,
		Target: DialTarget{
			RequestURI:    "sip:bob@203.0.113.1",
			SignalingAddr: "127.0.0.1:5060",
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if cid == "" {
		t.Fatal("empty call-id")
	}
	mu.Lock()
	n := len(invites)
	mu.Unlock()
	if n != 1 || invites[0].Method != stack.MethodInvite {
		t.Fatalf("expected 1 INVITE, got %d", n)
	}
}

func TestManager_HandleSIPResponse_ProvisionalAndEstablished(t *testing.T) {
	var events []DialEvent
	var established bool
	m := NewManager(ManagerConfig{
		SIPHost: "127.0.0.1", SIPPort: 5060,
		OnEvent: func(e DialEvent) { events = append(events, e) },
		OnEstablished: func(EstablishedLeg) { established = true },
	})
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { return nil }))

	leg := &outLeg{
		m: m,
		params: inviteParams{
			CallID: "cid@127.0.0.1", Branch: "z9hG4bKabc", CSeq: 1,
			RequestURI: "sip:bob@127.0.0.1", SIPHost: "127.0.0.1", SIPPort: 5060,
			FromUser: "alice", FromTag: "ft",
		},
		req: DialRequest{Target: DialTarget{RequestURI: "sip:bob@127.0.0.1"}},
		dst: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
		txKey: inviteTxKey("abc", 1),
	}
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.legsByTx[leg.txKey] = leg
	m.mu.Unlock()

	ring := &stack.Message{IsRequest: false, StatusCode: 180, StatusText: "Ringing"}
	ring.SetHeader(stack.HeaderCallID, leg.params.CallID)
	ring.SetHeader(stack.HeaderCSeq, "1 INVITE")
	ring.SetHeader(stack.HeaderVia, "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bKabc")
	m.HandleSIPResponse(ring, leg.dst)
	if len(events) != 1 || events[0].State != DialEventProvisional {
		t.Fatalf("provisional events: %+v", events)
	}
	if leg.params.CallID != "cid@127.0.0.1" {
		t.Fatal("1xx must not adopt call-id")
	}

	answerSDP := sdp.Generate("203.0.113.2", 4000, sdp.DefaultOutboundOfferCodecs())
	ok := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK", Body: answerSDP}
	ok.SetHeader(stack.HeaderCallID, "dialog@carrier.example")
	ok.SetHeader(stack.HeaderCSeq, "1 INVITE")
	ok.SetHeader(stack.HeaderVia, "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bKabc")
	ok.SetHeader(stack.HeaderTo, "<sip:bob@127.0.0.1>;tag=remote")
	ok.SetHeader(stack.HeaderContact, "<sip:bob@203.0.113.2:5060>")
	m.HandleSIPResponse(ok, leg.dst)

	if leg.params.CallID != "dialog@carrier.example" {
		t.Fatalf("2xx should adopt call-id, got %q", leg.params.CallID)
	}
	if !established {
		t.Fatal("OnEstablished not called")
	}
}

func TestManager_AdoptCallID_CollisionRefused(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{m: m, params: inviteParams{CallID: "old@host"}}
	other := &outLeg{m: m, params: inviteParams{CallID: "new@host"}}
	m.mu.Lock()
	m.legs["old@host"] = leg
	m.legs["new@host"] = other
	m.mu.Unlock()

	resp := &stack.Message{IsRequest: false, StatusCode: 200}
	resp.SetHeader(stack.HeaderCallID, "new@host")
	m.adoptOutboundDialogCallIDIfNeeded(leg, resp)
	if leg.params.CallID != "old@host" {
		t.Fatalf("collision must refuse adopt, got %q", leg.params.CallID)
	}
}

func TestManager_LegByCallIDOrHostRewrite(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{m: m, params: inviteParams{CallID: "abc@local.example"}}
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.mu.Unlock()
	if got := m.legByCallIDOrHostRewrite("abc@other.example"); got != leg {
		t.Fatal("host rewrite match")
	}
	if got := m.legByCallIDOrHostRewrite("missing@x"); got != nil {
		t.Fatal("missing")
	}
}

func TestManager_ClosePool(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if err := m.ClosePool(); err != nil {
		t.Fatal(err)
	}
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { return nil }))
	_, _ = m.Dial(context.Background(), DialRequest{
		Target: DialTarget{RequestURI: "sip:a@b", SignalingAddr: "127.0.0.1:5060"},
	})
	if err := m.ClosePool(); err != nil {
		t.Fatalf("ClosePool: %v", err)
	}
}

func TestManager_OnSignalingSentHook(t *testing.T) {
	var sent bool
	m := NewManager(ManagerConfig{
		LocalIP: "127.0.0.1", SIPHost: "127.0.0.1", SIPPort: 5060,
		OnSignalingSent: func(*stack.Message, *net.UDPAddr) { sent = true },
	})
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { return nil }))
	_, err := m.Dial(context.Background(), DialRequest{
		Target: DialTarget{RequestURI: "sip:a@b", SignalingAddr: "127.0.0.1:5060"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("OnSignalingSent not fired")
	}
}

func TestManager_Dial_OfferSRTP(t *testing.T) {
	var body string
	m := NewManager(ManagerConfig{LocalIP: "127.0.0.1", SIPHost: "127.0.0.1", SIPPort: 5060})
	m.BindSender(mockSenderFunc(func(msg *stack.Message, _ *net.UDPAddr) error {
		body = msg.Body
		return nil
	}))
	_, err := m.Dial(context.Background(), DialRequest{
		Target:    DialTarget{RequestURI: "sip:a@b", SignalingAddr: "127.0.0.1:5060"},
		OfferSRTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "a=crypto") {
		t.Fatalf("SRTP offer missing crypto: %q", body)
	}
}
