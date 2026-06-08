// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package sdp

import "testing"

func TestDTLSRole_IsValid(t *testing.T) {
	for _, r := range []DTLSRole{DTLSRoleActive, DTLSRolePassive, DTLSRoleActPass, DTLSRoleHoldConn} {
		if !r.IsValid() {
			t.Errorf("%s should be valid", r)
		}
	}
	if DTLSRole("garbage").IsValid() {
		t.Error("garbage should not be valid")
	}
}

func TestAnswerRole(t *testing.T) {
	cases := map[DTLSRole]DTLSRole{
		DTLSRoleActPass:  DTLSRolePassive, // we choose passive (cheaper)
		DTLSRoleActive:   DTLSRolePassive,
		DTLSRolePassive:  DTLSRoleActive,
		DTLSRoleHoldConn: DTLSRoleHoldConn,
		DTLSRole(""):     DTLSRoleActPass, // missing → safe default
	}
	for in, want := range cases {
		if got := AnswerRole(in); got != want {
			t.Errorf("AnswerRole(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFingerprint_RoundTrip(t *testing.T) {
	in := Fingerprint{
		HashFunc: "sha-256",
		Hex:      "12:34:56:78:9A:BC:DE:F0:12:34:56:78:9A:BC:DE:F0:12:34:56:78:9A:BC:DE:F0:12:34:56:78:9A:BC:DE:F0",
	}
	rendered := FormatFingerprint(in)
	if rendered == "" {
		t.Fatal("format failed")
	}
	out := ParseFingerprint(rendered)
	if out.HashFunc != "sha-256" {
		t.Errorf("hash func = %q", out.HashFunc)
	}
	if out.Hex != in.Hex {
		t.Errorf("hex round-trip: %q vs %q", out.Hex, in.Hex)
	}
}

func TestFingerprint_RejectsWeakHashes(t *testing.T) {
	// MD5 / SHA-1 are explicitly forbidden by RFC 8122.
	for _, weak := range []string{"md5", "sha-1"} {
		fp := Fingerprint{HashFunc: weak, Hex: "AA:BB"}
		if FormatFingerprint(fp) != "" {
			t.Errorf("%s should be rejected by FormatFingerprint", weak)
		}
		if ParseFingerprint(weak+" AA:BB").HashFunc != "" {
			t.Errorf("%s should be rejected by ParseFingerprint", weak)
		}
	}
}

func TestFingerprint_NormalisesCase(t *testing.T) {
	out := ParseFingerprint("SHA-256 ab:cd:ef")
	if out.HashFunc != "sha-256" {
		t.Errorf("hash func not lowercased: %q", out.HashFunc)
	}
	if out.Hex != "AB:CD:EF" {
		t.Errorf("hex not uppercased: %q", out.Hex)
	}
}

func TestFingerprint_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"sha-256",             // no hex
		"sha-256 ",            // empty hex
		"sha-256 :AA:BB",      // leading colon
		"sha-256 AA:BB:",      // trailing colon
		"sha-256 ZZ:YY",       // not hex
		"sha-256 AA BB CC",    // multiple fields after hash (we want exactly 2)
		"sha-256 AA:BB extra", // extra field
	}
	for _, c := range cases {
		if ParseFingerprint(c).HashFunc != "" {
			t.Errorf("%q should be rejected", c)
		}
	}
}

func TestParseRole(t *testing.T) {
	cases := map[string]DTLSRole{
		"active":   DTLSRoleActive,
		"PASSIVE":  DTLSRolePassive,
		"actpass":  DTLSRoleActPass,
		"holdconn": DTLSRoleHoldConn,
		"":         DTLSRole(""),
		"garbage":  DTLSRole(""),
	}
	for in, want := range cases {
		if got := ParseRole(in); got != want {
			t.Errorf("ParseRole(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsDTLSTransport(t *testing.T) {
	cases := map[string]bool{
		"UDP/TLS/RTP/SAVP":  true,
		"UDP/TLS/RTP/SAVPF": true,
		"udp/tls/rtp/savpf": true,  // case-insensitive
		"RTP/SAVP":          false, // SDES, not DTLS
		"RTP/AVP":           false,
		"":                  false,
	}
	for proto, want := range cases {
		if got := IsDTLSTransport(proto); got != want {
			t.Errorf("IsDTLSTransport(%q) = %v, want %v", proto, got, want)
		}
	}
}

func TestParse_ExtractsFingerprintAndSetup(t *testing.T) {
	body := "v=0\r\n" +
		"o=- 0 0 IN IP4 1.2.3.4\r\n" +
		"s=-\r\n" +
		"c=IN IP4 1.2.3.4\r\n" +
		"t=0 0\r\n" +
		"m=audio 5004 UDP/TLS/RTP/SAVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"a=fingerprint:sha-256 AB:CD:EF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC\r\n" +
		"a=setup:actpass\r\n"
	info, err := Parse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !IsDTLSTransport(info.Proto) {
		t.Errorf("proto = %q, expected DTLS", info.Proto)
	}
	if len(info.Fingerprints) != 1 {
		t.Fatalf("fingerprints = %v", info.Fingerprints)
	}
	if info.Fingerprints[0].HashFunc != "sha-256" {
		t.Errorf("fp hash = %q", info.Fingerprints[0].HashFunc)
	}
	if info.DTLSRole != DTLSRoleActPass {
		t.Errorf("role = %q, want actpass", info.DTLSRole)
	}
}

func TestVerifyDTLSCertFingerprint_Match(t *testing.T) {
	// Synthetic "cert" — any byte string with stable hash will do.
	der := []byte("cert-data-for-test-1234567890")
	got := hashCertSHA256ForTest(der)
	advertised := []Fingerprint{{HashFunc: "sha-256", Hex: got}}
	if err := VerifyDTLSCertFingerprint(der, advertised); err != nil {
		t.Errorf("self-computed sha-256 should match: %v", err)
	}
}

func TestVerifyDTLSCertFingerprint_Mismatch(t *testing.T) {
	der := []byte("cert-A")
	other := []byte("cert-B")
	advertised := []Fingerprint{
		{HashFunc: "sha-256", Hex: hashCertSHA256ForTest(other)},
	}
	if err := VerifyDTLSCertFingerprint(der, advertised); err == nil {
		t.Error("mismatched fingerprints must NOT verify — MITM risk")
	}
}

func TestVerifyDTLSCertFingerprint_RejectsEmptyInputs(t *testing.T) {
	if err := VerifyDTLSCertFingerprint(nil, []Fingerprint{{HashFunc: "sha-256", Hex: "AA"}}); err == nil {
		t.Error("nil cert should fail")
	}
	if err := VerifyDTLSCertFingerprint([]byte("x"), nil); err == nil {
		t.Error("empty advertised list should fail")
	}
}

func TestVerifyDTLSCertFingerprint_UnsupportedHashesSkipped(t *testing.T) {
	der := []byte("cert-A")
	advertised := []Fingerprint{
		{HashFunc: "sha-512", Hex: "DEAD:BEEF"},                // unsupported
		{HashFunc: "sha-256", Hex: hashCertSHA256ForTest(der)}, // matches
	}
	if err := VerifyDTLSCertFingerprint(der, advertised); err != nil {
		t.Errorf("should accept on sha-256 match even with unsupported algs present: %v", err)
	}
}

func TestVerifyDTLSCertFingerprint_AllUnsupportedHashesFails(t *testing.T) {
	der := []byte("cert-A")
	advertised := []Fingerprint{
		{HashFunc: "sha-512", Hex: "DEAD:BEEF"},
		{HashFunc: "sha-384", Hex: "CAFE:BABE"},
	}
	if err := VerifyDTLSCertFingerprint(der, advertised); err == nil {
		t.Error("no supported hash → must fail")
	}
}

func TestFormatFingerprintLine(t *testing.T) {
	fp := Fingerprint{HashFunc: "sha-256", Hex: "AA:BB:CC"}
	got := FormatFingerprintLine(fp)
	want := "a=fingerprint:sha-256 AA:BB:CC"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Malformed → empty
	if FormatFingerprintLine(Fingerprint{HashFunc: "md5", Hex: "AA"}) != "" {
		t.Error("weak hash should yield empty line")
	}
}

func TestFormatSetupLine(t *testing.T) {
	cases := map[DTLSRole]string{
		DTLSRoleActive:   "a=setup:active",
		DTLSRolePassive:  "a=setup:passive",
		DTLSRoleActPass:  "a=setup:actpass",
		DTLSRoleHoldConn: "a=setup:holdconn",
		DTLSRole(""):     "",
		DTLSRole("foo"):  "",
	}
	for in, want := range cases {
		if got := FormatSetupLine(in); got != want {
			t.Errorf("FormatSetupLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// hashCertSHA256ForTest mirrors hashCertWithAlg("sha-256") without
// exporting internals.
func hashCertSHA256ForTest(der []byte) string {
	h, _ := hashCertWithAlg(der, "sha-256")
	return h
}

func TestParse_NoFingerprintWhenSDES(t *testing.T) {
	// SDP using SDES (RTP/SAVP + a=crypto) — should NOT yield
	// fingerprints in Info.
	body := "v=0\r\n" +
		"m=audio 5004 RTP/SAVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:abc\r\n"
	info, err := Parse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(info.Fingerprints) != 0 {
		t.Errorf("SDES SDP should have no fingerprints, got %v", info.Fingerprints)
	}
	if IsDTLSTransport(info.Proto) {
		t.Errorf("RTP/SAVP must not register as DTLS transport")
	}
}
