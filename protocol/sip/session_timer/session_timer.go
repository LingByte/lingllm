// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Package session_timer implements RFC 4028 Session Timers in SIP. It
// is the protocol "are we still alive?" probe for long calls; without
// it, carrier SBCs and intermediate proxies will eventually BYE us
// mid-stream because they can't tell a hung dialog from a live one.
//
// Coverage in this package:
//
//   - Header parsing: Session-Expires, Min-SE, Supported, Require
//   - Header formatting on the wire
//   - Negotiation logic for both UAS and UAC roles (pure function,
//     trivially unit-testable)
//   - Constants for default intervals + the standard expiry Reason
//
// What lives OUTSIDE this package (in pkg/sip/server and outbound):
//
//   - The watchdog goroutine that actually fires BYE on expiry
//   - The hook from handleReInvite / handleUpdate that bumps the
//     watchdog on a refresh
//   - The 422 retry loop on the UAC side
//
// That split keeps this package free of any net / dialog state, so
// it stays cheap to test and easy to reuse from the eventual TCP /
// TLS / TLS-mTLS transports in batch 2.
//
// Design notes & defaults:
//
//   - DefaultSE = 1800 s (30 min). RFC 4028 §4 recommends ≥ 90 s and
//     suggests 1800 as a "reasonable refresh interval" in §4. Lots of
//     real carriers send exactly 1800.
//   - DefaultMinSE = 90 s. RFC 4028 §4 hard minimum.
//   - We currently choose refresher=uas by default on the inbound
//     leg (i.e. the peer refreshes us). That's intentional: we don't
//     yet send in-dialog UAC requests, so being the refresher would
//     require new machinery. Accepting either side's choice on the
//     wire is fine; we just don't volunteer to refresh.
package session_timer

import (
	"fmt"
	"strconv"
	"strings"
)

// Refresher denotes which side of the dialog owns refresh duty per
// RFC 4028 §4. The empty Refresher means "not negotiated" — accept
// any inbound refresh but don't initiate.
type Refresher string

const (
	// RefresherUAC means the original INVITE sender refreshes.
	RefresherUAC Refresher = "uac"
	// RefresherUAS means the INVITE receiver refreshes.
	RefresherUAS Refresher = "uas"
	// RefresherUnset means no refresher was negotiated. Consumers
	// should treat the dialog as "no timer" — don't arm a watchdog,
	// don't schedule refreshes.
	RefresherUnset Refresher = ""
)

// Defaults — see package doc for rationale.
const (
	DefaultMinSE        = 90
	DefaultSE           = 1800
	HardMaxSE           = 7200 // we refuse SE > 2h to bound goroutine lifetimes; carriers rarely exceed this
	SupportedTokenTimer = "timer"
)

// ReasonExpired is the Reason header value RFC 4028 §10 prescribes
// for the BYE we send when the timer fires.
const ReasonExpired = `SIP;cause=408;text="Session Timer Expired"`

// ParseSessionExpires parses a Session-Expires header value. RFC 4028
// §6 syntax: `delta-seconds [;refresher=uac|uas] [;other-generic-params]`.
//
// Returns:
//   - sec: parsed delta-seconds (0 if missing / unparseable)
//   - refresher: RefresherUAC / RefresherUAS / RefresherUnset
//   - extra: any parameters we didn't recognise, kept verbatim so
//     callers can echo them back unmangled per RFC 4028 §10's "MUST
//     copy"-style requirement for unknown params on responses.
//
// Returns sec=0 when the header is absent or syntactically invalid;
// the caller should treat sec=0 as "peer didn't request a timer".
func ParseSessionExpires(headerValue string) (sec int, refresher Refresher, extra []string) {
	s := strings.TrimSpace(headerValue)
	if s == "" {
		return 0, RefresherUnset, nil
	}
	parts := strings.Split(s, ";")
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || n <= 0 {
		return 0, RefresherUnset, nil
	}
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := ""
		if len(kv) == 2 {
			v = strings.TrimSpace(kv[1])
		}
		switch k {
		case "refresher":
			switch strings.ToLower(v) {
			case "uac":
				refresher = RefresherUAC
			case "uas":
				refresher = RefresherUAS
			default:
				// unknown value → leave Unset
			}
		default:
			extra = append(extra, p)
		}
	}
	return n, refresher, extra
}

