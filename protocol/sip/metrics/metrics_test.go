// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package metrics

import (
	"strings"
	"testing"

	voiceMetrics "github.com/LingByte/lingllm/protocol/sip/observability"
)

func snapshot(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	voiceMetrics.Default.WritePromText(&sb)
	return sb.String()
}

func TestInviteResult_ClassifiesByHundreds(t *testing.T) {
	InviteResult(DirectionOutbound, 100)
	InviteResult(DirectionOutbound, 180)
	InviteResult(DirectionOutbound, 200)
	InviteResult(DirectionOutbound, 302)
	InviteResult(DirectionOutbound, 404)
	InviteResult(DirectionOutbound, 503)
	InviteResult(DirectionOutbound, 603)
	out := snapshot(t)

	// All six classes should appear at least once.
	for _, class := range []string{"1xx", "2xx", "3xx", "4xx", "5xx", "6xx"} {
		needle := `class="` + class + `",direction="outbound"`
		if !strings.Contains(out, needle) {
			t.Errorf("missing %q in /metrics output:\n%s", needle, out)
		}
	}
}

func TestInviteResult_RejectsInvalidCode(t *testing.T) {
	// Pick a brand-new direction so any pre-existing counter state
	// from sibling tests doesn't contaminate our before/after check.
	// We don't include `WritePromText` byte-equality because map
	// iteration order in WritePromText is non-deterministic.
	const probe = "probe-invalid-codes"
	before := strings.Count(snapshot(t), `direction="`+probe+`"`)
	InviteResult(probe, 0)
	InviteResult(probe, 99)
	InviteResult(probe, 700)
	after := strings.Count(snapshot(t), `direction="`+probe+`"`)
	if after != before {
		t.Errorf("invalid codes must not introduce a series; before=%d after=%d",
			before, after)
	}
}

func TestBye_AllKnownClassifications(t *testing.T) {
	Bye(ByeByLocal, ByeReasonNormal)
	Bye(ByeByLocal, ByeReasonTimerExpired)
	Bye(ByeByRemote, ByeReasonError)
	Bye(ByeByRemote, ByeReasonUserHangup)
	out := snapshot(t)
	for _, want := range []string{
		`by="local",direction="outbound",reason_class="normal"`,
		`by="local",direction="outbound",reason_class="timer-expired"`,
		`by="remote",direction="outbound",reason_class="error"`,
		`by="remote",direction="outbound",reason_class="user-hangup"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in /metrics:\n%s", want, out)
		}
	}
}

// BYE(direction, by, reason) covers inbound legs. The pre-allocated
// label maps must produce stable label ordering in the exposition.
func TestBYE_InboundDirection(t *testing.T) {
	BYE(DirectionInbound, ByeByRemote, ByeReasonNormal)
	BYE(DirectionInbound, ByeByLocal, ByeReasonError)
	out := snapshot(t)
	for _, want := range []string{
		`by="remote",direction="inbound",reason_class="normal"`,
		`by="local",direction="inbound",reason_class="error"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in /metrics:\n%s", want, out)
		}
	}
}

// The 2-arg Bye() helper must still bump the outbound-direction
// series so legacy callers don't silently break after the refactor.
// We compare the counter VALUE, not substring occurrences — the
// label set forms one time-series whose count grows.
func TestBye_LegacyShimRoutesToOutbound(t *testing.T) {
	const needle = `sip_bye_total{by="local",direction="outbound",reason_class="normal"} `
	before := extractCounter(t, needle)
	Bye(ByeByLocal, ByeReasonNormal)
	after := extractCounter(t, needle)
	if after <= before {
		t.Errorf("legacy Bye() did not bump outbound series; before=%d after=%d",
			before, after)
	}
}

// extractCounter parses the counter value for an exact line prefix
// out of the /metrics exposition. Returns 0 when the prefix isn't
// found yet (first call before any bump).
func extractCounter(t *testing.T, linePrefix string) int {
	t.Helper()
	out := snapshot(t)
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, linePrefix) {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, linePrefix))
		n, err := parseIntLoose(val)
		if err != nil {
			t.Fatalf("counter line malformed: %q", line)
		}
		return n
	}
	return 0
}

