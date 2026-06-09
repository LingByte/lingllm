package transfer

import (
	"net"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func sampleDialogINVITE() (*stack.Message, *net.UDPAddr) {
	raw := strings.Join([]string{
		"INVITE sip:agent@10.0.0.1 SIP/2.0",
		"Via: SIP/2.0/UDP 10.0.0.2:5060;branch=z9hG4bK1",
		"From: <sip:caller@10.0.0.2>;tag=from1",
		"To: <sip:agent@10.0.0.1>",
		"Call-ID: xfer-dialog",
		"CSeq: 3 INVITE",
		"Contact: <sip:gw@10.0.0.2:5060>",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	m, err := stack.Parse(raw)
	if err != nil {
		panic(err)
	}
	return m, &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 5060}
}

func TestDialogStore_RememberAndNotify(t *testing.T) {
	ds := NewDialogStore()
	inv, remote := sampleDialogINVITE()
	ds.Remember("xfer-dialog", remote, inv, `<sip:agent@10.0.0.1>;tag=local1`)

	msg, dst, err := ds.BuildNotify("xfer-dialog", "10.0.0.9", 5060, "SIP/2.0 100 Trying", "active;expires=60")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != stack.MethodNotify || dst.String() != remote.String() {
		t.Fatalf("notify: method=%s dst=%s", msg.Method, dst)
	}
	if msg.GetHeader(stack.HeaderEvent) != "refer" || !strings.Contains(msg.Body, "100 Trying") {
		t.Fatalf("notify headers/body: %+v", msg.Headers)
	}
}

func TestDialogStore_BuildBye(t *testing.T) {
	ds := NewDialogStore()
	inv, remote := sampleDialogINVITE()
	ds.Remember("xfer-dialog", remote, inv, `<sip:agent@10.0.0.1>;tag=local1`)

	msg, dst, err := ds.BuildBye("xfer-dialog", "10.0.0.9", 5060)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != stack.MethodBye || dst.Port != remote.Port {
		t.Fatalf("bye: %+v", msg)
	}
}

func TestDialogStore_Forget(t *testing.T) {
	ds := NewDialogStore()
	inv, remote := sampleDialogINVITE()
	ds.Remember("xfer-dialog", remote, inv, "to")
	ds.Forget("xfer-dialog")
	if ds.Get("xfer-dialog") != nil {
		t.Fatal("expected gone")
	}
}
