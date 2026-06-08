// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Package metrics is the SIP-signaling-layer observability surface.
//
// Scope: counters and histograms covering the protocol behaviour
// itself (INVITE rate, response classes, transaction timeouts,
// session-timer refresh outcomes, STIR/DTLS handshake results, RTCP
// per-call QoS roll-up). The package never holds per-call state and
// every call site is O(1).
//
// Cardinality discipline:
//
//   - Label keys come from a small enum set (direction, scenario,
//     code_class, method, result, reason_class). Per-call identifiers
//     (Call-ID, phone, SSRC) are NEVER labelled — those belong in
//     the CDR record, not the metrics registry.
//   - Each metric declares its allowed keys via RegisterLabels in
//     init(); the underlying metrics package enforces this softly.
//
// Hot-path discipline:
//
//   - All exported helpers take only enums/integers and look up the
//     pre-allocated label map. Zero string formatting at the call
//     site, zero allocation in the steady state.
package metrics

import (
	"strconv"

	metrics "github.com/LingByte/lingllm/protocol/sip/observability"
)

// Metric names. Single source of truth so dashboards can grep.
const (
	// INVITE responses, classified by direction and response class.
	// One series per (direction, class) — bounded to 2 × 6 = 12 series.
	MetricInviteResultTotal = "sip_invite_result_total"

	// BYE events, classified by who initiated and reason class.
	MetricByeTotal = "sip_bye_total"

	// Transaction-level timeouts (timer F / B fired, RFC 3261).
	MetricTransactionTimeoutTotal = "sip_transaction_timeout_total"

	// RFC 4028 session-timer refresher events. result = ok / 422 /
	// 481 / role-swap / gave-up.
	MetricSessionTimerRefreshTotal = "sip_session_timer_refresh_total"

	// DTLS-SRTP handshake outcomes. result = ok / fail / timeout /
	// fingerprint-mismatch.
	MetricDTLSHandshakeTotal = "sip_dtls_handshake_total"

	// RFC 8224 STIR verification outcomes.
	MetricSTIRVerifyTotal = "sip_stir_verify_total"

	// RTCP-derived per-call QoS roll-ups, recorded ONCE at call end.
	MetricCallRTTMs        = "sip_call_rtt_ms"
	MetricCallJitterMs     = "sip_call_jitter_ms"
	MetricCallLossFraction = "sip_call_loss_fraction"
	MetricCallMOSEstimate  = "sip_call_mos_estimate"
)

// Direction enum.
const (
	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"
)

// Pre-registration of cardinality whitelists. Done in init() so the
// soft defense kicks in even if no producer calls a helper before
// the first scrape.
func init() {
	metrics.RegisterLabels(MetricInviteResultTotal, "direction", "class")
	metrics.RegisterLabels(MetricByeTotal, "direction", "by", "reason_class")
	metrics.RegisterLabels(MetricTransactionTimeoutTotal, "method")
	metrics.RegisterLabels(MetricSessionTimerRefreshTotal, "result")
	metrics.RegisterLabels(MetricDTLSHandshakeTotal, "result")
	metrics.RegisterLabels(MetricSTIRVerifyTotal, "result")
	// Histograms don't have labels in our package today.
	metrics.RegisterLabels(MetricCallRTTMs)
	metrics.RegisterLabels(MetricCallJitterMs)
	metrics.RegisterLabels(MetricCallLossFraction)
	metrics.RegisterLabels(MetricCallMOSEstimate)
}

// ---- Pre-allocated label maps ----

// Layout: maps are keyed by the underlying value so a single
// `switch + return` resolves them with no allocation.

var (
	labelsInviteInbound1xx  = map[string]string{"direction": "inbound", "class": "1xx"}
	labelsInviteInbound2xx  = map[string]string{"direction": "inbound", "class": "2xx"}
	labelsInviteInbound3xx  = map[string]string{"direction": "inbound", "class": "3xx"}
	labelsInviteInbound4xx  = map[string]string{"direction": "inbound", "class": "4xx"}
	labelsInviteInbound5xx  = map[string]string{"direction": "inbound", "class": "5xx"}
	labelsInviteInbound6xx  = map[string]string{"direction": "inbound", "class": "6xx"}
	labelsInviteOutbound1xx = map[string]string{"direction": "outbound", "class": "1xx"}
	labelsInviteOutbound2xx = map[string]string{"direction": "outbound", "class": "2xx"}
	labelsInviteOutbound3xx = map[string]string{"direction": "outbound", "class": "3xx"}
	labelsInviteOutbound4xx = map[string]string{"direction": "outbound", "class": "4xx"}
	labelsInviteOutbound5xx = map[string]string{"direction": "outbound", "class": "5xx"}
	labelsInviteOutbound6xx = map[string]string{"direction": "outbound", "class": "6xx"}
)

