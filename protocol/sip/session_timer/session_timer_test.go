// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package session_timer

import (
	"reflect"
	"testing"
)

func TestParseSessionExpires(t *testing.T) {
	cases := []struct {
		in     string
		sec    int
		ref    Refresher
		extras []string
	}{
		{"", 0, RefresherUnset, nil},
		{"   ", 0, RefresherUnset, nil},
		{"1800", 1800, RefresherUnset, nil},
		{"1800;refresher=uac", 1800, RefresherUAC, nil},
		{"1800;refresher=UAS", 1800, RefresherUAS, nil},
		{"1800;refresher=other;foo=bar", 1800, RefresherUnset, []string{"foo=bar"}},
		// Unknown extras must be preserved verbatim.
		{"3600;refresher=uas;custom=x;flag", 3600, RefresherUAS, []string{"custom=x", "flag"}},
		// Negative / zero → invalid → 0.
		{"0", 0, RefresherUnset, nil},
		{"-5", 0, RefresherUnset, nil},
		{"garbage", 0, RefresherUnset, nil},
		{"1800 ; refresher=uac", 1800, RefresherUAC, nil}, // whitespace tolerance
	}
	for _, tc := range cases {
		sec, ref, extras := ParseSessionExpires(tc.in)
		if sec != tc.sec || ref != tc.ref || !reflect.DeepEqual(extras, tc.extras) {
			t.Errorf("ParseSessionExpires(%q) = (%d, %q, %v); want (%d, %q, %v)",
				tc.in, sec, ref, extras, tc.sec, tc.ref, tc.extras)
		}
	}
}

func TestParseMinSE(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"90", 90},
		{"90;param=x", 90},
		{"  90  ", 90},
		{"bad", 0},
		{"0", 0},
	}
	for _, tc := range cases {
		if got := ParseMinSE(tc.in); got != tc.want {
			t.Errorf("ParseMinSE(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseTokenList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"timer", []string{"timer"}},
		{"timer, replaces", []string{"timer", "replaces"}},
		{"TIMER", []string{"timer"}}, // lowercased
		{"timer\r\n100rel", []string{"timer", "100rel"}}, // folded headers
		{",  ,timer,,", []string{"timer"}},
	}
	for _, tc := range cases {
		got := ParseTokenList(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseTokenList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestHasToken(t *testing.T) {
	list := []string{"timer", "100rel"}
	if !HasToken(list, "timer") || !HasToken(list, "TIMER") {
		t.Errorf("HasToken should be case-insensitive")
	}
	if HasToken(list, "replaces") {
		t.Errorf("false positive")
	}
}

func TestFormatSessionExpires(t *testing.T) {
	cases := []struct {
		sec  int
		ref  Refresher
		want string
	}{
		{0, RefresherUAC, ""},
		{1800, RefresherUnset, "1800"},
		{1800, RefresherUAC, "1800;refresher=uac"},
		{1800, RefresherUAS, "1800;refresher=uas"},
	}
	for _, tc := range cases {
		if got := FormatSessionExpires(tc.sec, tc.ref); got != tc.want {
			t.Errorf("FormatSessionExpires(%d, %q) = %q, want %q", tc.sec, tc.ref, got, tc.want)
		}
	}
}

func TestNegotiateUAS_NoTimerOffered(t *testing.T) {
	// Peer didn't mention timers at all → no timer.
	d := NegotiateUAS(0, RefresherUnset, 0, false, false, 90, 1800)
	if d.Reject422 || d.ChosenSE != 0 || d.Refresher != RefresherUnset {
		t.Errorf("expected disabled decision, got %s", d)
	}
	if d.IsActive() {
		t.Errorf("disabled decision should not be active")
	}
}

func TestNegotiateUAS_PeerSupportsButNoSE(t *testing.T) {
	// Peer's Supported: timer but no Session-Expires → we propose
	// localPreferredSE with refresher=uas.
	d := NegotiateUAS(0, RefresherUnset, 0, true, false, 90, 1800)
	if d.Reject422 {
		t.Fatalf("should not 422")
	}
	if d.ChosenSE != 1800 || d.Refresher != RefresherUAS {
		t.Errorf("got %s, want SE=1800 refresher=uas", d)
	}
	if !d.SupportedTimer {
		t.Errorf("response must advertise Supported: timer")
	}
}

func TestNegotiateUAS_Reject422(t *testing.T) {
	// Peer's SE below our Min-SE → 422.
	d := NegotiateUAS(60, RefresherUAC, 60, true, false, 90, 1800)
	if !d.Reject422 || d.MinSE != 90 {
		t.Errorf("expected 422 with Min-SE=90, got %s", d)
	}
	if d.IsActive() {
		t.Errorf("422 decision should not be active")
	}
}

func TestNegotiateUAS_AcceptHonorsPeerRefresher(t *testing.T) {
	d := NegotiateUAS(1800, RefresherUAC, 90, true, false, 90, 1800)
	if d.Refresher != RefresherUAC {
		t.Errorf("must honor peer's refresher=uac, got %s", d)
	}
	if d.ChosenSE != 1800 {
		t.Errorf("ChosenSE = %d, want 1800", d.ChosenSE)
	}
	if !d.IsActive() {
		t.Errorf("decision should be active")
	}
}

func TestNegotiateUAS_DefaultRefresherIsUAS(t *testing.T) {
	// Peer omitted refresher → we default to uas (peer refreshes us)
	// so we don't have to send in-dialog refreshes.
	d := NegotiateUAS(1800, RefresherUnset, 90, true, false, 90, 1800)
	if d.Refresher != RefresherUAS {
		t.Errorf("default refresher should be uas, got %s", d.Refresher)
	}
}

func TestNegotiateUAS_RequireTimer(t *testing.T) {
	d := NegotiateUAS(1800, RefresherUAS, 90, true, true, 90, 1800)
	if !d.RequireTimer {
		t.Errorf("RequireTimer must be propagated when peer requires it")
	}
}

func TestNegotiateUAS_CapsAtHardMax(t *testing.T) {
	d := NegotiateUAS(99999, RefresherUAS, 90, true, false, 90, 1800)
	if d.ChosenSE != HardMaxSE {
		t.Errorf("ChosenSE = %d; want capped at %d", d.ChosenSE, HardMaxSE)
	}
}

func TestNegotiateUAS_ZeroLocalConfigsFallToDefaults(t *testing.T) {
	d := NegotiateUAS(0, RefresherUnset, 0, true, false, 0, 0)
	if d.ChosenSE != DefaultSE || d.MinSE != DefaultMinSE {
		t.Errorf("zero local config should fall to defaults; got %s", d)
	}
}

func TestDecision_Windows(t *testing.T) {
	d := Decision{ChosenSE: 1800, Refresher: RefresherUAS}
	if d.WatchdogInterval() != 1800 {
		t.Errorf("WatchdogInterval = %d", d.WatchdogInterval())
	}
	if d.RefresherWindow() != 900 {
		t.Errorf("RefresherWindow = %d, want 900", d.RefresherWindow())
	}

	empty := Decision{}
	if empty.WatchdogInterval() != 0 || empty.RefresherWindow() != 0 {
		t.Errorf("empty decision should have 0 windows")
	}
}
