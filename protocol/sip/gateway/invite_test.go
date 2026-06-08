package gateway

import (
	"strconv"
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func sampleINVITE() *stack.Message {
	body := sdp.Generate("127.0.0.1", 10000, []sdp.Codec{
		{PayloadType: 8, Name: "PCMA", ClockRate: 8000, Channels: 1},
		{PayloadType: 0, Name: "PCMU", ClockRate: 8000, Channels: 1},
	})
	raw := strings.Join([]string{
		"INVITE sip:callee@192.168.1.1 SIP/2.0",
		"Via: SIP/2.0/UDP 192.168.1.2:5060;branch=z9hG4bKinvite1",
		"From: <sip:caller@192.168.1.2>;tag=from1",
		"To: <sip:callee@192.168.1.1>",
		"Call-ID: gw-test-call",
		"CSeq: 1 INVITE",
		"Contact: <sip:gw@192.168.1.2:5060>",
		"Content-Type: application/sdp",
		"Content-Length: " + strconv.Itoa(len(body)),
		"",
		body,
	}, "\r\n")
	m, err := stack.Parse(raw)
	if err != nil {
		panic(err)
	}
	return m
}

func TestPickCodec_PrefersPCMA(t *testing.T) {
	offer, err := sdp.Parse(sampleINVITE().Body)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := PickCodec(offer)
	if !ok || !strings.EqualFold(c.Name, "pcma") {
		t.Fatalf("codec: %+v", c)
	}
}

func TestPickCodec_CustomPrefer(t *testing.T) {
	offer, _ := sdp.Parse(sampleINVITE().Body)
	c, ok := PickCodec(offer, "pcmu")
	if !ok || !strings.EqualFold(c.Name, "pcmu") {
		t.Fatalf("codec: %+v", c)
	}
}

func TestInviteAnswer(t *testing.T) {
	req := sampleINVITE()
	resp, dlg, err := InviteAnswer(req, "10.0.0.9", 20000, sdp.Codec{PayloadType: 8, Name: "PCMA", ClockRate: 8000, Channels: 1}, "localtag")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || dlg == nil || dlg.CallID != "gw-test-call" {
		t.Fatalf("resp=%d dlg=%+v", resp.StatusCode, dlg)
	}
	if !strings.Contains(resp.GetHeader("To"), "localtag") {
		t.Fatalf("to: %q", resp.GetHeader("To"))
	}
	if !strings.Contains(resp.Body, "10.0.0.9") {
		t.Fatal("sdp missing local ip")
	}
}

func TestRinging(t *testing.T) {
	resp, err := Ringing(sampleINVITE(), "ringtag")
	if err != nil || resp.StatusCode != 180 {
		t.Fatalf("ringing: %v %d", err, resp.StatusCode)
	}
	if !strings.Contains(resp.GetHeader("To"), "ringtag") {
		t.Fatal("missing tag on 180")
	}
}

func TestNewTag_NonEmpty(t *testing.T) {
	if NewTag() == "" {
		t.Fatal("empty tag")
	}
}
