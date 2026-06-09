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
		Version: stack.SIPVersion,
	}
	// Via must match INVITE (single Via for our client) including
	// the same transport token (RFC 3261 §17.1.1.3).
	msg.SetHeader(stack.HeaderVia, formatVia(inv.ViaTransport, inv.SIPHost, inv.SIPPort, inv.Branch))
	msg.SetHeader(stack.HeaderMaxForwards, stack.DefaultMaxForwards)

	msg.SetHeader(stack.HeaderFrom, formatOutboundFromHeader(inv.FromDisplayName, inv.FromUser, inv.SIPHost, inv.SIPPort, inv.FromTag))
	if to := resp200.GetHeader(stack.HeaderTo); to != "" {
		msg.SetHeader(stack.HeaderTo, to)
	} else {
		msg.SetHeader(stack.HeaderTo, inv.RequestURI)
	}
	msg.SetHeader(stack.HeaderCallID, inv.CallID)
	msg.SetHeader(stack.HeaderCSeq, stack.WithCSeqACK(inv.CSeq))
	msg.SetHeader(stack.HeaderContentLength, "0")
	return msg
}

// ackRequestURI prefers Contact from 200 OK (RFC 3261).
func ackRequestURI(resp200 *stack.Message, fallback string) string {
	if resp200 == nil {
		return fallback
	}
	c := strings.TrimSpace(resp200.GetHeader(stack.HeaderContact))
	if c == "" {
		return fallback
	}
	c = strings.TrimPrefix(c, "<")
	if idx := strings.Index(c, ">"); idx >= 0 {
		c = c[:idx]
	}
	if idx := strings.Index(c, ";"); idx > 0 {
		c = c[:idx]
	}
	c = strings.TrimSpace(c)
	if strings.HasPrefix(strings.ToLower(c), "sip:") {
		return c
	}
	return fallback
}