// ParseMinSE parses a Min-SE header. RFC 4028 §5: just delta-seconds
// plus optional generic params. We ignore the params (no standard
// param has semantic content for Min-SE at writing time).
func ParseMinSE(headerValue string) int {
	s := strings.TrimSpace(headerValue)
	if s == "" {
		return 0
	}
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ParseTokenList splits an option-tag list header (Supported / Require
// / Allow) into lowercased trimmed tokens. RFC 3261 §7.3.1 allows the
// header to appear multiple times; sip.Message.GetHeader joins with
// "\r\n" — treat that as a comma here.
func ParseTokenList(headerValue string) []string {
	s := strings.TrimSpace(headerValue)
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", ",")
	var out []string
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(strings.ToLower(tok))
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// HasToken returns true if list contains tok (case-insensitive,
// already-lowercased input expected from ParseTokenList).
func HasToken(list []string, tok string) bool {
	tok = strings.ToLower(strings.TrimSpace(tok))
	for _, t := range list {
		if t == tok {
			return true
		}
	}
	return false
}

// FormatSessionExpires builds the wire form. refresher=Unset omits
// the refresher param entirely.
func FormatSessionExpires(sec int, refresher Refresher) string {
	if sec <= 0 {
		return ""
	}
	out := strconv.Itoa(sec)
	switch refresher {
	case RefresherUAC, RefresherUAS:
		out += ";refresher=" + string(refresher)
	}
	return out
}

// FormatMinSE renders Min-SE on the wire (just the integer).
func FormatMinSE(sec int) string {
	if sec <= 0 {
		return ""
	}
	return strconv.Itoa(sec)
}

// Decision is the output of negotiation: tells the caller whether to
// accept the call, what headers to emit, and what timer state to arm.
type Decision struct {
	// Reject422 = true means: send 422 Session Interval Too Small,
	// include Min-SE = MinSE in the response. The caller must NOT
	// proceed to set up the call.
	Reject422 bool

	// ChosenSE is the negotiated session expiration in seconds. 0
	// means "no timer in effect" — the caller should not arm a
	// watchdog or schedule refreshes.
	ChosenSE int

	// Refresher is the chosen refresh-owner. RefresherUnset when no
	// timer is in effect.
	Refresher Refresher

	// MinSE is what we advertise as our Min-SE on the response (UAS)
	// or on the next INVITE attempt (UAC after 422).
	MinSE int

	// RequireTimer signals the caller to put `timer` in a Require
	// header on the response. Only set when the peer included `timer`
	// in their Require header — RFC 4028 §7.2.
	RequireTimer bool

	// SupportedTimer indicates the response should include `timer` in
	// its Supported header. Always true when we accept a timer; lets
	// peers detect our capability for in-dialog refreshes via UPDATE.
	SupportedTimer bool
}

// NegotiateUAS runs the inbound (UAS) negotiation per RFC 4028 §9.
//
// Inputs are the relevant parsed fields from the inbound INVITE:
//
//   - peerSE: parsed Session-Expires value (0 if header absent)
//   - peerRefresher: parsed refresher param from Session-Expires
//   - peerMinSE: parsed Min-SE value (0 if header absent — treat as 90 per RFC)
//   - peerSupportsTimer: peer's Supported header contains "timer"
//   - peerRequiresTimer: peer's Require header contains "timer"
//   - localMinSE: our locally-configured floor (typically 90)
//   - localPreferredSE: what we'd choose if peer left it open (typically 1800)
//
// Behaviour:
//
//   - peerSE == 0 + !peerRequiresTimer + !peerSupportsTimer:
//     legacy peer; no timer. Decision has zeros.
//   - peerSE == 0 + peerSupportsTimer (or peerRequiresTimer): peer
//     offered timer-capability but no specific SE. We unilaterally
//     set SE = localPreferredSE, refresher = uas (we ask peer to
//     refresh us). This matches what most softswitches do.
//   - peerSE > 0 && peerSE < localMinSE: send 422 with Min-SE.
//   - peerSE > 0 && peerSE >= localMinSE: accept. Choose refresher:
//     prefer the peer's value if specified; else default to uas.
//     Cap at HardMaxSE for our own goroutine hygiene.
func NegotiateUAS(
	peerSE int,
	peerRefresher Refresher,
	peerMinSE int,
	peerSupportsTimer, peerRequiresTimer bool,
	localMinSE, localPreferredSE int,
) Decision {
	if localMinSE <= 0 {
		localMinSE = DefaultMinSE
	}
	if localPreferredSE <= 0 {
		localPreferredSE = DefaultSE
	}
	if localPreferredSE > HardMaxSE {
		localPreferredSE = HardMaxSE
	}

	// Case A: peer offered nothing related to timers.
	if peerSE == 0 && !peerSupportsTimer && !peerRequiresTimer {
		return Decision{} // no timer
	}

	// Case B: 422 — peer's SE below our Min-SE.
	if peerSE > 0 && peerSE < localMinSE {
		return Decision{
			Reject422: true,
			MinSE:     localMinSE,
		}
	}

	// Case C: accept. Determine ChosenSE.
	chosenSE := peerSE
	if chosenSE == 0 {
		// Peer indicated timer capability but no SE; we propose.
		chosenSE = localPreferredSE
	}
	if chosenSE > HardMaxSE {
		chosenSE = HardMaxSE
	}

	// Determine refresher. Peer's preference wins per RFC 4028 §7.1:
	// if peer specified refresher, the UAS MUST honour it.
	refresher := peerRefresher
	if refresher == RefresherUnset {
		// RFC 4028 §7.1 + §9: when peer didn't specify, UAS chooses.
		// We choose uas → peer is responsible for refreshing us. This
		// avoids us having to send in-dialog re-INVITE/UPDATE before
		// that machinery exists.
		refresher = RefresherUAS
	}

	return Decision{
		ChosenSE:       chosenSE,
		Refresher:      refresher,
		MinSE:          localMinSE,
		RequireTimer:   peerRequiresTimer,
		SupportedTimer: true,
	}
}

// IsActive returns whether this decision should drive a watchdog
// timer / refresh scheduler.
func (d Decision) IsActive() bool {
	return d.ChosenSE > 0 && d.Refresher != RefresherUnset
}

// WatchdogInterval is how long the local side waits before declaring
// the session dead and sending BYE. Per RFC 4028 §10, the refreshee
// should declare the session timed out at exactly the Session-Expires
// interval (not halfway like the refresher does).
func (d Decision) WatchdogInterval() int {
	return d.ChosenSE
}

// RefresherWindow returns when the refresher (us, if Refresher==local
// role) should send the next refresh. RFC 4028 §10: the refresher
// sends at half the interval, rounded down. We don't yet use this in
// the UAS-only refreshee implementation but expose it for the future
// UAC refresher path.
func (d Decision) RefresherWindow() int {
	if d.ChosenSE <= 0 {
		return 0
	}
	return d.ChosenSE / 2
}

// String for log lines.
func (d Decision) String() string {
	if d.Reject422 {
		return fmt.Sprintf("session_timer{reject422 min_se=%d}", d.MinSE)
	}
	if !d.IsActive() {
		return "session_timer{disabled}"
	}
	return fmt.Sprintf("session_timer{se=%d refresher=%s min_se=%d require=%v}",
		d.ChosenSE, d.Refresher, d.MinSE, d.RequireTimer)
}
