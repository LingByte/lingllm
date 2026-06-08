// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package outbound

// UAC-side RFC 4028 Session-Timer refresher.
//
// Scope on this side of the dialog is intentionally narrow:
//
//   - We DON'T propose Session-Expires in the outbound INVITE — see
//     buildINVITE comments. We only start a refresh loop when the 2xx
//     OK explicitly assigns `refresher=uac` to us. That means peer
//     policy is in charge of *whether* we refresh; we just honour it.
//   - We DON'T attempt to be refreshee (watchdog + BYE on no peer
//     refresh). The outbound `connPeer` currently drops mid-dialog
//     requests (see peer.go:223), so we cannot reliably observe the
//     peer's refresh UPDATEs and would BYE healthy calls.
//   - We DO refresh via UPDATE (RFC 3311) rather than re-INVITE.
//     UPDATE is the right tool when no offer/answer change is needed
//     (RFC 4028 §6 second paragraph), and avoids the SDP renegotiation
//     surface that a re-INVITE would expose.
//   - We DO handle 422 Session Interval Too Small on our refresh
//     UPDATE by bumping our local SE to peer's Min-SE and retrying
//     once (RFC 4028 §6).
//   - We DO stop on 481 (dialog gone), on transport BYE cleanup, and
//     on Manager.Stop / cleanupLeg.

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	sipMetrics "github.com/LingByte/lingllm/protocol/sip/metrics"
	"github.com/LingByte/lingllm/protocol/sip/session_timer"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/sirupsen/logrus"
)

// outRefresher tracks the UAC-side refresh loop for one outbound
// dialog. All public methods are safe to call from any goroutine; a
// concurrent stop() racing with a sendUPDATE() will not double-close
// the stop channel.
type outRefresher struct {
	leg *outLeg

	mu      sync.Mutex
	se      int // negotiated Session-Expires seconds
	minSE   int // our floor; bumped on 422
	stopped bool
	stopCh  chan struct{}
	// retried422 prevents a stuck 422→retry→422 loop on misbehaving
	// peers. After one bump-and-retry we give up the timer rather
	// than keep flapping. RFC 4028 §6 doesn't mandate a hard cap;
	// this is operational hygiene.
	retried422 bool
}

// startRefresherIfUAC arms the refresher iff peer assigned
// refresher=uac in the 2xx answer with a usable SE. Idempotent: a
// second call (e.g. from a UAC-side reINVITE re-negotiation later)
// silently leaves the existing refresher in place.
func (leg *outLeg) startRefresherIfUAC(se int, refresher session_timer.Refresher) {
	if leg == nil {
		return
	}
	if refresher != session_timer.RefresherUAC {
		return
	}
	if se < session_timer.DefaultMinSE {
		// Peer gave us a refresh interval below the spec floor. Don't
		// run a hot loop. RFC 4028 §6 says SE MUST NOT be < Min-SE;
		// we treat anything sub-90s as malformed.
		logrus.WithFields(logrus.Fields{"call_id": leg.params.CallID, "se": se}).Warn("sip outbound refresher rejected sub-90s SE")
		return
	}
	r := &outRefresher{
		leg:    leg,
		se:     se,
		minSE:  session_timer.DefaultMinSE,
		stopCh: make(chan struct{}),
	}
	leg.refreshMu.Lock()
	if leg.refresher != nil {
		leg.refreshMu.Unlock()
		return
	}
	leg.refresher = r
	leg.refreshMu.Unlock()

	logrus.WithFields(logrus.Fields{"call_id": leg.params.CallID, "se": se, "role": "uac"}).Info("sip outbound session-timer refresher armed")
	go r.run()
}

// stopRefresher tears down the loop (idempotent, lock-safe). Called
// from cleanupLeg and from inside the loop itself on terminal errors.
func (leg *outLeg) stopRefresher() {
	if leg == nil {
		return
	}
	leg.refreshMu.Lock()
	r := leg.refresher
	leg.refresher = nil
	leg.refreshMu.Unlock()
	if r != nil {
		r.stop()
	}
}

func (r *outRefresher) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.stopped = true
	close(r.stopCh)
}

