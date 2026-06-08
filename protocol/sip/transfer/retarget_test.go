package transfer

import (
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/historyinfo"
	"github.com/LingByte/lingllm/protocol/sip/outbound"
)

func TestApplyRetargetHeaders_REFER(t *testing.T) {
	req := &outbound.DialRequest{
		Target: outbound.DialTarget{RequestURI: "sip:agent@10.0.0.5"},
	}
	ApplyRetargetHeaders(req,
		"<sip:trunk@gw>",
		"",
		"",
		`SIP;cause=302;text="REFER"`,
		historyinfo.DiversionDeflection,
	)
	if len(req.HistoryInfo) == 0 || len(req.Diversion) == 0 {
		t.Fatalf("expected chains: hi=%d dv=%d", len(req.HistoryInfo), len(req.Diversion))
	}
	if req.Diversion[0].Reason != historyinfo.DiversionDeflection {
		t.Fatalf("diversion reason: %q", req.Diversion[0].Reason)
	}
}

func TestApplyRetargetHeaders_NoAnchorNoOp(t *testing.T) {
	req := &outbound.DialRequest{
		Target: outbound.DialTarget{RequestURI: "sip:agent@10.0.0.5"},
	}
	ApplyRetargetHeaders(req, "", "", "", "r", historyinfo.DiversionUnconditional)
	if len(req.HistoryInfo) != 0 || len(req.Diversion) != 0 {
		t.Fatal("expected no headers without anchor")
	}
}
