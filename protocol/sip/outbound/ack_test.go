package outbound

import (
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestBuildACK_Shape(t *testing.T) {
	inv := inviteParams{
		SIPHost:    "10.0.0.1",
		SIPPort:    5060,
		RequestURI: "sip:bob@10.0.0.2",
		CallID:     "cid@host",
		FromTag:    "ftag",
		FromUser:   "alice",
		Branch:     "branch1",
		CSeq:       1,
		ViaTransport: TransportUDP,
	}
	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK"}
	resp.SetHeader(stack.HeaderTo, "<sip:bob@10.0.0.2>;tag=rtag")
	ack := buildACK(inv, resp, "sip:bob@10.0.0.2")
	if ack == nil || ack.Method != stack.MethodAck {
		t.Fatalf("ACK: %+v", ack)
	}
	if ack.GetHeader(stack.HeaderCallID) != inv.CallID {
		t.Fatalf("call-id")
	}
	if ack.GetHeader(stack.HeaderCSeq) != stack.WithCSeqACK(inv.CSeq) {
		t.Fatalf("cseq")
	}
	if ack.GetHeader(stack.HeaderTo) != resp.GetHeader(stack.HeaderTo) {
		t.Fatalf("to from 200")
	}
}

func TestBuildACK_NilResponse(t *testing.T) {
	if buildACK(inviteParams{}, nil, "") != nil {
		t.Fatal("nil 200 → nil ACK")
	}
}

func TestAckRequestURI_ContactPreferred(t *testing.T) {
	resp := &stack.Message{IsRequest: false, StatusCode: 200}
	resp.SetHeader(stack.HeaderContact, "<sip:bob@203.0.113.1:5060>;transport=udp")
	got := ackRequestURI(resp, "sip:fallback@example")
	if got != "sip:bob@203.0.113.1:5060" {
		t.Fatalf("got %q", got)
	}
}

func TestAckRequestURI_ContactAngleBracketsOnly(t *testing.T) {
	resp := &stack.Message{IsRequest: false, StatusCode: 200}
	resp.SetHeader(stack.HeaderContact, "<sip:bob@example>")
	if got := ackRequestURI(resp, "x"); got != "sip:bob@example" {
		t.Fatalf("got %q", got)
	}
}

func TestAckRequestURI_FallbackWhenNoContact(t *testing.T) {
	resp := &stack.Message{IsRequest: false, StatusCode: 200}
	if got := ackRequestURI(resp, "sip:fallback@example"); got != "sip:fallback@example" {
		t.Fatalf("got %q", got)
	}
	if ackRequestURI(nil, "sip:fallback@example") != "sip:fallback@example" {
		t.Fatal("nil resp uses fallback")
	}
}
