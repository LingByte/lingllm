package session

import (
	"testing"

	siprtp "github.com/LingByte/lingllm/protocol/sipmedia/rtp"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
)

func TestNewCallSession_PCMU(t *testing.T) {
	rtpSess, err := siprtp.NewSession(0)
	if err != nil {
		t.Fatal(err)
	}
	defer rtpSess.Close()

	cs, err := NewCallSession("test-call", rtpSess, []sdp.Codec{
		{PayloadType: 0, Name: "pcmu", ClockRate: 8000, Channels: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cs.CallID != "test-call" || cs.SourceCodec().Codec != "pcmu" {
		t.Fatalf("session: %+v codec=%+v", cs, cs.SourceCodec())
	}
	if cs.PCMSampleRate() != 8000 {
		t.Fatalf("pcm rate: %d", cs.PCMSampleRate())
	}
}

func TestInboundRetargetHeaders(t *testing.T) {
	cs := &CallSession{CallID: "c1"}
	cs.SetInboundRetargetHeaders("to", "hi", "dv")
	to, hi, dv := cs.InboundRetargetHeaders()
	if to != "to" || hi != "hi" || dv != "dv" {
		t.Fatalf("headers: %q %q %q", to, hi, dv)
	}
}