// InviteResult bumps the INVITE result counter. `code` is the SIP
// status code (100..699); it's classified to its hundreds class so
// the label cardinality stays bounded at 6 per direction.
func InviteResult(direction string, code int) {
	if code < 100 || code > 699 {
		return
	}
	labels := inviteLabels(direction, code)
	metrics.Default.IncCounter(MetricInviteResultTotal,
		"INVITE results by direction and response class (1xx..6xx)",
		labels)
}

// inviteLabels is split out so the hot path is a switch, not a map
// literal allocation.
func inviteLabels(direction string, code int) map[string]string {
	class := code / 100
	if direction == DirectionOutbound {
		switch class {
		case 1:
			return labelsInviteOutbound1xx
		case 2:
			return labelsInviteOutbound2xx
		case 3:
			return labelsInviteOutbound3xx
		case 4:
			return labelsInviteOutbound4xx
		case 5:
			return labelsInviteOutbound5xx
		case 6:
			return labelsInviteOutbound6xx
		}
	}
	switch class {
	case 1:
		return labelsInviteInbound1xx
	case 2:
		return labelsInviteInbound2xx
	case 3:
		return labelsInviteInbound3xx
	case 4:
		return labelsInviteInbound4xx
	case 5:
		return labelsInviteInbound5xx
	case 6:
		return labelsInviteInbound6xx
	}
	// Cold-path fallback for invalid input — won't happen given the
	// validation above, but the compiler requires a return.
	return map[string]string{"direction": direction, "class": strconv.Itoa(class) + "xx"}
}

// ---- BYE ----

// Bye classification.
const (
	ByeByLocal  = "local"
	ByeByRemote = "remote"

	// Reason classes — bounded enum.
	ByeReasonNormal       = "normal"        // 200 OK BYE no special cause
	ByeReasonTimerExpired = "timer-expired" // RFC 4028 session-timer expired
	ByeReasonError        = "error"         // unexpected (pipeline failure, etc.)
	ByeReasonUserHangup   = "user-hangup"   // explicit hangup intent
)

// Cardinality:
//
//	2 directions × 2 by × 4 reasonClass = 16 series. Pre-allocate
//	all to stay zero-alloc on the hot path. The naming convention
//	is labelsBye<Direction><By><Reason>.
var (
	labelsByeOutLocalNormal    = map[string]string{"direction": "outbound", "by": "local", "reason_class": "normal"}
	labelsByeOutLocalTimer     = map[string]string{"direction": "outbound", "by": "local", "reason_class": "timer-expired"}
	labelsByeOutLocalError     = map[string]string{"direction": "outbound", "by": "local", "reason_class": "error"}
	labelsByeOutLocalUserHang  = map[string]string{"direction": "outbound", "by": "local", "reason_class": "user-hangup"}
	labelsByeOutRemoteNormal   = map[string]string{"direction": "outbound", "by": "remote", "reason_class": "normal"}
	labelsByeOutRemoteTimer    = map[string]string{"direction": "outbound", "by": "remote", "reason_class": "timer-expired"}
	labelsByeOutRemoteError    = map[string]string{"direction": "outbound", "by": "remote", "reason_class": "error"}
	labelsByeOutRemoteUserHang = map[string]string{"direction": "outbound", "by": "remote", "reason_class": "user-hangup"}
	labelsByeInLocalNormal     = map[string]string{"direction": "inbound", "by": "local", "reason_class": "normal"}
	labelsByeInLocalTimer      = map[string]string{"direction": "inbound", "by": "local", "reason_class": "timer-expired"}
	labelsByeInLocalError      = map[string]string{"direction": "inbound", "by": "local", "reason_class": "error"}
	labelsByeInLocalUserHang   = map[string]string{"direction": "inbound", "by": "local", "reason_class": "user-hangup"}
	labelsByeInRemoteNormal    = map[string]string{"direction": "inbound", "by": "remote", "reason_class": "normal"}
	labelsByeInRemoteTimer     = map[string]string{"direction": "inbound", "by": "remote", "reason_class": "timer-expired"}
	labelsByeInRemoteError     = map[string]string{"direction": "inbound", "by": "remote", "reason_class": "error"}
	labelsByeInRemoteUserHang  = map[string]string{"direction": "inbound", "by": "remote", "reason_class": "user-hangup"}
)

