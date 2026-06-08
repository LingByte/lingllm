package outbound

import (
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

// buildACK builds a SIP ACK for a completed INVITE transaction (200 OK with SDP answer).
func buildACK(inv inviteParams, resp200 *stack.Message, requestURI string) *stack.Message {
	if resp200 == nil {
		return nil
	}
	reqURI := strings.TrimSpace(requestURI)
	if reqURI == "" {
		reqURI = inv.RequestURI
	}

	msg := &stack.Message{
		IsRequest:  true,
		Method:     stack.MethodAck,
		RequestURI: reqURI,
		Version:    "SIP/2.0",
	}
	// Via must match INVITE (single Via for our client) including
	// the same transport token (RFC 3261 §17.1.1.3).
	msg.SetHeader("Via", formatVia(inv.ViaTransport, inv.SIPHost, inv.SIPPort, inv.Branch))
	msg.SetHeader("Max-Forwards", "70")

	msg.SetHeader("From", formatOutboundFromHeader(inv.FromDisplayName, inv.FromUser, inv.SIPHost, inv.SIPPort, inv.FromTag))
	if to := resp200.GetHeader("To"); to != "" {
		msg.SetHeader("To", to)
	} else {
		msg.SetHeader("To", inv.RequestURI)
	}
	msg.SetHeader("Call-ID", inv.CallID)
	msg.SetHeader("CSeq", stack.WithCSeqACK(inv.CSeq))
	msg.SetHeader("Content-Length", "0")
	return msg
}

// ackRequestURI prefers Contact from 200 OK (RFC 3261).
func ackRequestURI(resp200 *stack.Message, fallback string) string {
	if resp200 == nil {
		return fallback
	}
	c := strings.TrimSpace(resp200.GetHeader("Contact"))
	if c == "" {
		return fallback
	}
	c = strings.TrimPrefix(c, "<")
	c = strings.TrimSuffix(c, ">")
	if idx := strings.Index(c, ";"); idx > 0 {
		c = c[:idx]
	}
	c = strings.TrimSpace(c)
	if strings.HasPrefix(strings.ToLower(c), "sip:") {
		return c
	}
	return fallback
}
