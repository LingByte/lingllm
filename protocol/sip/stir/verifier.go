// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

// Verifier orchestrates the full RFC 8224 §6 inbound verification
// pipeline:
//
//   1. Parse the Identity header value (header.go).
//   2. Fetch + chain-validate the x5u cert (fetcher.go).
//   3. Verify the PASSporT signature against the cert's public key
//      (passport.go).
//   4. Cross-check the Identity `info=` parameter against the JWS
//      `x5u` claim — RFC 8224 §6.2.3 says they MUST match.
//   5. (optional, caller-driven) match `orig.tn` against the SIP
//      From header so a SHAKEN A-attest token can't claim a number
//      it doesn't own.
//
// This file does NOT touch SIP message types — the caller passes in
// the raw Identity header value plus the From-claimed TN/URI. That
// keeps the package free of dependencies on pkg/sip/stack and means
// inbound TCP/UDP servers can call it without bringing along extra
// types.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// VerdictCode identifies the outcome of a verification attempt.
// Distinct from SIP response codes — call SIPResponseCode() for the
// RFC 8224 §6.2.4 status to emit when rejecting. Keeping the verdict
// and SIP code separate lets multiple verdicts (e.g. BadIdentity and
// Mismatch) share the same on-wire response (438) without ambiguity
// in logs and metrics.
type VerdictCode int

const (
	// VerdictPass: signature valid, cert chain trusted, params match.
	VerdictPass VerdictCode = iota
	// VerdictBadIdentity: header failed to parse, the JWS was
	// malformed, or the signature didn't verify. Maps to SIP 438.
	VerdictBadIdentity
	// VerdictBadCert: the x5u cert wasn't fetchable, didn't validate
	// against the trust pool, or had an unsupported algorithm. Maps
	// to SIP 437 "Unsupported Credential".
	VerdictBadCert
	// VerdictStale: the iat claim is too old (RFC 8224 §6.3.1 replay
	// protection). Maps to SIP 403 "Stale Date".
	VerdictStale
	// VerdictMismatch: info ≠ x5u, attest level filtered out, or
	// claimed orig.tn doesn't match the SIP From header. Maps to
	// SIP 438 (same wire code as BadIdentity but logged distinctly).
	VerdictMismatch
)

// String renders the verdict for log lines and metrics labels.
func (v VerdictCode) String() string {
	switch v {
	case VerdictPass:
		return "pass"
	case VerdictBadIdentity:
		return "invalid_identity"
	case VerdictBadCert:
		return "bad_cert"
	case VerdictStale:
		return "stale"
	case VerdictMismatch:
		return "mismatch"
	}
	return fmt.Sprintf("unknown(%d)", int(v))
}

// SIPResponseCode is the RFC 8224 §6.2.4 SIP response a caller
// should emit when configured to reject this verdict.
func (v VerdictCode) SIPResponseCode() int {
	switch v {
	case VerdictPass:
		return 200
	case VerdictBadCert:
		return 437
	case VerdictStale:
		return 403
	case VerdictBadIdentity, VerdictMismatch:
		return 438
	}
	return 500
}

// Verdict is the outcome of a single Verifier.Verify call.
type Verdict struct {
	Code     VerdictCode
	Reason   string          // human-readable detail
	Passport *SignedPassport // populated on pass + most failure modes
	Header   *IdentityHeader // populated when the header parsed OK
}

// Pass reports whether the verdict is a clean pass.
func (v Verdict) Pass() bool { return v.Code == VerdictPass }

// VerifyOptions narrows what a single Verify call enforces. None of
// these are required for a basic signature check; populate them as
// the SIP server gains confidence about what to reject.
type VerifyOptions struct {
	// FromTN is the SIP From header's TN (E.164, leading +). When
	// non-empty, the verifier checks that PASSporT.orig.tn matches.
	// Empty disables the check (useful for URI-only origins).
	FromTN string

	// FromURI is the SIP From header's URI. When non-empty AND the
	// PASSporT's orig is URI-form, the verifier checks for equality.
	FromURI string

	// RequiredPpt: when non-empty, the JWS header's `ppt` MUST equal
	// this value. Typical: "shaken" for North American inbound.
	RequiredPpt string

	// RequiredAttests: when non-empty, the PASSporT's `attest` MUST
	// be one of these (e.g. []{"A","B"} to reject C-attest gateway
	// calls). Empty allows any value.
	RequiredAttests []string
}

// Verifier holds the dependencies and policy for inbound Identity
// header verification. Construct via NewVerifier; zero value is not
// usable (Fetcher is required).
type Verifier struct {
	Fetcher X5UFetcher

	// MaxAge is how old the JWS `iat` claim is allowed to be before
	// the verifier returns VerdictStale (RFC 8224 §6.3.1 anti-replay).
	// Zero → 60s, matching the RFC SHOULD recommendation.
	MaxAge time.Duration

	// Now is the clock used for staleness checks. Default time.Now;
	// inject for deterministic tests.
	Now func() time.Time
}

// NewVerifier returns a Verifier that uses the default HTTPFetcher
// and a 60s staleness window. Callers should swap Fetcher with one
// configured for the deployment's STI-PA trust pool before use.
func NewVerifier() *Verifier {
	return &Verifier{Fetcher: NewHTTPFetcher(), MaxAge: 60 * time.Second}
}