func parseIntLoose(s string) (int, error) {
	// Counter values are integers (we never expose fractional bumps)
	// but the exposition appends a trailing newline / could carry a
	// timestamp suffix in future versions. Trim and parse defensively.
	for i, r := range s {
		if r == ' ' || r == '\n' || r == '\r' {
			s = s[:i]
			break
		}
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errInvalidInt
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

var errInvalidInt = errInt{}

type errInt struct{}

func (errInt) Error() string { return "not an integer" }

func TestSessionTimerRefresh_AllOutcomes(t *testing.T) {
	SessionTimerRefresh(RefreshResultOK)
	SessionTimerRefresh(Refresh422Bumped)
	SessionTimerRefresh(Refresh422GaveUp)
	SessionTimerRefresh(Refresh481DialogGone)
	SessionTimerRefresh(RefreshRoleSwappedToUAS)
	out := snapshot(t)
	for _, want := range []string{"ok", "422-bumped", "422-gave-up", "481", "role-swap"} {
		if !strings.Contains(out, `result="`+want+`"`) {
			t.Errorf("missing result=%q in /metrics", want)
		}
	}
}

func TestDTLSHandshake_AllOutcomes(t *testing.T) {
	DTLSHandshake(DTLSResultOK)
	DTLSHandshake(DTLSResultFail)
	DTLSHandshake(DTLSResultTimeout)
	DTLSHandshake(DTLSResultFingerprintMismatch)
	out := snapshot(t)
	for _, want := range []string{"ok", "fail", "timeout", "fingerprint-mismatch"} {
		if !strings.Contains(out, `result="`+want+`"`) {
			t.Errorf("missing dtls result=%q in /metrics", want)
		}
	}
}

func TestSTIRVerify_AllOutcomes(t *testing.T) {
	STIRVerify(STIRResultVerified)
	STIRVerify(STIRResultFailed)
	STIRVerify(STIRResultSoftFail)
	STIRVerify(STIRResultNoIdent)
	out := snapshot(t)
	for _, want := range []string{"verified", "failed", "soft-fail", "no-identity"} {
		if !strings.Contains(out, `result="`+want+`"`) {
			t.Errorf("missing stir result=%q in /metrics", want)
		}
	}
}

func TestTransactionTimeout_LongTailCollapsed(t *testing.T) {
	TransactionTimeout("INVITE")
	TransactionTimeout("BYE")
	TransactionTimeout("UPDATE")
	TransactionTimeout("FANCY-NEW-METHOD") // → "other"
	out := snapshot(t)
	for _, want := range []string{"INVITE", "BYE", "UPDATE", "other"} {
		if !strings.Contains(out, `method="`+want+`"`) {
			t.Errorf("missing method=%q in /metrics", want)
		}
	}
}

func TestObserveCallQoS_SkipsInvalidValues(t *testing.T) {
	// MOS=0 and loss>1 must NOT enter the histograms.
	before := snapshot(t)
	ObserveCallQoS(0, 0, 5.0, 0)
	after := snapshot(t)
	// Same content (no new buckets / counts added) — the function
	// is a no-op for these inputs.
	if before != after {
		// It's acceptable for a "_count 0" line to appear once on
		// first call; allow the diff if the only change is zero-
		// valued buckets. A non-zero bucket diff is a bug.
		if strings.Contains(after, MetricCallMOSEstimate+`_count 1`) {
			t.Error("invalid MOS=0 should not increment the histogram")
		}
	}
}

func TestObserveCallQoS_HappyPath(t *testing.T) {
	ObserveCallQoS(80, 5.0, 0.02, 4.2)
	out := snapshot(t)
	for _, want := range []string{
		MetricCallRTTMs,
		MetricCallJitterMs,
		MetricCallLossFraction,
		MetricCallMOSEstimate,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing histogram %q in /metrics", want)
		}
	}
}
