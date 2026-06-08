// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

import (
	"strings"
	"testing"
)

const fakeJWT = "eyJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJ4In0.AAAA"

func TestFormatIdentityHeader_Basic(t *testing.T) {
	got, err := FormatIdentityHeader(IdentityHeader{
		Passport: fakeJWT,
		Info:     "https://sti.example/cert.pem",
		Alg:      AlgES256,
		Ppt:      PptShaken,
	})
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	want := fakeJWT + ";info=<https://sti.example/cert.pem>;alg=ES256;ppt=shaken"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestFormatIdentityHeader_NoPpt(t *testing.T) {
	got, err := FormatIdentityHeader(IdentityHeader{
		Passport: fakeJWT,
		Info:     "https://x.example/c.pem",
		Alg:      AlgES256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "ppt=") {
		t.Errorf("ppt= should be omitted when empty: %q", got)
	}
}

func TestFormatIdentityHeader_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		h    IdentityHeader
	}{
		{"empty jwt", IdentityHeader{Info: "https://x/c", Alg: "ES256"}},
		{"jwt with space", IdentityHeader{Passport: "a b", Info: "https://x/c", Alg: "ES256"}},
		{"missing info", IdentityHeader{Passport: fakeJWT, Alg: "ES256"}},
		{"http info", IdentityHeader{Passport: fakeJWT, Info: "http://x/c", Alg: "ES256"}},
		{"missing alg", IdentityHeader{Passport: fakeJWT, Info: "https://x/c"}},
	}
	for _, tc := range cases {
		if _, err := FormatIdentityHeader(tc.h); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestParseIdentityHeader_Unquoted(t *testing.T) {
	raw := fakeJWT + ";info=<https://sti.example/cert.pem>;alg=ES256;ppt=shaken"
	h, err := ParseIdentityHeader(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.Passport != fakeJWT {
		t.Errorf("passport: %q", h.Passport)
	}
	if h.Info != "https://sti.example/cert.pem" {
		t.Errorf("info: %q", h.Info)
	}
	if h.Alg != "ES256" || h.Ppt != "shaken" {
		t.Errorf("alg/ppt: %q %q", h.Alg, h.Ppt)
	}
}

func TestParseIdentityHeader_LegacyQuoted(t *testing.T) {
	// RFC 4474 form: JWT wrapped in double quotes.
	raw := `"` + fakeJWT + `";info=<https://x.example/c.pem>;alg=ES256`
	h, err := ParseIdentityHeader(raw)
	if err != nil {
		t.Fatalf("legacy quoted: %v", err)
	}
	if h.Passport != fakeJWT {
		t.Errorf("quoted jwt not unwrapped: %q", h.Passport)
	}
}

func TestParseIdentityHeader_CaseInsensitiveParams(t *testing.T) {
	raw := fakeJWT + ";INFO=<https://x.example/c.pem>;Alg=ES256;PPT=shaken"
	h, err := ParseIdentityHeader(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.Info != "https://x.example/c.pem" || h.Alg != "ES256" || h.Ppt != "shaken" {
		t.Errorf("case-insensitive failed: %+v", h)
	}
}

func TestParseIdentityHeader_RejectsMissingParts(t *testing.T) {
	cases := []string{
		"",
		"only.two;info=<https://x/c>;alg=ES256",        // bad JWT
		"a.b.c.d;info=<https://x/c>;alg=ES256",         // 4 segments
		fakeJWT + ";alg=ES256",                          // no info
		fakeJWT + ";info=<https://x/c>",                 // no alg
	}
	for _, raw := range cases {
		if _, err := ParseIdentityHeader(raw); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}

func TestParseFormat_RoundTrip(t *testing.T) {
	in := IdentityHeader{
		Passport: fakeJWT,
		Info:     "https://sti.example/cert.pem",
		Alg:      AlgES256,
		Ppt:      PptShaken,
	}
	raw, err := FormatIdentityHeader(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ParseIdentityHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("round-trip mismatch:\nin=%+v\nout=%+v", in, out)
	}
}
