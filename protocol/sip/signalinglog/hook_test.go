package signalinglog

import (
	"net"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestHook_LogRequest(t *testing.T) {
	h := NewHook("udp")
	raw := strings.Join([]string{
		"INVITE sip:a@b SIP/2.0",
		"Via: SIP/2.0/UDP 1.1.1.1;branch=z9hG4bK1",
		"From: <sip:a@b>;tag=1",
		"To: <sip:a@b>",
		"Call-ID: hook-cid",
		"CSeq: 1 INVITE",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	msg, err := stack.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	h.LogRequest(msg, &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5060})
	h.LogSent(msg, &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5060})
}

func TestHook_LogResponse(t *testing.T) {
	h := NewHook("tcp")
	raw := "SIP/2.0 200 OK\r\nCall-ID: r1\r\nContent-Length: 0\r\n\r\n"
	msg, err := stack.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	h.LogResponse(msg, nil)
}
