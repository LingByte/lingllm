// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

// fetcher.go: x5u cert fetcher + TTL cache + cert-chain validator.
//
// The `x5u` JWS-header claim (RFC 8225 §5.1.2) is an https:// URL
// where the verifier can download the PEM-encoded cert chain that
// signed the PASSporT. RFC 8224 §5.3 says the verifier:
//
//   1. Fetches x5u over HTTPS (server cert validation per RFC 6125).
//   2. Verifies the chain against a configured STI-PA trust root.
//   3. Checks notBefore/notAfter on the leaf.
//   4. Checks the TN Authorization List extension (RFC 8226) against
//      the `orig.tn` claim — caller decides if number is in scope.
//
// We implement 1-3 here. Step 4 (RFC 8226 TN-Auth-List) is left as
// a follow-up: it requires ASN.1 OID parsing of the X.509 extension
// (OID 1.3.6.1.5.5.7.1.26) which is small but non-trivial and only
// matters when verifying TN-scoped certs (most carriers don't yet).
//
// Cache: in-memory map keyed by URL with TTL (default 1h, matching
// the typical STI cert validity / ACME-style rotation cadence).
// Production deployments should set this from config. Failures are
// negative-cached for 1m so a misconfigured x5u doesn't hammer the
// origin.

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultCertCacheTTL    = 1 * time.Hour
	defaultCertNegativeTTL = 1 * time.Minute
	defaultFetchTimeout    = 5 * time.Second
	maxCertResponseBytes   = 64 * 1024 // STI certs are <10KB; cap to stop DoS
)

// X5UFetcher retrieves and validates the certificate chain that
// signed a PASSporT. Implementations MUST validate the HTTPS server
// cert and the chain against the STI-PA trust pool before returning.
type X5UFetcher interface {
	// Fetch returns the leaf cert for x5uURL. Implementations are
	// expected to cache successful + failed lookups appropriately.
	// The returned cert's PublicKey is the *ecdsa.PublicKey used to
	// verify the PASSporT signature.
	Fetch(ctx context.Context, x5uURL string) (*x509.Certificate, error)
}

// HTTPFetcher is the default X5UFetcher: HTTP GET + PEM parse +
// chain validation + in-memory TTL cache.
type HTTPFetcher struct {
	// HTTPClient is used for all GETs. Default: http.Client with
	// FetchTimeout. Inject a custom one for tests or to pin TLS.
	HTTPClient *http.Client

	// RootCAs is the STI-PA trust pool. nil → system roots (NOT
	// recommended in production; STI certs are issued by industry-
	// specific CAs, not WebPKI).
	RootCAs *x509.CertPool

	// IntermediateCAs is the optional intermediate pool — STI chains
	// typically have one intermediate cert between the leaf and the
	// STI-PA root. The fetcher will also accept intermediates served
	// inline in the x5u response.
	IntermediateCAs *x509.CertPool

	// CacheTTL is how long a successful lookup is cached. Zero →
	// defaultCertCacheTTL.
	CacheTTL time.Duration

	// NegativeCacheTTL is how long a failed lookup is cached. Zero
	// → defaultCertNegativeTTL.
	NegativeCacheTTL time.Duration

	// FetchTimeout caps one HTTP GET. Zero → defaultFetchTimeout.
	FetchTimeout time.Duration

	// Now is the clock function used for cache TTL and cert
	// notBefore/notAfter checks. Default time.Now. Injected for
	// deterministic tests.
	Now func() time.Time

	// SkipChainVerify disables chain validation. **Test-only.** When
	// true, only the PEM parse + key-type check runs. Never set this
	// in production — it accepts any cert as legitimate.
	SkipChainVerify bool

	mu    sync.Mutex
	cache map[string]cachedCert
}

type cachedCert struct {
	cert      *x509.Certificate
	err       error
	expiresAt time.Time
}

// NewHTTPFetcher returns a fetcher with sensible defaults.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{cache: make(map[string]cachedCert)}
}

// Fetch implements X5UFetcher.
func (f *HTTPFetcher) Fetch(ctx context.Context, x5uURL string) (*x509.Certificate, error) {
	if f == nil {
		return nil, errors.New("stir: nil fetcher")
	}
	x5uURL = strings.TrimSpace(x5uURL)
	if x5uURL == "" {
		return nil, errors.New("stir: empty x5u URL")
	}
	if !strings.HasPrefix(strings.ToLower(x5uURL), "https://") {
		return nil, fmt.Errorf("stir: x5u must be https:// (got %q)", x5uURL)
	}

	now := f.clock()
	if cert, err, hit := f.cacheGet(x5uURL, now); hit {
		return cert, err
	}

	cert, err := f.fetchOnce(ctx, x5uURL, now)
	f.cachePut(x5uURL, cert, err, now)
	return cert, err
}

