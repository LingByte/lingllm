// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

// newES256Key spins up a fresh P-256 keypair for one test.
func newES256Key(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func validPassport() (PassportHeader, PassportClaims) {
	return PassportHeader{
			Alg: AlgES256, Ppt: PptShaken, Typ: "passport",
			X5u: "https://sti-cert.example/cert.pem",
		}, PassportClaims{
			Orig:   PassportParty{TN: "+15551234567"},
			Dest:   PassportDestSet{TN: []string{"+15559876543"}},
			Attest: string(AttestA),
			OrigID: "f5b0e0e8-3e7c-4d6e-9d5e-9b1a4f4b9c1f",
		}
}

func TestSignPassport_RoundTrip(t *testing.T) {
	key := newES256Key(t)
	hdr, claims := validPassport()
	signed, err := SignPassport(hdr, claims, key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if strings.Count(signed.Compact, ".") != 2 {
		t.Errorf("compact form should have 2 dots, got %q", signed.Compact)
	}
	if signed.Claims.IAT == 0 {
		t.Error("IAT should be auto-populated")
	}

	v, err := VerifyPassport(signed.Compact, &key.PublicKey)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if v.Header.Alg != AlgES256 || v.Header.Ppt != PptShaken {
		t.Errorf("header lost in round-trip: %+v", v.Header)
	}
	if v.Claims.Orig.TN != "+15551234567" {
		t.Errorf("orig TN: %q", v.Claims.Orig.TN)
	}
	if len(v.Claims.Dest.TN) != 1 || v.Claims.Dest.TN[0] != "+15559876543" {
		t.Errorf("dest TN: %v", v.Claims.Dest.TN)
	}
	if v.Claims.Attest != "A" || v.Claims.OrigID == "" {
		t.Errorf("SHAKEN claims lost: %+v", v.Claims)
	}
}

func TestSignPassport_RejectsWrongKey(t *testing.T) {
	key := newES256Key(t)
	other := newES256Key(t)
	hdr, claims := validPassport()
	signed, err := SignPassport(hdr, claims, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPassport(signed.Compact, &other.PublicKey); err == nil {
		t.Fatal("verify with wrong key must fail")
	}
}

func TestSignPassport_TamperedClaimsFail(t *testing.T) {
	key := newES256Key(t)
	hdr, claims := validPassport()
	signed, err := SignPassport(hdr, claims, key)
	if err != nil {
		t.Fatal(err)
	}
	// Replace the middle segment with a re-encoded claims block that
	// flips the attestation level — re-encode without re-signing.
	parts := strings.Split(signed.Compact, ".")
	tamperedClaims := strings.NewReplacer(`"attest":"A"`, `"attest":"C"`).Replace(string(mustDecodeB64URL(t, parts[1])))
	parts[1] = string(b64URLEncode([]byte(tamperedClaims)))
	tampered := strings.Join(parts, ".")
	if _, err := VerifyPassport(tampered, &key.PublicKey); err == nil {
		t.Fatal("tampered claims must fail verification")
	}
}

func TestSignPassport_RejectsNonES256Key(t *testing.T) {
	// P-384 key — same algo family but wrong curve.
	bad, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hdr, claims := validPassport()
	if _, err := SignPassport(hdr, claims, bad); err == nil {
		t.Fatal("P-384 key must be rejected")
	}
}

func TestSignPassport_ValidationErrors(t *testing.T) {
	key := newES256Key(t)
	hdr, claims := validPassport()

	// missing x5u
	bad := hdr
	bad.X5u = ""
	if _, err := SignPassport(bad, claims, key); err == nil {
		t.Error("empty x5u should fail")
	}
	// http (not https) x5u
	bad = hdr
	bad.X5u = "http://insecure.example/cert"
	if _, err := SignPassport(bad, claims, key); err == nil {
		t.Error("http x5u should fail")
	}

	// SHAKEN bad attest
	bad2 := claims
	bad2.Attest = "Z"
	if _, err := SignPassport(hdr, bad2, key); err == nil {
		t.Error("invalid attest should fail")
	}
	// SHAKEN missing origid
	bad2 = claims
	bad2.OrigID = ""
	if _, err := SignPassport(hdr, bad2, key); err == nil {
		t.Error("missing origid should fail")
	}
	// missing orig
	bad2 = claims
	bad2.Orig = PassportParty{}
	if _, err := SignPassport(hdr, bad2, key); err == nil {
		t.Error("empty orig should fail")
	}
	// empty dest list
	bad2 = claims
	bad2.Dest = PassportDestSet{}
	if _, err := SignPassport(hdr, bad2, key); err == nil {
		t.Error("empty dest should fail")
	}
}

func TestSignPassport_NonShakenSkipsExtensionChecks(t *testing.T) {
	// A plain RFC 8225 passport (no ppt extension) should NOT require
	// attest / origid — those are SHAKEN-only.
	key := newES256Key(t)
	hdr := PassportHeader{
		Alg: AlgES256, Typ: "passport",
		X5u: "https://x.example/cert.pem",
	}
	claims := PassportClaims{
		Orig: PassportParty{URI: "sip:alice@example.com"},
		Dest: PassportDestSet{URI: []string{"sip:bob@example.com"}},
	}
	signed, err := SignPassport(hdr, claims, key)
	if err != nil {
		t.Fatalf("plain RFC 8225: %v", err)
	}
	if _, err := VerifyPassport(signed.Compact, &key.PublicKey); err != nil {
		t.Fatalf("verify plain: %v", err)
	}
}

func TestSignPassport_IATFreshness(t *testing.T) {
	key := newES256Key(t)
	hdr, claims := validPassport()
	before := time.Now().Unix()
	signed, err := SignPassport(hdr, claims, key)
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().Unix()
	if signed.Claims.IAT < before || signed.Claims.IAT > after {
		t.Errorf("IAT %d not in [%d,%d]", signed.Claims.IAT, before, after)
	}
}

func TestVerifyPassport_RejectsMalformed(t *testing.T) {
	key := newES256Key(t)
	bad := []string{
		"",
		"only.two",
		"a.b.c.d",
		"!!!.!!!.!!!", // not base64
	}
	for _, b := range bad {
		if _, err := VerifyPassport(b, &key.PublicKey); err == nil {
			t.Errorf("expected error for malformed %q", b)
		}
	}
}

// ---------------------------------------------------------------------------
// keys.go tests
// ---------------------------------------------------------------------------

func TestLoadES256PrivateKey_SEC1AndPKCS8(t *testing.T) {
	key := newES256Key(t)

	// SEC1 form
	sec1, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	sec1PEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1})
	loaded, err := LoadES256PrivateKey(sec1PEM)
	if err != nil {
		t.Fatalf("SEC1 load: %v", err)
	}
	if loaded.D.Cmp(key.D) != 0 {
		t.Error("SEC1 round-trip changed scalar")
	}

	// PKCS#8 form
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	loaded2, err := LoadES256PrivateKey(pkcs8PEM)
	if err != nil {
		t.Fatalf("PKCS#8 load: %v", err)
	}
	if loaded2.D.Cmp(key.D) != 0 {
		t.Error("PKCS#8 round-trip changed scalar")
	}
}

func TestLoadES256PrivateKey_RejectsRSA(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	rsaPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	if _, err := LoadES256PrivateKey(rsaPEM); err == nil {
		t.Fatal("RSA key must be rejected")
	}
}

func TestLoadES256PrivateKey_RejectsP384(t *testing.T) {
	k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sec1, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	p384PEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1})
	if _, err := LoadES256PrivateKey(p384PEM); err == nil {
		t.Fatal("P-384 must be rejected")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustDecodeB64URL(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64URLDecode(s)
	if err != nil {
		t.Fatalf("b64 decode %q: %v", s, err)
	}
	return b
}
