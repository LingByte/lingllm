// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package sdp

import (
	"testing"

	"github.com/pion/srtp/v2"
)

func TestPionProfileForSuite_KnownSuites(t *testing.T) {
	cases := map[string]srtp.ProtectionProfile{
		"AES_CM_128_HMAC_SHA1_80": srtp.ProtectionProfileAes128CmHmacSha1_80,
		"AES_CM_128_HMAC_SHA1_32": srtp.ProtectionProfileAes128CmHmacSha1_32,
		// Whitespace + lowercase shouldn't break the matcher; SDP
		// values from the wild are commonly lowercased by some SBCs.
		"  aes_cm_128_hmac_sha1_80  ": srtp.ProtectionProfileAes128CmHmacSha1_80,
	}
	for in, want := range cases {
		got, ok := PionProfileForSuite(in)
		if !ok {
			t.Errorf("PionProfileForSuite(%q) ok=false", in)
			continue
		}
		if got != want {
			t.Errorf("PionProfileForSuite(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPionProfileForSuite_Unsupported(t *testing.T) {
	for _, suite := range []string{
		"",
		"AES_256_CM_HMAC_SHA1_80", // 256-bit cipher — not supported
		"AEAD_AES_128_GCM",        // RFC 7714 — not supported
		"F8_128_HMAC_SHA1_80",     // F8 mode — not supported
		"unknown",
	} {
		if _, ok := PionProfileForSuite(suite); ok {
			t.Errorf("PionProfileForSuite(%q) unexpectedly OK", suite)
		}
	}
}

func TestPickSupportedSDESOffer_PrefersSHA180(t *testing.T) {
	// Peer lists _32 first then _80 — we MUST pick _80 because it's
	// the WebRTC default and our offer-side default. RFC 4568 §6.1
	// allows multiple suites in the offer; the answerer picks one.
	offers := []CryptoOffer{
		{Tag: 1, Suite: SuiteAESCM128HMACSHA132, KeyParams: "inline:aaa"},
		{Tag: 2, Suite: SuiteAESCM128HMACSHA180, KeyParams: "inline:bbb"},
	}
	got, ok := PickSupportedSDESOffer(offers)
	if !ok {
		t.Fatal("expected match")
	}
	if got.Tag != 2 {
		t.Errorf("picked tag %d, want 2 (the SHA1_80 entry)", got.Tag)
	}
}

func TestPickSupportedSDESOffer_FallsBackToSHA132(t *testing.T) {
	// Peer ONLY lists _32 (Cisco / Avaya fashion). We accept it.
	offers := []CryptoOffer{
		{Tag: 5, Suite: SuiteAESCM128HMACSHA132, KeyParams: "inline:aaa"},
	}
	got, ok := PickSupportedSDESOffer(offers)
	if !ok {
		t.Fatal("expected fallback match for sha-32-only offer")
	}
	if got.Suite != SuiteAESCM128HMACSHA132 {
		t.Errorf("picked %q, want sha-32 fallback", got.Suite)
	}
}

func TestPickSupportedSDESOffer_RejectsUnsupportedOnly(t *testing.T) {
	offers := []CryptoOffer{
		{Tag: 1, Suite: "AEAD_AES_128_GCM", KeyParams: "inline:x"},
		{Tag: 2, Suite: "AES_256_CM_HMAC_SHA1_80", KeyParams: "inline:y"},
	}
	if _, ok := PickSupportedSDESOffer(offers); ok {
		t.Error("must NOT accept offers we can't actually decrypt")
	}
}

func TestPickSupportedSDESOffer_EmptyList(t *testing.T) {
	if _, ok := PickSupportedSDESOffer(nil); ok {
		t.Error("nil offers should not match")
	}
	if _, ok := PickSupportedSDESOffer([]CryptoOffer{}); ok {
		t.Error("empty offers should not match")
	}
}

// TestPickAESCM128Offer_BackCompat ensures we didn't break the
// legacy single-suite picker — some external callers might still
// use it (and we need to give them a deprecation window).
func TestPickAESCM128Offer_BackCompat(t *testing.T) {
	offers := []CryptoOffer{
		{Tag: 1, Suite: SuiteAESCM128HMACSHA132, KeyParams: "inline:a"},
		{Tag: 2, Suite: SuiteAESCM128HMACSHA180, KeyParams: "inline:b"},
	}
	got, ok := PickAESCM128Offer(offers)
	if !ok || got.Tag != 2 {
		t.Errorf("legacy picker: got %v ok=%v, want tag=2", got, ok)
	}
	// _32-only must NOT match the legacy picker (it's _80-strict by name).
	if _, ok := PickAESCM128Offer([]CryptoOffer{{Suite: SuiteAESCM128HMACSHA132}}); ok {
		t.Error("legacy picker should reject sha-32-only — that's why PickSupportedSDESOffer exists")
	}
}

// TestFormatCryptoLine_AcceptsBothSuites confirms the renderer
// works with _32 too — same key/salt lengths, only the auth tag
// length differs (which lives in the SRTP context, not the SDP).
func TestFormatCryptoLine_AcceptsBothSuites(t *testing.T) {
	key := make([]byte, 16)
	salt := make([]byte, 14)
	for _, suite := range []string{
		SuiteAESCM128HMACSHA180,
		SuiteAESCM128HMACSHA132,
	} {
		line, err := FormatCryptoLine(1, suite, key, salt)
		if err != nil {
			t.Errorf("suite=%s err=%v", suite, err)
		}
		// Sanity: the rendered line carries the suite back unchanged.
		if line == "" || !contains(line, suite) {
			t.Errorf("suite=%s line=%q", suite, line)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
