// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package metrics

import "github.com/LingByte/lingllm/protocol/sip/observability"

// Voice attach observability surface (PR-8b).
//
// These metrics close the loop on PR-7's mode-honesty work: every
// engine.Mode dispatch path becomes visible in dashboards. The hot
// path is the SIP OnACK callback which runs once per call; allocation
// budget is the same as the existing INVITE/BYE counters (pre-allocated
// label maps, switch-based lookup, zero string formatting at call site).
//
// Cardinality:
//
//   - sip_voice_attach_total{mode,result}             — 2 × 2 = 4 series
//   - sip_voice_attach_mode_fallback_total{from,to}   — 1 series today
//                                                      (pipeline→realtime
//                                                       is the only
//                                                       fallback PR-7
//                                                       implements)
//
// Both stay well under the cardinality budget per the package comment.

const (
	// MetricVoiceAttachTotal counts voice-attach attempts at the OnACK
	// seam, classified by resolved engine.Mode and final outcome.
	//
	// labels:
	//   mode   = "cascaded" | "realtime"
	//   result = "ok" | "config_error"
	//
	// "config_error" is the umbrella for every failure path that
	// played scripts/config_error.wav (no tenant id, env load error,
	// missing/incomplete credentials). Granular reasons live in the
	// log lines emitted by AttachCascadedLegacy / AttachRealtimeLegacy;
	// they're not labels because the cardinality blows up fast.
	MetricVoiceAttachTotal = "sip_voice_attach_total"

	// MetricVoiceAttachModeFallbackTotal counts implicit mode
	// fallbacks made by ResolveAttachMode (today: tenant persisted
	// voice_mode="pipeline" but pipeline creds are unusable and
	// realtime is ready → we auto-select realtime).
	//
	// labels:
	//   from = "pipeline"
	//   to   = "realtime"
	MetricVoiceAttachModeFallbackTotal = "sip_voice_attach_mode_fallback_total"

	// MetricVoiceAttachNativeTotal counts decisions made by the
	// PR-9d feature flag to route a cascaded call through the
	// native cascaded.Engine (engine.ModeCascadedNative) instead of
	// the legacy bridge. Independent from MetricVoiceAttachTotal so
	// dashboards can monitor opt-in rollout without churn-affecting
	// the existing per-mode chart.
	//
	// labels:
	//   result = "ok" | "err"
	MetricVoiceAttachNativeTotal = "sip_voice_attach_native_total"
)

// Voice-attach mode enum. Mirrors engine.Mode but kept as plain
// strings here so this package doesn't import pkg/dialog/engine
// (which would create an import cycle once engines start emitting
// metrics directly). The constants MUST stay in sync with
// engine.Mode's string values.
const (
	VoiceAttachModeCascaded = "cascaded"
	VoiceAttachModeRealtime = "realtime"
)

// Voice-attach result enum.
const (
	VoiceAttachResultOK          = "ok"
	VoiceAttachResultConfigError = "config_error"
)

// Pre-allocated label maps. One per (mode, result) pair; the switch
// in VoiceAttach() resolves them with zero allocation.
var (
	labelsVoiceAttachCascadedOK    = map[string]string{"mode": VoiceAttachModeCascaded, "result": VoiceAttachResultOK}
	labelsVoiceAttachCascadedErr   = map[string]string{"mode": VoiceAttachModeCascaded, "result": VoiceAttachResultConfigError}
	labelsVoiceAttachRealtimeOK    = map[string]string{"mode": VoiceAttachModeRealtime, "result": VoiceAttachResultOK}
	labelsVoiceAttachRealtimeErr   = map[string]string{"mode": VoiceAttachModeRealtime, "result": VoiceAttachResultConfigError}
	labelsVoiceAttachFallbackPL2RT = map[string]string{"from": VoiceAttachModeCascaded, "to": VoiceAttachModeRealtime}
	labelsVoiceAttachNativeOK      = map[string]string{"result": VoiceAttachResultOK}
	labelsVoiceAttachNativeErr     = map[string]string{"result": "err"}
)

// init registers the label whitelist for the voice-attach metrics so
// the soft cardinality defense kicks in before any producer call.
func init() {
	metrics.RegisterLabels(MetricVoiceAttachTotal, "mode", "result")
	metrics.RegisterLabels(MetricVoiceAttachModeFallbackTotal, "from", "to")
	metrics.RegisterLabels(MetricVoiceAttachNativeTotal, "result")
}

// VoiceAttach bumps the voice-attach counter for one OnACK dispatch.
// Unknown mode / result strings are dropped silently — the goal is
// hot-path safety, not enforcement (dashboards alert on missing
// series, not on rejected inputs).
func VoiceAttach(mode string, ok bool) {
	var labels map[string]string
	switch mode {
	case VoiceAttachModeCascaded:
		if ok {
			labels = labelsVoiceAttachCascadedOK
		} else {
			labels = labelsVoiceAttachCascadedErr
		}
	case VoiceAttachModeRealtime:
		if ok {
			labels = labelsVoiceAttachRealtimeOK
		} else {
			labels = labelsVoiceAttachRealtimeErr
		}
	default:
		return
	}
	metrics.Default.IncCounter(MetricVoiceAttachTotal,
		"Voice attach attempts at SIP OnACK, classified by resolved engine.Mode and outcome.",
		labels)
}

// VoiceAttachModeFallback bumps the mode-fallback counter. Today this
// is only called when ResolveAttachMode promotes "pipeline" to
// "realtime" because pipeline creds are unusable. Future fallbacks
// would add new pre-allocated label maps and a switch arm.
func VoiceAttachModeFallback(from, to string) {
	if from == VoiceAttachModeCascaded && to == VoiceAttachModeRealtime {
		metrics.Default.IncCounter(MetricVoiceAttachModeFallbackTotal,
			"Implicit voice-mode fallbacks made by ResolveAttachMode (e.g. pipeline→realtime when pipeline creds missing).",
			labelsVoiceAttachFallbackPL2RT)
		return
	}
	// Unknown pair — drop silently. Same discipline as VoiceAttach.
}

// VoiceAttachNative bumps the native-cascaded routing counter. ok
// reflects whether the native attach succeeded (engine.New + Attach
// both returned nil). Hot-path: same allocation profile as VoiceAttach.
func VoiceAttachNative(ok bool) {
	labels := labelsVoiceAttachNativeErr
	if ok {
		labels = labelsVoiceAttachNativeOK
	}
	metrics.Default.IncCounter(MetricVoiceAttachNativeTotal,
		"Native cascaded engine attach decisions (PR-9d feature flag).",
		labels)
}
