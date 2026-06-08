// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// genCert returns a leaf cert signed by a freshly-minted self-signed
// CA. The CA cert + leaf private key are also returned so tests can
// (a) pin the CA in RootCAs and (b) sign PASSporT JWTs with the leaf
// key.
func genCertChain(t *testing.T, notBefore, notAfter time.Time) (leaf *x509.Certificate, leafKey *ecdsa.PrivateKey, caCert *x509.Certificate) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "stir-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ = x509.ParseCertificate(caDER)

	leafKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "stir-test-leaf"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ = x509.ParseCertificate(leafDER)
	return leaf, leafKey, caCert
}

func pemChain(certs ...*x509.Certificate) []byte {
	var b strings.Builder
	for _, c := range certs {
		_ = pem.Encode(&strBuilderWriter{&b}, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return []byte(b.String())
}

type strBuilderWriter struct{ b *strings.Builder }

func (w *strBuilderWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// ---------------------------------------------------------------------------
// HTTPFetcher tests
// ---------------------------------------------------------------------------

func TestHTTPFetcher_Success(t *testing.T) {
	now := time.Now()
	leaf, _, ca := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	body := pemChain(leaf)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	roots := x509.NewCertPool()
	roots.AddCert(ca)
	f := &HTTPFetcher{
		HTTPClient: srv.Client(),
		RootCAs:    roots,
		Now:        func() time.Time { return now },
	}
	got, err := f.Fetch(context.Background(), srv.URL+"/cert.pem")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.SerialNumber.Cmp(leaf.SerialNumber) != 0 {
		t.Error("returned cert serial mismatch")
	}
}

func TestHTTPFetcher_Cache(t *testing.T) {
	now := time.Now()
	leaf, _, ca := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	body := pemChain(leaf)

	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	roots := x509.NewCertPool()
	roots.AddCert(ca)
	f := &HTTPFetcher{
		HTTPClient: srv.Client(),
		RootCAs:    roots,
		Now:        func() time.Time { return now },
		CacheTTL:   time.Hour,
	}
	for i := 0; i < 5; i++ {
		if _, err := f.Fetch(context.Background(), srv.URL+"/cert.pem"); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("expected 1 origin hit (rest cached), got %d", hits)
	}
}

func TestHTTPFetcher_NegativeCache(t *testing.T) {
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	f := &HTTPFetcher{
		HTTPClient:       srv.Client(),
		NegativeCacheTTL: time.Hour,
		Now:              func() time.Time { return time.Now() },
	}
	for i := 0; i < 3; i++ {
		if _, err := f.Fetch(context.Background(), srv.URL+"/cert.pem"); err == nil {
			t.Fatal("expected error")
		}
	}
	if hits != 1 {
		t.Errorf("expected 1 origin hit (rest negative-cached), got %d", hits)
	}
}

func TestHTTPFetcher_RejectsHTTP(t *testing.T) {
	f := NewHTTPFetcher()
	if _, err := f.Fetch(context.Background(), "http://insecure.example/cert.pem"); err == nil {
		t.Fatal("http:// URL must be rejected")
	}
}

func TestHTTPFetcher_ExpiredCert(t *testing.T) {
	now := time.Now()
	leaf, _, ca := genCertChain(t, now.Add(-2*time.Hour), now.Add(-time.Hour)) // expired 1h ago
	body := pemChain(leaf)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	f := &HTTPFetcher{HTTPClient: srv.Client(), RootCAs: roots, Now: func() time.Time { return now }}
	if _, err := f.Fetch(context.Background(), srv.URL+"/cert.pem"); err == nil {
		t.Fatal("expired cert must be rejected")
	}
}

func TestHTTPFetcher_UntrustedCA(t *testing.T) {
	now := time.Now()
	leaf, _, _ := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	body := pemChain(leaf)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	// RootCAs is empty pool, so chain validation should fail.
	f := &HTTPFetcher{
		HTTPClient: srv.Client(),
		RootCAs:    x509.NewCertPool(),
		Now:        func() time.Time { return now },
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/cert.pem"); err == nil {
		t.Fatal("untrusted CA must be rejected")
	}
}

func TestHTTPFetcher_TooLarge(t *testing.T) {
	huge := make([]byte, maxCertResponseBytes+10)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(huge)
	}))
	defer srv.Close()
	f := &HTTPFetcher{HTTPClient: srv.Client()}
	if _, err := f.Fetch(context.Background(), srv.URL+"/cert.pem"); err == nil {
		t.Fatal("oversized response must be rejected")
	}
}