// run sleeps half the negotiated SE (RFC 4028 §10: refresher sends at
// SE/2) then fires an UPDATE. Loops until stopped. The SE/2 figure is
// recomputed each iteration because a 422 may have bumped r.se.
func (r *outRefresher) run() {
	for {
		r.mu.Lock()
		se := r.se
		r.mu.Unlock()

		// Half-interval, clamped to ≥ 30s to avoid pathological refresh
		// pressure if peer downgrades to the 90s floor.
		half := time.Duration(se/2) * time.Second
		if half < 30*time.Second {
			half = 30 * time.Second
		}
		t := time.NewTimer(half)
		select {
		case <-r.stopCh:
			t.Stop()
			return
		case <-t.C:
		}

		if !r.sendUPDATE() {
			return
		}
	}
}

// sendUPDATE emits one refresh. Returns false on terminal failure
// (dialog gone / leg cleaned up). Single-send transport hiccups
// return true so the next tick gets another shot.
func (r *outRefresher) sendUPDATE() bool {
	leg := r.leg
	if leg == nil {
		return false
	}

	// Pull the mid-dialog state under sigMu so we don't race the
	// BYE path (which reads/writes the same byeCSeqNext counter).
	leg.sigMu.Lock()
	if strings.TrimSpace(leg.byeToHeader) == "" {
		leg.sigMu.Unlock()
		return false
	}
	cseq := leg.byeCSeqNext
	if cseq <= 0 {
		cseq = leg.params.CSeq + 1
	}
	leg.byeCSeqNext = cseq + 1
	dst := leg.byeRemote
	if dst == nil {
		dst = leg.dst
	}
	toH := leg.byeToHeader
	reqURI := leg.byeRequestURI
	leg.sigMu.Unlock()

	r.mu.Lock()
	se := r.se
	minSE := r.minSE
	r.mu.Unlock()

	branch := randomHex(10)
	msg := buildUPDATE(leg.params, toH, reqURI, cseq, branch, se, minSE)
	if err := leg.sendOnPeer(msg, dst); err != nil {
		logrus.WithFields(logrus.Fields{"call_id": leg.params.CallID, "cseq": cseq, "error": err}).Warn("sip outbound refresh UPDATE send failed")
		// Stay armed — a single send error (peer momentarily
		// unreachable) shouldn't kill the timer.
		return true
	}
	logrus.WithFields(logrus.Fields{"call_id": leg.params.CallID, "cseq": cseq, "se": se}).Debug("sip outbound refresh UPDATE sent")
	return true
}

// handleUPDATEResponse processes responses to our refresh UPDATEs.
// Returns true when we accepted the result (200 OK or recoverable
// 422); false on terminal failure (481 dialog gone — caller stops).
func (r *outRefresher) handleUPDATEResponse(resp *stack.Message) bool {
	if r == nil || resp == nil {
		return true
	}
	st := resp.StatusCode
	switch {
	case st >= 200 && st < 300:
		// Peer may shorten SE in the 200 OK (RFC 4028 §7.1: response
		// SE ≤ request SE). Adopt their value so we refresh sooner
		// than we asked, never later.
		if peerSE, peerRefresher, _ := session_timer.ParseSessionExpires(resp.GetHeader("Session-Expires")); peerSE > 0 {
			r.mu.Lock()
			if peerSE < r.se {
				r.se = peerSE
			}
			r.mu.Unlock()
			// Refresher swap: if peer flipped the role to uas in the
			// response, we MUST stop refreshing (they own it now).
			if peerRefresher == session_timer.RefresherUAS {
				sipMetrics.SessionTimerRefresh(sipMetrics.RefreshRoleSwappedToUAS)
				logrus.WithField("call_id", r.leg.params.CallID).Info("sip outbound refresher role swapped to uas by peer 200 OK; stopping")
				return false
			}
		}
		sipMetrics.SessionTimerRefresh(sipMetrics.RefreshResultOK)
		return true

	case st == 422:
		// Session Interval Too Small. Peer's Min-SE tells us the
		// floor. RFC 4028 §6: retry with SE ≥ peer's Min-SE.
		peerMin := session_timer.ParseMinSE(resp.GetHeader("Min-SE"))
		if peerMin <= 0 || peerMin > session_timer.HardMaxSE {
			sipMetrics.SessionTimerRefresh(sipMetrics.Refresh422GaveUp)
			logrus.WithFields(logrus.Fields{"call_id": r.leg.params.CallID, "peer_min_se": peerMin}).Warn("sip outbound refresh got 422 without usable Min-SE; stopping")
			return false
		}
		r.mu.Lock()
		if r.retried422 {
			r.mu.Unlock()
			sipMetrics.SessionTimerRefresh(sipMetrics.Refresh422GaveUp)
			logrus.WithField("call_id", r.leg.params.CallID).Warn("sip outbound refresh 422 again after retry; stopping refresher")
			return false
		}
		r.retried422 = true
		sipMetrics.SessionTimerRefresh(sipMetrics.Refresh422Bumped)
		if peerMin > r.minSE {
			r.minSE = peerMin
		}
		if r.se < peerMin {
			r.se = peerMin
		}
		newSE := r.se
		r.mu.Unlock()
		logrus.WithFields(logrus.Fields{
			"call_id":     r.leg.params.CallID,
			"new_se":      newSE,
			"peer_min_se": peerMin,
		}).Info("sip outbound refresh bumped SE after 422")
		go func() { r.sendUPDATE() }()
		return true

	case st == 481:
		// Call/Transaction Does Not Exist — dialog is gone (peer
		// crashed / SBC dropped state). No point refreshing.
		sipMetrics.SessionTimerRefresh(sipMetrics.Refresh481DialogGone)
		logrus.WithField("call_id", r.leg.params.CallID).Warn("sip outbound refresh 481; stopping refresher")
		return false

	case st >= 400:
		// Other 4xx/5xx/6xx on a refresh UPDATE: per RFC 4028 §7.4
		// the dialog stays alive on UPDATE failure other than 481;
		// the peer will eventually time us out and BYE if it cares.
		// We keep the loop running so a transient SBC error doesn't
		// kill the call's keepalive entirely.
		logrus.WithFields(logrus.Fields{
			"call_id": r.leg.params.CallID,
			"status":  st,
			"reason":  strings.TrimSpace(resp.StatusText),
		}).Warn("sip outbound refresh non-2xx response")
		return true
	}
	return true
}