// BYE bumps the BYE counter for the given direction (inbound /
// outbound), initiator (local / remote), and reason class. Backwards-
// compat shim: a 2-arg call still works via the Bye() helper which
// defaults direction to outbound. Hot path; zero allocation for any
// known combination.
func BYE(direction, by, reasonClass string) {
	var labels map[string]string
	if direction == DirectionInbound {
		switch by + "|" + reasonClass {
		case "local|normal":
			labels = labelsByeInLocalNormal
		case "local|timer-expired":
			labels = labelsByeInLocalTimer
		case "local|error":
			labels = labelsByeInLocalError
		case "local|user-hangup":
			labels = labelsByeInLocalUserHang
		case "remote|normal":
			labels = labelsByeInRemoteNormal
		case "remote|timer-expired":
			labels = labelsByeInRemoteTimer
		case "remote|error":
			labels = labelsByeInRemoteError
		case "remote|user-hangup":
			labels = labelsByeInRemoteUserHang
		default:
			labels = map[string]string{"direction": "inbound", "by": by, "reason_class": reasonClass}
		}
	} else {
		switch by + "|" + reasonClass {
		case "local|normal":
			labels = labelsByeOutLocalNormal
		case "local|timer-expired":
			labels = labelsByeOutLocalTimer
		case "local|error":
			labels = labelsByeOutLocalError
		case "local|user-hangup":
			labels = labelsByeOutLocalUserHang
		case "remote|normal":
			labels = labelsByeOutRemoteNormal
		case "remote|timer-expired":
			labels = labelsByeOutRemoteTimer
		case "remote|error":
			labels = labelsByeOutRemoteError
		case "remote|user-hangup":
			labels = labelsByeOutRemoteUserHang
		default:
			labels = map[string]string{"direction": "outbound", "by": by, "reason_class": reasonClass}
		}
	}
	metrics.Default.IncCounter(MetricByeTotal, "BYE events", labels)
}

// Bye is the outbound-default shim. Kept for existing callers that
// don't yet care about direction. New callers should prefer BYE()
// with an explicit direction.
func Bye(by, reasonClass string) {
	BYE(DirectionOutbound, by, reasonClass)
}

// ---- Transaction timeout ----

var (
	labelsTimeoutINVITE = map[string]string{"method": "INVITE"}
	labelsTimeoutBYE    = map[string]string{"method": "BYE"}
	labelsTimeoutUPDATE = map[string]string{"method": "UPDATE"}
	labelsTimeoutOTHER  = map[string]string{"method": "other"}
)

// TransactionTimeout reports a transaction-layer timeout (timer B/F
// fired). Method is the SIP method name (UPPER); we collapse the
// long tail into "other" to keep cardinality bounded.
func TransactionTimeout(method string) {
	var labels map[string]string
	switch method {
	case "INVITE":
		labels = labelsTimeoutINVITE
	case "BYE":
		labels = labelsTimeoutBYE
	case "UPDATE":
		labels = labelsTimeoutUPDATE
	default:
		labels = labelsTimeoutOTHER
	}
	metrics.Default.IncCounter(MetricTransactionTimeoutTotal,
		"SIP transaction timeouts (timer B/F fired)", labels)
}

// ---- Session-timer refresher ----

// Refresher event classification.
const (
	RefreshResultOK         = "ok"          // peer accepted with 200
	Refresh422Bumped        = "422-bumped"  // got 422, retried with peer Min-SE
	Refresh422GaveUp        = "422-gave-up" // second 422, stopped
	Refresh481DialogGone    = "481"         // dialog disappeared
	RefreshRoleSwappedToUAS = "role-swap"   // peer flipped refresher to itself
)

var (
	labelsRefreshOK        = map[string]string{"result": "ok"}
	labelsRefresh422Bumped = map[string]string{"result": "422-bumped"}
	labelsRefresh422GaveUp = map[string]string{"result": "422-gave-up"}
	labelsRefresh481       = map[string]string{"result": "481"}
	labelsRefreshRoleSwap  = map[string]string{"result": "role-swap"}
)

