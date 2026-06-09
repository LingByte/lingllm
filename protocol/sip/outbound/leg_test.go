package outbound

import (
	"context"
	"net"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestOutLeg_HandleResponse_NonInvite200Ignored(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{m: m, params: inviteParams{CallID: "c@h"}}
	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK"}
	resp.SetHeader(stack.HeaderCSeq, "2 OPTIONS")
	leg.handleResponse(context.Background(), resp, nil)
}

func TestOutLeg_HandleResponse_FailedInvite(t *testing.T) {
	var ev DialEvent
	m := NewManager(ManagerConfig{
		OnEvent: func(e DialEvent) { ev = e },
	})
	leg := &outLeg{
		m: m,
		params: inviteParams{CallID: "c@h"},
		req:    DialRequest{Target: DialTarget{RequestURI: "sip:b@x"}},
	}
	resp := &stack.Message{IsRequest: false, StatusCode: 486, StatusText: "Busy Here"}
	resp.SetHeader(stack.HeaderCSeq, "1 INVITE")
	leg.handleResponse(context.Background(), resp, nil)
	if ev.State != DialEventFailed || ev.StatusCode != 486 {
		t.Fatalf("event %+v", ev)
	}
	m.mu.Lock()
	_, ok := m.legs["c@h"]
	m.mu.Unlock()
	if ok {
		t.Fatal("leg cleaned up")
	}
}

func TestOutLeg_HandleResponse_200WithoutSDP(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{
		m: m,
		params: inviteParams{CallID: "c@h", RequestURI: "sip:b@x"},
		req:    DialRequest{},
	}
	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK"}
	resp.SetHeader(stack.HeaderCSeq, "1 INVITE")
	leg.handleResponse(context.Background(), resp, nil)
}

func TestOutLeg_HandleResponse_BadSDP(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{
		m: m,
		params: inviteParams{CallID: "c@h", RequestURI: "sip:b@x"},
		req:    DialRequest{},
	}
	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK", Body: "not sdp"}
	resp.SetHeader(stack.HeaderCSeq, "1 INVITE")
	leg.handleResponse(context.Background(), resp, nil)
}

func TestOutLeg_HandleResponse_ByeAck(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{m: m, params: inviteParams{CallID: "c@h"}}
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.mu.Unlock()
	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK"}
	resp.SetHeader(stack.HeaderCSeq, "3 BYE")
	leg.handleResponse(context.Background(), resp, nil)
	m.mu.Lock()
	_, ok := m.legs[leg.params.CallID]
	m.mu.Unlock()
	if ok {
		t.Fatal("leg cleaned on BYE 200")
	}
}

func TestOutLeg_HandleResponse_EstablishedSendsACK(t *testing.T) {
	var ackSent bool
	m := NewManager(ManagerConfig{})
	m.BindSender(mockSenderFunc(func(msg *stack.Message, _ *net.UDPAddr) error {
		if msg.Method == stack.MethodAck {
			ackSent = true
		}
		return nil
	}))
	leg := &outLeg{
		m: m,
		params: inviteParams{
			CallID: "c@h", RequestURI: "sip:b@127.0.0.1",
			SIPHost: "127.0.0.1", SIPPort: 5060,
			FromUser: "a", FromTag: "t", Branch: "br", CSeq: 1,
		},
		req: DialRequest{Target: DialTarget{RequestURI: "sip:b@127.0.0.1"}},
		dst: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
	}
	answerSDP := sdp.Generate("203.0.113.2", 4000, sdp.DefaultOutboundOfferCodecs())
	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK", Body: answerSDP}
	resp.SetHeader(stack.HeaderCSeq, "1 INVITE")
	resp.SetHeader(stack.HeaderTo, "<sip:b@127.0.0.1>;tag=rt")
	leg.handleResponse(context.Background(), resp, leg.dst)
	if !ackSent {
		t.Fatal("ACK not sent")
	}
	leg.mu.Lock()
	ok := leg.established
	leg.mu.Unlock()
	if !ok {
		t.Fatal("not established")
	}
}

func TestOutLeg_CleanupLeg(t *testing.T) {
	m := NewManager(ManagerConfig{})
	leg := &outLeg{
		m:      m,
		params: inviteParams{CallID: "c@h"},
		txKey:  "k|1",
	}
	m.mu.Lock()
	m.legs[leg.params.CallID] = leg
	m.legsByTx[leg.txKey] = leg
	m.mu.Unlock()
	leg.cleanupLeg()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.legs) != 0 || len(m.legsByTx) != 0 {
		t.Fatal("maps not cleared")
	}
}

func TestOutLeg_EmitEvent_NilSafe(t *testing.T) {
	leg := &outLeg{}
	leg.emitEvent(DialEventInvited, 0, "", nil)
}

func TestOutLeg_PreAckHook(t *testing.T) {
	var preAck bool
	m := NewManager(ManagerConfig{
		PreAck: func(ctx context.Context, p PreAckContext) error {
			preAck = true
			if p.Answer == nil || p.Leg.CallID == "" {
				t.Fatal("pre-ack context")
			}
			return nil
		},
	})
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { return nil }))
	leg := &outLeg{
		m: m,
		params: inviteParams{
			CallID: "c@h", RequestURI: "sip:b@127.0.0.1",
			SIPHost: "127.0.0.1", SIPPort: 5060,
			FromUser: "a", FromTag: "t", Branch: "br", CSeq: 1,
		},
		req: DialRequest{Target: DialTarget{RequestURI: "sip:b@127.0.0.1"}},
		dst: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
	}
	answerSDP := sdp.Generate("203.0.113.2", 4000, sdp.DefaultOutboundOfferCodecs())
	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK", Body: answerSDP}
	resp.SetHeader(stack.HeaderCSeq, "1 INVITE")
	resp.SetHeader(stack.HeaderTo, "<sip:b@127.0.0.1>;tag=rt")
	leg.handleResponse(context.Background(), resp, leg.dst)
	if !preAck {
		t.Fatal("PreAck not called")
	}
}