// buildUPDATE constructs an in-dialog UPDATE that ONLY refreshes the
// session timer — no SDP body, no offer/answer renegotiation. This
// is the simplest valid refresh per RFC 3311 §5.1 + RFC 4028 §6.
//
// Headers we MUST emit:
//   - Session-Expires: <se>;refresher=uac     (we own the refresh)
//   - Min-SE: <minSE>                         (our floor for retries)
//   - Supported: timer                        (RFC 4028 §3)
//
// We intentionally don't set a Require header — that would force the
// peer to error if it doesn't support timer. RFC 4028 §3 says the
// refresher MUST include `timer` in Supported; Require is optional
// and we prefer the soft-fail behavior on legacy peers.
func buildUPDATE(inv inviteParams, toHeader200, requestURI string,
	cseq int, branch string, se, minSE int) *stack.Message {
	reqURI := strings.TrimSpace(requestURI)
	if reqURI == "" {
		reqURI = inv.RequestURI
	}
	msg := &stack.Message{
		IsRequest:  true,
		Method:     stack.MethodUpdate,
		RequestURI: reqURI,
		Version:    "SIP/2.0",
	}
	msg.SetHeader("Via", formatVia(inv.ViaTransport, inv.SIPHost, inv.SIPPort, branch))
	msg.SetHeader("Max-Forwards", "70")
	msg.SetHeader("From", formatOutboundFromHeader(inv.FromDisplayName, inv.FromUser,
		inv.SIPHost, inv.SIPPort, inv.FromTag))
	if strings.TrimSpace(toHeader200) != "" {
		msg.SetHeader("To", toHeader200)
	} else {
		msg.SetHeader("To", formatToHeader(inv.RequestURI))
	}
	msg.SetHeader("Call-ID", inv.CallID)
	msg.SetHeader("CSeq", fmt.Sprintf("%d %s", cseq, stack.MethodUpdate))
	msg.SetHeader("Contact", formatOutboundContact(inv.FromUser, inv.SIPHost, inv.SIPPort))
	msg.SetHeader("Allow", "INVITE, ACK, BYE, CANCEL, OPTIONS, UPDATE")
	msg.SetHeader("Supported", "timer, 100rel, replaces")
	msg.SetHeader("Session-Expires",
		session_timer.FormatSessionExpires(se, session_timer.RefresherUAC))
	msg.SetHeader("Min-SE", strconv.Itoa(minSE))
	msg.SetHeader("User-Agent", "SoulNexus-SIP/1.0")
	msg.SetHeader("Content-Length", "0")
	return msg
}