// SessionTimerRefresh logs one refresher state transition. Hot
// path — called from outbound refresher response handler.
func SessionTimerRefresh(result string) {
	var labels map[string]string
	switch result {
	case RefreshResultOK:
		labels = labelsRefreshOK
	case Refresh422Bumped:
		labels = labelsRefresh422Bumped
	case Refresh422GaveUp:
		labels = labelsRefresh422GaveUp
	case Refresh481DialogGone:
		labels = labelsRefresh481
	case RefreshRoleSwappedToUAS:
		labels = labelsRefreshRoleSwap
	default:
		labels = map[string]string{"result": result}
	}
	metrics.Default.IncCounter(MetricSessionTimerRefreshTotal,
		"RFC 4028 session-timer refresh outcomes", labels)
}

// ---- DTLS-SRTP handshake ----

const (
	DTLSResultOK                  = "ok"
	DTLSResultFail                = "fail"
	DTLSResultTimeout             = "timeout"
	DTLSResultFingerprintMismatch = "fingerprint-mismatch"
)

var (
	labelsDTLSOK      = map[string]string{"result": "ok"}
	labelsDTLSFail    = map[string]string{"result": "fail"}
	labelsDTLSTimeout = map[string]string{"result": "timeout"}
	labelsDTLSFPMiss  = map[string]string{"result": "fingerprint-mismatch"}
)

// DTLSHandshake reports the outcome of one DTLS-SRTP handshake.
func DTLSHandshake(result string) {
	var labels map[string]string
	switch result {
	case DTLSResultOK:
		labels = labelsDTLSOK
	case DTLSResultFail:
		labels = labelsDTLSFail
	case DTLSResultTimeout:
		labels = labelsDTLSTimeout
	case DTLSResultFingerprintMismatch:
		labels = labelsDTLSFPMiss
	default:
		labels = map[string]string{"result": result}
	}
	metrics.Default.IncCounter(MetricDTLSHandshakeTotal,
		"DTLS-SRTP handshake outcomes", labels)
}

// ---- STIR/SHAKEN verification ----

const (
	STIRResultVerified = "verified"
	STIRResultFailed   = "failed"
	STIRResultSoftFail = "soft-fail" // verifier rejected but call continued
	STIRResultNoIdent  = "no-identity"
)

var (
	labelsSTIRVerified = map[string]string{"result": "verified"}
	labelsSTIRFailed   = map[string]string{"result": "failed"}
	labelsSTIRSoftFail = map[string]string{"result": "soft-fail"}
	labelsSTIRNoIdent  = map[string]string{"result": "no-identity"}
)

// STIRVerify reports one STIR (RFC 8224) verification outcome.
func STIRVerify(result string) {
	var labels map[string]string
	switch result {
	case STIRResultVerified:
		labels = labelsSTIRVerified
	case STIRResultFailed:
		labels = labelsSTIRFailed
	case STIRResultSoftFail:
		labels = labelsSTIRSoftFail
	case STIRResultNoIdent:
		labels = labelsSTIRNoIdent
	default:
		labels = map[string]string{"result": result}
	}
	metrics.Default.IncCounter(MetricSTIRVerifyTotal,
		"STIR/SHAKEN verification outcomes", labels)
}

// ---- Call-end QoS roll-up ----

// ObserveCallQoS records the per-call RTCP-derived metrics. Call
// this ONCE per call at cleanup (after the last RTCPSnapshot).
// All inputs are optional; zero / negative values are skipped so
// "no data" doesn't pollute the distribution.
//
// Hot path? No — this runs at most once per call (~0.02 Hz/leg).
// Cardinality? Zero labels — these are global distributions.
func ObserveCallQoS(rttMs uint32, jitterMs float64, lossFraction float64, mosEstimate float64) {
	if rttMs > 0 {
		metrics.Default.Observe(MetricCallRTTMs,
			"per-call round-trip time at call end (ms)", float64(rttMs))
	}
	if jitterMs > 0 {
		metrics.Default.Observe(MetricCallJitterMs,
			"per-call interarrival jitter at call end (ms)", jitterMs)
	}
	if lossFraction >= 0 && lossFraction <= 1 {
		metrics.Default.Observe(MetricCallLossFraction,
			"per-call peer-reported loss fraction at call end (0..1)", lossFraction)
	}
	if mosEstimate >= 1 && mosEstimate <= 5 {
		metrics.Default.Observe(MetricCallMOSEstimate,
			"per-call E-Model MOS estimate at call end (1..5)", mosEstimate)
	}
}