func (f *HTTPFetcher) clock() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *HTTPFetcher) cacheGet(url string, now time.Time) (*x509.Certificate, error, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache == nil {
		return nil, nil, false
	}
	c, ok := f.cache[url]
	if !ok {
		return nil, nil, false
	}
	if now.After(c.expiresAt) {
		delete(f.cache, url)
		return nil, nil, false
	}
	return c.cert, c.err, true
}

func (f *HTTPFetcher) cachePut(url string, cert *x509.Certificate, err error, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache == nil {
		f.cache = make(map[string]cachedCert)
	}
	ttl := f.CacheTTL
	if ttl <= 0 {
		ttl = defaultCertCacheTTL
	}
	if err != nil {
		ttl = f.NegativeCacheTTL
		if ttl <= 0 {
			ttl = defaultCertNegativeTTL
		}
	}
	f.cache[url] = cachedCert{cert: cert, err: err, expiresAt: now.Add(ttl)}
}

// fetchOnce performs the HTTP GET + PEM parse + chain validation.
// Returns the leaf cert (the one whose public key signed the JWS).
func (f *HTTPFetcher) fetchOnce(ctx context.Context, url string, now time.Time) (*x509.Certificate, error) {
	timeout := f.FetchTimeout
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("stir: build request: %w", err)
	}
	req.Header.Set("Accept", "application/pem-certificate-chain, application/x-pem-file, */*")

	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stir: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stir: fetch %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCertResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("stir: read body: %w", err)
	}
	if len(body) > maxCertResponseBytes {
		return nil, fmt.Errorf("stir: response too large (>%d bytes)", maxCertResponseBytes)
	}

	leaf, inlineIntermediates, err := parsePEMChain(body)
	if err != nil {
		return nil, err
	}
	if leaf.PublicKey == nil {
		return nil, errors.New("stir: leaf cert has no public key")
	}
	// Stricter than PublicKeyFromCert (which panics) — return error.
	if !isECDSAP256(leaf) {
		return nil, errors.New("stir: leaf cert public key must be ECDSA P-256")
	}

	// Validity window.
	if now.Before(leaf.NotBefore) {
		return nil, fmt.Errorf("stir: leaf cert not yet valid (NotBefore=%s)", leaf.NotBefore)
	}
	if now.After(leaf.NotAfter) {
		return nil, fmt.Errorf("stir: leaf cert expired (NotAfter=%s)", leaf.NotAfter)
	}

	if f.SkipChainVerify {
		return leaf, nil
	}

	// Chain validation.
	intermediates := f.IntermediateCAs
	if len(inlineIntermediates) > 0 {
		if intermediates == nil {
			intermediates = x509.NewCertPool()
		} else {
			// Don't mutate the user-supplied pool.
			intermediates = poolClone(intermediates)
		}
		for _, c := range inlineIntermediates {
			intermediates.AddCert(c)
		}
	}
	opts := x509.VerifyOptions{
		Roots:         f.RootCAs,
		Intermediates: intermediates,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	if _, err := leaf.Verify(opts); err != nil {
		return nil, fmt.Errorf("stir: chain verify: %w", err)
	}
	return leaf, nil
}

// parsePEMChain extracts the leaf cert (first CERTIFICATE block) and
// any additional CERTIFICATE blocks as inline intermediates. STI-CA
// pipelines may serve the leaf alone, or the full chain in order.
func parsePEMChain(body []byte) (*x509.Certificate, []*x509.Certificate, error) {
	var leaf *x509.Certificate
	var intermediates []*x509.Certificate
	rest := body
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			// Skip private keys or other blocks silently — some
			// providers concatenate cert + chain + key. STI never
			// exposes the key, but be lenient.
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("stir: parse cert in chain: %w", err)
		}
		if leaf == nil {
			leaf = c
		} else {
			intermediates = append(intermediates, c)
		}
	}
	if leaf == nil {
		return nil, nil, errors.New("stir: no CERTIFICATE blocks in x5u response")
	}
	return leaf, intermediates, nil
}

// isECDSAP256 reports whether the cert's public key is ECDSA P-256.
// Avoids the reflect path crypto/ecdsa would force; cheap.
func isECDSAP256(c *x509.Certificate) bool {
	pub := PublicKeyFromCertOrNil(c)
	if pub == nil {
		return false
	}
	if pub.Curve == nil {
		return false
	}
	return pub.Curve.Params().BitSize == 256
}

// PublicKeyFromCertOrNil is the non-panicking variant of
// PublicKeyFromCert. Returns nil when the cert's key isn't ECDSA.
func PublicKeyFromCertOrNil(c *x509.Certificate) *ecdsa.PublicKey {
	if c == nil {
		return nil
	}
	p, ok := c.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil
	}
	return p
}

// poolClone returns a shallow copy of a *x509.CertPool. The stdlib
// doesn't expose this directly, but Subjects() round-trips the certs
// only by DN — we instead use the documented Clone method (Go 1.19+).
func poolClone(p *x509.CertPool) *x509.CertPool {
	if p == nil {
		return x509.NewCertPool()
	}
	return p.Clone()
}
