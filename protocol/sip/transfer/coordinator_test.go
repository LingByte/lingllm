package transfer

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/outbound"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestCoordinator_HandleRefer_MissingDialog(t *testing.T) {
	c := NewCoordinator(Config{})
	req, _ := stack.Parse(strings.Join([]string{
		"REFER sip:a@b SIP/2.0",
		"Via: SIP/2.0/UDP 1.1.1.1;branch=z9hG4bK1",
		"From: <sip:a@b>;tag=1",
		"To: <sip:a@b>;tag=2",
		"Call-ID: cid",
		"CSeq: 2 REFER",
		"Refer-To: <sip:agent@10.0.0.1>",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n"))
	resp, err := c.HandleRefer(req, &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 5060})
	if err != nil || resp == nil || resp.StatusCode != 481 {
		t.Fatalf("want 481: err=%v status=%v", err, resp)
	}
}

func TestCoordinator_HandleRefer_202(t *testing.T) {
	var mu sync.Mutex
	var sent bool
	c := NewCoordinator(Config{
		LocalIP: "10.0.0.9",
		SIPPort: 5060,
		Dial: func(context.Context, outbound.DialRequest) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			sent = true
			return "out-call", nil
		},
		SendSIP: func(*stack.Message, *net.UDPAddr) error { return nil },
	})
	inv, remote := sampleDialogINVITE()
	c.Dialogs.Remember("xfer-dialog", remote, inv, `<sip:agent@10.0.0.1>;tag=local1`)

	raw := strings.Join([]string{
		"REFER sip:agent@10.0.0.1 SIP/2.0",
		"Via: SIP/2.0/UDP 10.0.0.2;branch=z9hG4bK1",
		"From: <sip:caller@10.0.0.2>;tag=from1",
		"To: <sip:agent@10.0.0.1>;tag=local1",
		"Call-ID: xfer-dialog",
		"CSeq: 4 REFER",
		"Refer-To: <sip:target@192.168.0.10>",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	req, err := stack.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.HandleRefer(req, remote)
	if err != nil || resp.StatusCode != 202 {
		t.Fatalf("refer: err=%v status=%d", err, resp.StatusCode)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		mu.Lock()
		if sent {
			mu.Unlock()
			break
		}
		mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("expected dial to run")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCoordinator_HandleDialEvent_ReferNotify(t *testing.T) {
	var frag string
	c := NewCoordinator(Config{})
	c.mu.Lock()
	c.referNotifyWait["out1"] = func(s, _ string) { frag = s }
	c.mu.Unlock()

	c.HandleDialEvent(outbound.DialEvent{
		CallID:        "out1",
		CorrelationID: "in1",
		Scenario:      outbound.ScenarioTransferAgent,
		State:         outbound.DialEventEstablished,
	})
	if frag != "SIP/2.0 200 OK" {
		t.Fatalf("frag: %q", frag)
	}
}
