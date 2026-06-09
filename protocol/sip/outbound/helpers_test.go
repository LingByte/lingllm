package outbound

import (
	"net"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestPreviewBody(t *testing.T) {
	if got := previewBody("  hello  ", 10); got != "hello" {
		t.Fatalf("trim: %q", got)
	}
	if got := previewBody(strings.Repeat("a", 20), 5); got != "aaaaa...(truncated)" {
		t.Fatalf("truncate: %q", got)
	}
	if previewBody("", 5) != "" {
		t.Fatal("empty")
	}
}

func TestInviteTxKey(t *testing.T) {
	if inviteTxKey("", 1) != "" {
		t.Fatal("empty branch")
	}
	if inviteTxKey("z9hG4bKabc", 2) != "abc|2" {
		t.Fatalf("branch strip")
	}
}

func TestTxKeyFromResponse(t *testing.T) {
	raw := strings.Join([]string{
		"SIP/2.0 180 Ringing",
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bKdeadbeef;rport",
		"From: <sip:a@b>;tag=1",
		"To: <sip:a@b>;tag=2",
		"Call-ID: cid",
		"CSeq: 7 INVITE",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	resp, err := stack.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := txKeyFromResponse(resp); got != "deadbeef|7" {
		t.Fatalf("got %q", got)
	}
	if txKeyFromResponse(nil) != "" {
		t.Fatal("nil resp")
	}
}

func TestCallIDLocalPart(t *testing.T) {
	local, ok := callIDLocalPart("abc@host.example")
	if !ok || local != "abc" {
		t.Fatalf("got %q %v", local, ok)
	}
	if _, ok := callIDLocalPart("nohost"); ok {
		t.Fatal("expected false")
	}
}

func TestUdpAddrString(t *testing.T) {
	if udpAddrString(nil) != "" {
		t.Fatal("nil")
	}
	if udpAddrString(&net.UDPAddr{IP: net.ParseIP("1.1.1.1"), Port: 9}) == "" {
		t.Fatal("addr")
	}
}

func TestRandomHex(t *testing.T) {
	if len(randomHex(4)) != 8 {
		t.Fatalf("hex len %d", len(randomHex(4)))
	}
}
