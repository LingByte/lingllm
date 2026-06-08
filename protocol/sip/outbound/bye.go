package outbound

import (
	"fmt"
	"net"
	"strings"

	sipMetrics "github.com/LingByte/lingllm/protocol/sip/metrics"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func buildBYE(inv inviteParams, toHeader200, requestURI string, cseq int, branch string) *stack.Message {
	reqURI := strings.TrimSpace(requestURI)
	if reqURI == "" {
		reqURI = inv.RequestURI
	}
	msg := &stack.Message{
		IsRequest:  true,
		Method:     stack.MethodBye,
		RequestURI: reqURI,
		Version:    "SIP/2.0",
	}
	msg.SetHeader("Via", formatVia(inv.ViaTransport, inv.SIPHost, inv.SIPPort, branch))
	msg.SetHeader("Max-Forwards", "70")
	msg.SetHeader("From", formatOutboundFromHeader(inv.FromDisplayName, inv.FromUser, inv.SIPHost, inv.SIPPort, inv.FromTag))
	if strings.TrimSpace(toHeader200) != "" {
		msg.SetHeader("To", toHeader200)
	} else {
		msg.SetHeader("To", formatToHeader(inv.RequestURI))
	}
	msg.SetHeader("Call-ID", inv.CallID)
	msg.SetHeader("CSeq", fmt.Sprintf("%d BYE", cseq))
	msg.SetHeader("Content-Length", "0")
	return msg
}

// SendBYE sends an in-dialog BYE for an established outbound leg (after 200 OK to INVITE).
func (m *Manager) SendBYE(callID string) error {
	if m == nil || m.send == nil {
		return fmt.Errorf("sip/outbound: manager not ready")
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return fmt.Errorf("sip/outbound: empty call-id")
	}
	leg := m.legByCallIDOrHostRewrite(callID)
	if leg == nil {
		return fmt.Errorf("sip/outbound: unknown call-id %s", callID)
	}
	leg.sigMu.Lock()
	defer leg.sigMu.Unlock()
	if strings.TrimSpace(leg.byeToHeader) == "" {
		return fmt.Errorf("sip/outbound: dialog not ready for BYE")
	}
	dst := leg.byeRemote
	if dst == nil {
		dst = leg.dst
	}
	if dst == nil {
		return fmt.Errorf("sip/outbound: no signaling address for BYE")
	}
	cseq := leg.byeCSeqNext
	if cseq <= 0 {
		cseq = leg.params.CSeq + 1
	}
	leg.byeCSeqNext = cseq + 1
	branch := randomHex(10)
	msg := buildBYE(leg.params, leg.byeToHeader, leg.byeRequestURI, cseq, branch)
	// Reuse the same signaling peer as the INVITE so TCP/TLS in-
	// dialog requests stay on the same connection per RFC 5923. Fall
	// back to UDP via the shared sender when no peer is bound (e.g.
	// the leg was constructed by older code that didn't set one).
	return leg.sendOnPeer(msg, dst)
}

// CleanupLegIfPresent removes outbound leg state when
// no longer needed, e.g. after remote BYE. reasonClass is the
// bounded enum extracted from the peer's RFC 3326 Reason header
// (empty string → default "normal"). Both the BYE counter and the
// CDR hangup_reason inherit this value.
func (m *Manager) CleanupLegIfPresent(callID, reasonClass string) {
	if m == nil {
		return
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	leg := m.legByCallIDOrHostRewrite(callID)
	if leg == nil {
		return
	}
	if reasonClass == "" {
		reasonClass = sipMetrics.ByeReasonNormal
	}
	// Remote-initiated BYE. Tag the metric with the parsed reason
	// class so dashboards can split normal hang-ups from carrier
	// errors or session-timer expiry.
	sipMetrics.Bye(sipMetrics.ByeByRemote, reasonClass)
	leg.cleanupLeg()
}

func cloneUDPAddr(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	b := *a
	return &b
}
