package outbound

import (
	"fmt"
	"strings"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/sirupsen/logrus"
)

// CANCEL retransmit timing: RFC 3261 §17.1.2 non-INVITE client
// transaction over unreliable transport. T1=500ms, exponential
// backoff capped at T2=4s, overall Timer F = 64*T1 = 32s.
//
// In practice the 487 Request Terminated should arrive within the
// first 1-2 seconds; we cap retries at 6 attempts (~12s wall-clock)
// to keep the goroutine bounded if the carrier silently drops both
// the CANCEL and any response.
const (
	cancelRetransmitT1       = 500 * time.Millisecond
	cancelRetransmitT2       = 4 * time.Second
	cancelRetransmitAttempts = 6
	// cancelProvisionalGrace is how long we wait for the first 1xx
	// before sending CANCEL anyway. RFC 3261 §9.1 says we MUST wait,
	// but a strictly silent carrier would otherwise hold the agent
	// ringing forever. T1 is the standard "INVITE has reached the
	// destination" assumption window.
	cancelProvisionalGrace = cancelRetransmitT1
)

// buildCANCEL renders a RFC 3261 §9.1 CANCEL request for a pending
// INVITE transaction. The CANCEL MUST:
//
//   - reuse the INVITE's Request-URI / Call-ID / From (with tag) /
//     To (with whatever tag was on the INVITE, i.e. NONE — we never
//     put a to-tag on UAC INVITEs)
//   - carry a single top Via with the SAME branch as the INVITE
//     (this is how the proxy / UAS matches CANCEL to INVITE
//     transaction)
//   - keep the CSeq numeric value identical to the INVITE, but with
//     Method = "CANCEL"
//
// Anything else (extra Via, different branch, bumped CSeq) makes the
// CANCEL look like a fresh request to most stacks and they'll
// silently drop it, leaving the agent's phone ringing.
func buildCANCEL(inv inviteParams) *stack.Message {
	msg := &stack.Message{
		IsRequest:  true,
		Method:     stack.MethodCancel,
		RequestURI: inv.RequestURI,
		Version: stack.SIPVersion,
	}
	msg.SetHeader(stack.HeaderVia, formatVia(inv.ViaTransport, inv.SIPHost, inv.SIPPort, inv.Branch))
	msg.SetHeader(stack.HeaderMaxForwards, stack.DefaultMaxForwards)
	msg.SetHeader(stack.HeaderFrom, formatOutboundFromHeader(inv.FromDisplayName, inv.FromUser, inv.SIPHost, inv.SIPPort, inv.FromTag))
	msg.SetHeader(stack.HeaderTo, formatToHeader(inv.RequestURI))
	msg.SetHeader(stack.HeaderCallID, inv.CallID)
	msg.SetHeader(stack.HeaderCSeq, fmt.Sprintf("%d %s", inv.CSeq, stack.MethodCancel))
	msg.SetHeader(stack.HeaderContentLength, "0")
	return msg
}

// SendCANCEL emits a SIP CANCEL for a not-yet-established outbound
// INVITE leg (transfer-agent ring, campaign abandon, etc). Safe to
// call concurrently with the response handler; idempotent (second
// call is a no-op).
//
// Behavior:
//
//   - if the INVITE has not yet received its first 1xx provisional,
//     the CANCEL is QUEUED (RFC 3261 §9.1) and fires either when 1xx
//     arrives or after a 500ms grace timer, whichever comes first
//   - if 1xx already received, CANCEL goes on the wire immediately
//     and a retransmit goroutine starts (T1=500ms, capped at T2=4s,
//     6 attempts) per RFC 3261 §17.1.2
//   - second call is a no-op (cancelSent CompareAndSwap guard) so
//     the inbound-BYE path and ring-timeout path can both invoke
//     safely
//
// Returns nil on success (queued or sent). Common error reasons:
//
//   - unknown call-id (leg already torn down / never existed)
//   - leg already established (CANCEL is illegal — caller should
//     use SendBYE instead)
func (m *Manager) SendCANCEL(callID string) error {
	if m == nil {
		return fmt.Errorf("sip/outbound: nil manager")
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return fmt.Errorf("sip/outbound: empty call-id")
	}
	leg := m.legByCallIDOrHostRewrite(callID)
	if leg == nil {
		return fmt.Errorf("sip/outbound: unknown call-id %s", callID)
	}
	leg.mu.Lock()
	established := leg.established
	leg.mu.Unlock()
	if established {
		return fmt.Errorf("sip/outbound: leg %s already established; use SendBYE", callID)
	}
	return leg.requestCANCEL()
}