// Verify runs the end-to-end pipeline on one Identity header value
// (the part after `Identity: `). Returns a Verdict + non-nil error
// only on programming faults; verification failures are encoded in
// the Verdict.Code so callers can switch on the SIP response code
// directly.
func (v *Verifier) Verify(ctx context.Context, identityHeader string, opts VerifyOptions) (Verdict, error) {
	if v == nil || v.Fetcher == nil {
		return Verdict{}, errors.New("stir: verifier missing fetcher")
	}

	// Step 1: parse the Identity header.
	hdr, err := ParseIdentityHeader(identityHeader)
	if err != nil {
		return Verdict{Code: VerdictBadIdentity, Reason: err.Error()}, nil
	}

	// Step 2: fetch + validate the x5u chain.
	cert, err := v.Fetcher.Fetch(ctx, hdr.Info)
	if err != nil {
		return Verdict{Code: VerdictBadCert, Reason: err.Error(), Header: &hdr}, nil
	}
	pub := PublicKeyFromCertOrNil(cert)
	if pub == nil {
		return Verdict{Code: VerdictBadCert, Reason: "cert public key is not ECDSA P-256", Header: &hdr}, nil
	}

	// Step 3: verify the PASSporT signature.
	signed, err := VerifyPassport(hdr.Passport, pub)
	if err != nil {
		return Verdict{Code: VerdictBadIdentity, Reason: err.Error(), Header: &hdr}, nil
	}

	// Step 4: info= MUST match the JWS x5u (RFC 8224 §6.2.3).
	if !urlEqual(signed.Header.X5u, hdr.Info) {
		return Verdict{
			Code:     VerdictMismatch,
			Reason:   fmt.Sprintf("Identity info=%q != JWS x5u=%q", hdr.Info, signed.Header.X5u),
			Header:   &hdr,
			Passport: signed,
		}, nil
	}

	// Step 5: ppt constraint.
	if opts.RequiredPpt != "" && signed.Header.Ppt != opts.RequiredPpt {
		return Verdict{
			Code:     VerdictMismatch,
			Reason:   fmt.Sprintf("required ppt=%q, got %q", opts.RequiredPpt, signed.Header.Ppt),
			Header:   &hdr,
			Passport: signed,
		}, nil
	}

	// Step 6: staleness check (anti-replay).
	maxAge := v.MaxAge
	if maxAge <= 0 {
		maxAge = 60 * time.Second
	}
	now := v.clock()
	if signed.Claims.IAT > 0 {
		age := now.Sub(time.Unix(signed.Claims.IAT, 0))
		if age > maxAge {
			return Verdict{
				Code:     VerdictStale,
				Reason:   fmt.Sprintf("iat age %s exceeds MaxAge %s", age, maxAge),
				Header:   &hdr,
				Passport: signed,
			}, nil
		}
		// Future-dated iat (clock skew) past 30s is also suspicious.
		if -age > 30*time.Second {
			return Verdict{
				Code:     VerdictStale,
				Reason:   fmt.Sprintf("iat is %s in the future", -age),
				Header:   &hdr,
				Passport: signed,
			}, nil
		}
	}

	// Step 7: From-header TN/URI sanity check.
	if opts.FromTN != "" && signed.Claims.Orig.TN != "" {
		if !tnEqual(opts.FromTN, signed.Claims.Orig.TN) {
			return Verdict{
				Code:     VerdictMismatch,
				Reason:   fmt.Sprintf("From TN %q != orig.tn %q", opts.FromTN, signed.Claims.Orig.TN),
				Header:   &hdr,
				Passport: signed,
			}, nil
		}
	}
	if opts.FromURI != "" && signed.Claims.Orig.URI != "" {
		if !uriEqual(opts.FromURI, signed.Claims.Orig.URI) {
			return Verdict{
				Code:     VerdictMismatch,
				Reason:   fmt.Sprintf("From URI %q != orig.uri %q", opts.FromURI, signed.Claims.Orig.URI),
				Header:   &hdr,
				Passport: signed,
			}, nil
		}
	}

	// Step 8: attestation level constraint.
	if len(opts.RequiredAttests) > 0 {
		ok := false
		for _, a := range opts.RequiredAttests {
			if signed.Claims.Attest == a {
				ok = true
				break
			}
		}
		if !ok {
			return Verdict{
				Code:     VerdictMismatch,
				Reason:   fmt.Sprintf("attest=%q not in required set %v", signed.Claims.Attest, opts.RequiredAttests),
				Header:   &hdr,
				Passport: signed,
			}, nil
		}
	}

	return Verdict{Code: VerdictPass, Header: &hdr, Passport: signed}, nil
}

func (v *Verifier) clock() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// urlEqual compares two URLs case-insensitively after trimming. We
// don't do full RFC 3986 normalisation because STI-PA endpoints emit
// canonical https://host/path forms and any divergence usually
// signals tampering, not legitimate variation.
func urlEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// tnEqual compares two E.164 TNs leniently. We strip common
// punctuation that some From-header renderers introduce (`-`, ` `,
// `(`, `)`) so `+1 (555) 123-4567` matches `+15551234567`.
func tnEqual(a, b string) bool {
	return canonicalTN(a) == canonicalTN(b)
}

func canonicalTN(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '+' || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// uriEqual compares SIP URIs case-insensitively on the scheme + host
// but case-sensitively on the user-part (RFC 3261 §19.1.4). Strips
// `<>` brackets and `;tag=...` parameters that the From header would
// normally carry.
func uriEqual(a, b string) bool {
	return canonicalURI(a) == canonicalURI(b)
}

func canonicalURI(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i+1:], '>'); j >= 0 {
			s = s[i+1 : i+1+j]
		}
	}
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	at := strings.IndexByte(s, '@')
	if at < 0 {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return strings.TrimSpace(s[:at]) + strings.ToLower(strings.TrimSpace(s[at:]))
}
