package gateway

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/LingByte/lingllm/protocol/sip/uas"
)

func TestStartTCPListeners_DispatchOPTIONS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ep := stack.NewEndpoint(stack.EndpointConfig{
		Host: "127.0.0.1",
		Port: 0,
	})
	h := uas.Handlers{}
	_ = h.Attach(ep)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := StartTCPListeners(ctx, ep, TCPListenerConfig{TCPAddr: addr}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := "OPTIONS sip:u@h SIP/2.0\r\nVia: SIP/2.0/TCP 127.0.0.1;branch=z9hG4bK1\r\n" +
		"From: <sip:a@b>;tag=1\r\nTo: <sip:a@b>\r\nCall-ID: tcp1\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || !strings.Contains(string(buf[:n]), "200") {
		t.Fatalf("response: %q", string(buf[:n]))
	}
}
