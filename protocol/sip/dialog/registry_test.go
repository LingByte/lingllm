package dialog

import (
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestRegistry_PutGetDelete(t *testing.T) {
	r := NewRegistry()
	raw := strings.Join([]string{
		"INVITE sip:a@b SIP/2.0",
		"Via: SIP/2.0/UDP 1.1.1.1;branch=z9hG4bK1",
		"From: <sip:a@b>;tag=1",
		"To: <sip:a@b>",
		"Call-ID: reg1",
		"CSeq: 1 INVITE",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	inv, err := stack.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewUASFromINVITE(inv)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Put(d); err != nil {
		t.Fatal(err)
	}
	if r.Get("reg1") != d {
		t.Fatal("get")
	}
	r.Delete("reg1")
	if r.Get("reg1") != nil {
		t.Fatal("delete")
	}
}

func TestRegistry_PutErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.Put(nil); err == nil {
		t.Fatal("nil dialog")
	}
	if err := r.Put(&Dialog{}); err == nil {
		t.Fatal("empty call-id")
	}
}