// ---------------------------------------------------------------------------
// Verifier end-to-end tests
// ---------------------------------------------------------------------------

// mockFetcher returns canned certs without going through HTTP. Used
// for Verifier tests where we want to control the cert independently
// of the signature key.
type mockFetcher struct {
	certs map[string]*x509.Certificate
	err   error
}

func (m *mockFetcher) Fetch(_ context.Context, url string) (*x509.Certificate, error) {
	if m.err != nil {
		return nil, m.err
	}
	c, ok := m.certs[url]
	if !ok {
		return nil, fmt.Errorf("no mock cert for %s", url)
	}
	return c, nil
}

func TestVerifier_EndToEnd_Pass(t *testing.T) {
	now := time.Now()
	leaf, key, _ := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))

	x5u := "https://sti.example/cert.pem"
	hdr := PassportHeader{Alg: AlgES256, Ppt: PptShaken, Typ: "passport", X5u: x5u}
	claims := PassportClaims{
		IAT:    now.Unix(),
		Orig:   PassportParty{TN: "+15551234567"},
		Dest:   PassportDestSet{TN: []string{"+15559876543"}},
		Attest: "A",
		OrigID: "uuid-1",
	}
	signed, err := SignPassport(hdr, claims, key)
	if err != nil {
		t.Fatal(err)
	}
	idHeader, err := FormatIdentityHeader(IdentityHeader{
		Passport: signed.Compact, Info: x5u, Alg: AlgES256, Ppt: PptShaken,
	})
	if err != nil {
		t.Fatal(err)
	}

	v := &Verifier{
		Fetcher: &mockFetcher{certs: map[string]*x509.Certificate{x5u: leaf}},
		MaxAge:  time.Minute,
		Now:     func() time.Time { return now },
	}
	verdict, err := v.Verify(context.Background(), idHeader, VerifyOptions{
		FromTN:          "+1 (555) 123-4567", // formatted differently — should match
		RequiredPpt:     PptShaken,
		RequiredAttests: []string{"A", "B"},
	})
	if err != nil {
		t.Fatalf("verify err: %v", err)
	}
	if !verdict.Pass() {
		t.Fatalf("expected pass, got %s (%s)", verdict.Code, verdict.Reason)
	}
	if verdict.Code.SIPResponseCode() != 200 {
		t.Errorf("pass should map to 200, got %d", verdict.Code.SIPResponseCode())
	}
}

