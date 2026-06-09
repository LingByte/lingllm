package outbound

import (
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/session_timer"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func makeRefreshLeg() *outLeg {
	return &outLeg{
		params: inviteParams{
			CallID: "leg-1@example.com", FromUser: "alice", FromTag: "a-tag",
			SIPHost: "127.0.0.1", SIPPort: 5060,
			RequestURI: "sip:bob@127.0.0.1", CSeq: 1,
		},
		byeToHeader:   "<sip:bob@127.0.0.1>;tag=b-tag",
		byeRequestURI: "sip:bob@127.0.0.1",
		byeCSeqNext:   2,
	}
}

func TestBuildUPDATE_HeadersAndShape(t *testing.T) {
	leg := makeRefreshLeg()
	msg := buildUPDATE(leg.params, leg.byeToHeader, leg.byeRequestURI,
		leg.byeCSeqNext, "deadbeef", 1800, 90)
	if !msg.IsRequest || msg.Method != stack.MethodUpdate {
		t.Fatalf("not UPDATE: %+v", msg)
	}
	if got := msg.GetHeader(stack.HeaderCSeq); got != "2 UPDATE" {
		t.Errorf("CSeq=%q", got)
	}
	se := msg.GetHeader(stack.HeaderSessionExpires)
	if !strings.Contains(se, "1800") || !strings.Contains(se, "refresher=uac") {
		t.Errorf("Session-Expires=%q", se)
	}
}

func TestStartRefresherIfUAC_OnlyArmsWhenAssignedUAC(t *testing.T) {
	cases := []struct {
		name      string
		se        int
		role      session_timer.Refresher
		wantArmed bool
	}{
		{"uac", 1800, session_timer.RefresherUAC, true},
		{"uas", 1800, session_timer.RefresherUAS, false},
		{"sub-90", 30, session_timer.RefresherUAC, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leg := makeRefreshLeg()
			leg.startRefresherIfUAC(tc.se, tc.role)
			leg.refreshMu.Lock()
			armed := leg.refresher != nil
			r := leg.refresher
			leg.refreshMu.Unlock()
			if armed != tc.wantArmed {
				t.Fatalf("armed=%v", armed)
			}
			if r != nil {
				r.stop()
			}
		})
	}
}

func TestHandleUPDATEResponse_200OK_AdoptsShorterSE(t *testing.T) {
	leg := makeRefreshLeg()
	leg.startRefresherIfUAC(1800, session_timer.RefresherUAC)
	r := leg.refresher
	defer r.stop()

	resp := &stack.Message{IsRequest: false, StatusCode: 200, StatusText: "OK"}
	resp.SetHeader(stack.HeaderSessionExpires, "900;refresher=uac")
	if !r.handleUPDATEResponse(resp) {
		t.Fatal("200 should keep armed")
	}
	r.mu.Lock()
	got := r.se
	r.mu.Unlock()
	if got != 900 {
		t.Errorf("se=%d", got)
	}
}

func TestHandleUPDATEResponse_422_BumpsSE(t *testing.T) {
	leg := makeRefreshLeg()
	leg.startRefresherIfUAC(120, session_timer.RefresherUAC)
	r := leg.refresher
	defer r.stop()

	resp := &stack.Message{IsRequest: false, StatusCode: 422, StatusText: "Session Interval Too Small"}
	resp.SetHeader(stack.HeaderMinSE, "1800")
	if !r.handleUPDATEResponse(resp) {
		t.Fatal("first 422 recoverable")
	}
	r.mu.Lock()
	gotSE := r.se
	r.mu.Unlock()
	if gotSE < 1800 {
		t.Errorf("se=%d", gotSE)
	}
}

func TestHandleUPDATEResponse_481Stops(t *testing.T) {
	leg := makeRefreshLeg()
	leg.startRefresherIfUAC(1800, session_timer.RefresherUAC)
	r := leg.refresher
	defer r.stop()

	resp := &stack.Message{IsRequest: false, StatusCode: 481}
	if r.handleUPDATEResponse(resp) {
		t.Fatal("481 stops refresher")
	}
}

func TestStopRefresher_Idempotent(t *testing.T) {
	leg := makeRefreshLeg()
	leg.startRefresherIfUAC(1800, session_timer.RefresherUAC)
	leg.stopRefresher()
	leg.stopRefresher()
	if leg.refresher != nil {
		t.Error("cleared")
	}
}