// requestCANCEL is the leg-side entry point. Distinguishes "send
// now" (gotProvisional) from "queue for first 1xx" (RFC 3261 §9.1).
func (leg *outLeg) requestCANCEL() error {
	if leg == nil {
		return fmt.Errorf("sip/outbound: nil leg")
	}
	// Mark intent first so the provisional handler knows to fire
	// even if it races with us right here.
	leg.pendingCancel.Store(true)

	if leg.gotProvisional.Load() {
		return leg.fireDeferredCANCELLocked()
	}

	// No 1xx yet → queue. Schedule a 500ms grace timer that will
	// force-send if no provisional arrives. The goroutine picks
	// whichever path runs first via the cancelSent CAS guard.
	go func() {
		time.Sleep(cancelProvisionalGrace)
		if leg.cancelSent.Load() {
			return
		}
		// Still nothing — force send. The carrier may have dropped
		// our INVITE entirely; either way, sending CANCEL now is
		// the right move (worst case the proxy ignores us).
		logrus.WithFields(logrus.Fields{
			"call_id": leg.params.CallID,
			"grace":   cancelProvisionalGrace,
		}).Info("sip outbound CANCEL: provisional grace expired, forcing send")
		_ = leg.fireDeferredCANCELLocked()
	}()
	logrus.WithFields(logrus.Fields{
		"call_id":        leg.params.CallID,
		"correlation_id": strings.TrimSpace(leg.req.CorrelationID),
	}).Info("sip outbound CANCEL queued (waiting for first 1xx)")
	return nil
}

// fireDeferredCANCEL is the goroutine-friendly entry point used by
// the provisional response handler. It just delegates to the locked
// version and discards errors (already logged inside).
func (leg *outLeg) fireDeferredCANCEL() {
	_ = leg.fireDeferredCANCELLocked()
}

// fireDeferredCANCELLocked actually puts the CANCEL on the wire and
// kicks off the retransmit goroutine. Idempotent via cancelSent CAS.
func (leg *outLeg) fireDeferredCANCELLocked() error {
	if !leg.cancelSent.CompareAndSwap(false, true) {
		// Another path already sent. RFC 3261 explicitly allows CANCEL
		// retransmissions for a transaction, but we de-dup at the
		// "first send" level to avoid log noise.
		return nil
	}
	dst := leg.dst
	if dst == nil {
		return fmt.Errorf("sip/outbound: leg %s has no signaling address", leg.params.CallID)
	}
	msg := buildCANCEL(leg.params)
	if err := leg.sendOnPeer(msg, dst); err != nil {
		return fmt.Errorf("sip/outbound: send CANCEL %s: %w", leg.params.CallID, err)
	}
	logrus.WithFields(logrus.Fields{
		"call_id":        leg.params.CallID,
		"request_uri":    strings.TrimSpace(leg.params.RequestURI),
		"correlation_id": strings.TrimSpace(leg.req.CorrelationID),
		"dst":            dst.String(),
	}).Info("sip outbound CANCEL sent")
	leg.startCANCELRetransmit(msg, dst)
	return nil
}

// startCANCELRetransmit launches the RFC 3261 §17.1.2 retransmit
// goroutine. UDP-only — TCP/TLS retransmits are owned by the OS.
// The goroutine stops when either:
//   - cancelStop channel is closed (final response to INVITE arrived,
//     OR cleanupLeg fires)
//   - cancelRetransmitAttempts retries are exhausted
func (leg *outLeg) startCANCELRetransmit(msg *stack.Message, dst interface{}) {
	if leg == nil || leg.transport != "" && leg.transport != TransportUDP {
		// Reliable transport — kernel + SIP transaction layer in stack/
		// handle retransmit semantics for us. Nothing to do.
		return
	}
	leg.cancelStopMu.Lock()
	if leg.cancelStop == nil {
		leg.cancelStop = make(chan struct{})
	}
	stop := leg.cancelStop
	leg.cancelStopMu.Unlock()

	go func() {
		interval := cancelRetransmitT1
		// First send already happened in fireDeferredCANCELLocked. We
		// retransmit ATTEMPTS-1 more times, doubling each time up to T2.
		for i := 1; i < cancelRetransmitAttempts; i++ {
			t := time.NewTimer(interval)
			select {
			case <-stop:
				t.Stop()
				return
			case <-t.C:
			}
			d := leg.dst
			if d == nil {
				return
			}
			if err := leg.sendOnPeer(buildCANCEL(leg.params), d); err != nil {
				logrus.WithFields(logrus.Fields{
					"call_id": leg.params.CallID,
					"attempt": i + 1,
					"error":   err,
				}).Warn("sip outbound CANCEL retransmit failed")
				return
			}
			logrus.WithFields(logrus.Fields{
				"call_id": leg.params.CallID,
				"attempt": i + 1,
				"after":   interval,
			}).Debug("sip outbound CANCEL retransmit")
			interval *= 2
			if interval > cancelRetransmitT2 {
				interval = cancelRetransmitT2
			}
		}
	}()
}

// stopCANCELRetransmit signals the retransmit goroutine to exit.
// Safe to call multiple times; safe before retransmit ever started.
func (leg *outLeg) stopCANCELRetransmit() {
	if leg == nil {
		return
	}
	leg.cancelStopMu.Lock()
	defer leg.cancelStopMu.Unlock()
	if leg.cancelStop == nil {
		return
	}
	select {
	case <-leg.cancelStop:
		// already closed
	default:
		close(leg.cancelStop)
	}
}

// buildAndSendCANCEL is the legacy entry point kept for
// AbandonEarlyTransferInvite. It delegates to the same gated /
// retransmit-aware path as the public SendCANCEL.
func buildAndSendCANCEL(leg *outLeg) error {
	if leg == nil {
		return fmt.Errorf("sip/outbound: nil leg")
	}
	return leg.requestCANCEL()
}