func TestVerifier_TamperedSignature(t *testing.T) {
	now := time.Now()
	leaf, key, _ := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	x5u := "https://sti.example/cert.pem"
	hdr := PassportHeader{Alg: AlgES256, Ppt: PptShaken, Typ: "passport", X5u: x5u}
	claims := PassportClaims{
		IAT: now.Unix(), Orig: PassportParty{TN: "+15551234567"},
		Dest: PassportDestSet{TN: []string{"+15559876543"}}, Attest: "A", OrigID: "u",
	}
	signed, _ := SignPassport(hdr, claims, key)
	// Flip a bit in the signature segment.
	parts := strings.Split(signed.Compact, ".")
	parts[2] = strings.Repeat("A", len(parts[2]))
	tampered := strings.Join(parts, ".")
	idHeader, _ := FormatIdentityHeader(IdentityHeader{
		Passport: tampered, Info: x5u, Alg: AlgES256, Ppt: PptShaken,
	})

	v := &Verifier{
		Fetcher: &mockFetcher{certs: map[string]*x509.Certificate{x5u: leaf}},
		Now:     func() time.Time { return now },
	}
	verdict, err := v.Verify(context.Background(), idHeader, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Code != VerdictBadIdentity {
		t.Errorf("expected BadIdentity, got %s", verdict.Code)
	}
	if verdict.Code.SIPResponseCode() != 438 {
		t.Errorf("BadIdentity must map to 438, got %d", verdict.Code.SIPResponseCode())
	}
}

func TestVerifier_StaleIAT(t *testing.T) {
	now := time.Now()
	leaf, key, _ := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	x5u := "https://sti.example/cert.pem"
	hdr := PassportHeader{Alg: AlgES256, Ppt: PptShaken, Typ: "passport", X5u: x5u}
	claims := PassportClaims{
		IAT: now.Add(-10 * time.Minute).Unix(),
		Orig: PassportParty{TN: "+1"}, Dest: PassportDestSet{TN: []string{"+2"}},
		Attest: "A", OrigID: "u",
	}
	signed, _ := SignPassport(hdr, claims, key)
	idHeader, _ := FormatIdentityHeader(IdentityHeader{
		Passport: signed.Compact, Info: x5u, Alg: AlgES256, Ppt: PptShaken,
	})

	v := &Verifier{
		Fetcher: &mockFetcher{certs: map[string]*x509.Certificate{x5u: leaf}},
		MaxAge:  time.Minute,
		Now:     func() time.Time { return now },
	}
	verdict, _ := v.Verify(context.Background(), idHeader, VerifyOptions{})
	if verdict.Code != VerdictStale {
		t.Errorf("expected Stale, got %s (%s)", verdict.Code, verdict.Reason)
	}
	if verdict.Code.SIPResponseCode() != 403 {
		t.Errorf("Stale must map to 403, got %d", verdict.Code.SIPResponseCode())
	}
}

func TestVerifier_InfoX5UMismatch(t *testing.T) {
	now := time.Now()
	leaf, key, _ := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	jwsX5U := "https://sti.example/cert.pem"
	headerInfo := "https://different.example/cert.pem"

	hdr := PassportHeader{Alg: AlgES256, Ppt: PptShaken, Typ: "passport", X5u: jwsX5U}
	claims := PassportClaims{
		IAT: now.Unix(),
		Orig: PassportParty{TN: "+1"}, Dest: PassportDestSet{TN: []string{"+2"}},
		Attest: "A", OrigID: "u",
	}
	signed, _ := SignPassport(hdr, claims, key)
	idHeader, _ := FormatIdentityHeader(IdentityHeader{
		Passport: signed.Compact, Info: headerInfo, Alg: AlgES256, Ppt: PptShaken,
	})

	v := &Verifier{
		Fetcher: &mockFetcher{certs: map[string]*x509.Certificate{headerInfo: leaf}},
		Now:     func() time.Time { return now },
	}
	verdict, _ := v.Verify(context.Background(), idHeader, VerifyOptions{})
	if verdict.Code != VerdictMismatch {
		t.Errorf("expected Mismatch, got %s (%s)", verdict.Code, verdict.Reason)
	}
}

func TestVerifier_TNMismatch(t *testing.T) {
	now := time.Now()
	leaf, key, _ := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	x5u := "https://sti.example/cert.pem"
	hdr := PassportHeader{Alg: AlgES256, Ppt: PptShaken, Typ: "passport", X5u: x5u}
	claims := PassportClaims{
		IAT: now.Unix(),
		Orig: PassportParty{TN: "+15551234567"}, Dest: PassportDestSet{TN: []string{"+2"}},
		Attest: "A", OrigID: "u",
	}
	signed, _ := SignPassport(hdr, claims, key)
	idHeader, _ := FormatIdentityHeader(IdentityHeader{
		Passport: signed.Compact, Info: x5u, Alg: AlgES256, Ppt: PptShaken,
	})

	v := &Verifier{
		Fetcher: &mockFetcher{certs: map[string]*x509.Certificate{x5u: leaf}},
		Now:     func() time.Time { return now },
	}
	verdict, _ := v.Verify(context.Background(), idHeader, VerifyOptions{
		FromTN: "+19998887777",
	})
	if verdict.Code != VerdictMismatch {
		t.Errorf("expected Mismatch on TN diff, got %s", verdict.Code)
	}
}

func TestVerifier_AttestFiltered(t *testing.T) {
	now := time.Now()
	leaf, key, _ := genCertChain(t, now.Add(-time.Hour), now.Add(time.Hour))
	x5u := "https://sti.example/cert.pem"
	hdr := PassportHeader{Alg: AlgES256, Ppt: PptShaken, Typ: "passport", X5u: x5u}
	claims := PassportClaims{
		IAT: now.Unix(),
		Orig: PassportParty{TN: "+1"}, Dest: PassportDestSet{TN: []string{"+2"}},
		Attest: "C", OrigID: "u",
	}
	signed, _ := SignPassport(hdr, claims, key)
	idHeader, _ := FormatIdentityHeader(IdentityHeader{
		Passport: signed.Compact, Info: x5u, Alg: AlgES256, Ppt: PptShaken,
	})

	v := &Verifier{
		Fetcher: &mockFetcher{certs: map[string]*x509.Certificate{x5u: leaf}},
		Now:     func() time.Time { return now },
	}
	verdict, _ := v.Verify(context.Background(), idHeader, VerifyOptions{
		RequiredAttests: []string{"A", "B"},
	})
	if verdict.Code != VerdictMismatch {
		t.Errorf("expected Mismatch filtering attest=C, got %s", verdict.Code)
	}
}

func TestVerifier_BadHeader(t *testing.T) {
	v := NewVerifier()
	verdict, _ := v.Verify(context.Background(), "garbage no semicolons", VerifyOptions{})
	if verdict.Code != VerdictBadIdentity {
		t.Errorf("garbage header should be BadIdentity, got %s", verdict.Code)
	}
}

func TestVerifier_FetcherError(t *testing.T) {
	v := &Verifier{
		Fetcher: &mockFetcher{err: fmt.Errorf("simulated network down")},
		Now:     func() time.Time { return time.Now() },
	}
	idHeader := fakeJWT + ";info=<https://x.example/c.pem>;alg=ES256;ppt=shaken"
	verdict, _ := v.Verify(context.Background(), idHeader, VerifyOptions{})
	if verdict.Code != VerdictBadCert {
		t.Errorf("fetch err should be BadCert, got %s", verdict.Code)
	}
}

func TestCanonicalTN(t *testing.T) {
	cases := map[string]string{
		"+15551234567":          "+15551234567",
		"+1 (555) 123-4567":     "+15551234567",
		"  +1-555-1234567  ":   "+15551234567",
		"abc+1xyz5551234567pq": "+15551234567",
	}
	for in, want := range cases {
		if got := canonicalTN(in); got != want {
			t.Errorf("canonicalTN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalURI(t *testing.T) {
	cases := map[string]string{
		"sip:alice@example.com":                  "sip:alice@example.com",
		"<sip:alice@EXAMPLE.com>":                "sip:alice@example.com",
		"<sip:Alice@example.com>;tag=abc":        "sip:Alice@example.com",
		"\"Alice\" <sip:alice@example.com>;lr":   "sip:alice@example.com",
	}
	for in, want := range cases {
		if got := canonicalURI(in); got != want {
			t.Errorf("canonicalURI(%q) = %q, want %q", in, got, want)
		}
	}
}
