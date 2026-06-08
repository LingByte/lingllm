// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package metrics

import (
	"strings"
	"testing"
)

func TestVoiceAttach_EmitsAllFourSeries(t *testing.T) {
	VoiceAttach(VoiceAttachModeCascaded, true)
	VoiceAttach(VoiceAttachModeCascaded, false)
	VoiceAttach(VoiceAttachModeRealtime, true)
	VoiceAttach(VoiceAttachModeRealtime, false)

	out := snapshot(t)
	for _, want := range []string{
		`mode="cascaded",result="ok"`,
		`mode="cascaded",result="config_error"`,
		`mode="realtime",result="ok"`,
		`mode="realtime",result="config_error"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing label combo %q in /metrics output:\n%s", want, out)
		}
	}
	if !strings.Contains(out, MetricVoiceAttachTotal) {
		t.Errorf("missing metric name %q in output:\n%s", MetricVoiceAttachTotal, out)
	}
}

func TestVoiceAttach_DropsUnknownMode(t *testing.T) {
	// Capture the snapshot before, push an unknown mode, snapshot
	// after, assert no new lines containing our metric were added.
	before := snapshot(t)
	beforeCount := strings.Count(before, MetricVoiceAttachTotal)

	VoiceAttach("multimodal-future", true) // unknown — should drop
	VoiceAttach("", true)                  // empty — should drop

	after := snapshot(t)
	afterCount := strings.Count(after, MetricVoiceAttachTotal)
	if afterCount != beforeCount {
		t.Errorf("unknown mode produced new series: before=%d after=%d", beforeCount, afterCount)
	}
}

func TestVoiceAttachModeFallback_PipelineToRealtime(t *testing.T) {
	VoiceAttachModeFallback(VoiceAttachModeCascaded, VoiceAttachModeRealtime)
	out := snapshot(t)
	want := `from="cascaded",to="realtime"`
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in /metrics output:\n%s", want, out)
	}
	if !strings.Contains(out, MetricVoiceAttachModeFallbackTotal) {
		t.Errorf("missing metric name %q in output:\n%s", MetricVoiceAttachModeFallbackTotal, out)
	}
}

func TestVoiceAttachModeFallback_DropsUnknownPair(t *testing.T) {
	before := snapshot(t)
	beforeCount := strings.Count(before, MetricVoiceAttachModeFallbackTotal)

	VoiceAttachModeFallback("realtime", "cascaded") // not implemented
	VoiceAttachModeFallback("foo", "bar")           // nonsense

	after := snapshot(t)
	afterCount := strings.Count(after, MetricVoiceAttachModeFallbackTotal)
	if afterCount != beforeCount {
		t.Errorf("unknown fallback pair produced new series: before=%d after=%d", beforeCount, afterCount)
	}
}

// TestVoiceAttachConstants_MatchEngineModeStringValues guards against
// drift between sipMetrics.VoiceAttachMode* and engine.Mode. The two
// can't share a type because the import would be a cycle, but they
// MUST stringify identically.
//
// engine.Mode values are tested in pkg/dialog/engine. Here we just
// pin the strings — if engine.Mode ever changes its values, both
// tests fail, and the fix is to update either side.
func TestVoiceAttachConstants_PinStringValues(t *testing.T) {
	if VoiceAttachModeCascaded != "cascaded" {
		t.Errorf("VoiceAttachModeCascaded = %q, want \"cascaded\"", VoiceAttachModeCascaded)
	}
	if VoiceAttachModeRealtime != "realtime" {
		t.Errorf("VoiceAttachModeRealtime = %q, want \"realtime\"", VoiceAttachModeRealtime)
	}
}
