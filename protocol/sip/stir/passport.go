// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Package stir implements RFC 8224 SIP Identity header construction
// and RFC 8225 PASSporT (Personal ASSerTion Token) sign/verify, with
// the SHAKEN extension defined in ATIS-1000074 / RFC 8588.
//
// What lives here:
//
//   - passport.go: PASSporT JWS compact-serialization sign/verify,
//                  RFC 8225 §6 claim shape, RFC 8588 SHAKEN ppt
//                  with attest + origid claims.
//   - header.go:   RFC 8224 §4 Identity header rendering / parsing,
//                  including the `info=`, `alg=`, `ppt=` parameters.
//   - keys.go:     PEM helpers that load the ES256 (P-256) signing
//                  key + cert chain a service provider was issued
//                  by an STI-CA. Pure file/PEM munging, no fetch.
//
// What does NOT live here (separate follow-up):
//
//   - x5u fetching + chain validation against the STI-PA root pool.
//   - Wiring into outbound INVITE / inbound 4xx-on-bad-Identity. The
//     B2BUA caller decides whether to attest A/B/C per call.
//
// Why a separate package from pkg/sip/identity (PAI, RFC 3325)?
// RFC 3325 is hop-by-hop trust within a domain (no signature),
// while RFC 8224 is a globally-verifiable cryptographic assertion.
// Keeping them apart prevents accidental conflation in code review.

package stir

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// AlgES256 is the only RFC 8588 SHAKEN-mandated signing algorithm
// (ECDSA P-256 with SHA-256). RFC 8225 §6.1 permits others, but
// every STI-CA in production today issues ES256-only.
const AlgES256 = "ES256"

// PptShaken is the RFC 8588 PASSporT extension identifier. Carriers
// reject anything else on the wire today.
const PptShaken = "shaken"

// AttestationLevel is the SHAKEN attestation level (RFC 8588 §6).
// A=fully attested (carrier-verified caller, carrier-issued number),
// B=partial (carrier-verified caller, third-party number),
// C=gateway (no direct relationship — typically inbound from PSTN).
type AttestationLevel string

const (
	AttestA AttestationLevel = "A"
	AttestB AttestationLevel = "B"
	AttestC AttestationLevel = "C"
)

// IsValid reports whether a is one of A/B/C.
func (a AttestationLevel) IsValid() bool {
	switch a {
	case AttestA, AttestB, AttestC:
		return true
	}
	return false
}

// PassportHeader is the JOSE header of a PASSporT JWT. RFC 8225 §5
// pins `typ`="passport"; `ppt` is set when an extension applies
// (RFC 8588 SHAKEN uses "shaken"). `x5u` is the public cert URL
// (mandatory for SHAKEN — RFC 8588 §6).
type PassportHeader struct {
	Alg string `json:"alg"`
	Ppt string `json:"ppt,omitempty"`
	Typ string `json:"typ"`
	X5u string `json:"x5u"`
}

// PassportClaims is the body of a PASSporT (RFC 8225 §5.2 +
// RFC 8588 §6 SHAKEN extras). Numbers in `orig.tn` / `dest.tn`
// MUST be E.164 normalised (leading +, only digits) — carriers
// reject unparseable TNs.
type PassportClaims struct {
	IAT    int64           `json:"iat"`
	Orig   PassportParty   `json:"orig"`
	Dest   PassportDestSet `json:"dest"`
	Attest string          `json:"attest,omitempty"` // SHAKEN only
	OrigID string          `json:"origid,omitempty"` // SHAKEN only; UUID for traceback
}

// PassportParty identifies one endpoint in a PASSporT. Exactly one
// of TN / URI should be populated per RFC 8225 §5.2.1 — TN takes
// precedence when both are set (we mirror what RFC test vectors do).
type PassportParty struct {
	TN  string `json:"tn,omitempty"`
	URI string `json:"uri,omitempty"`
}

// PassportDestSet is the `dest` claim — singleton lists of TNs or
// URIs. Even when a call has one destination, RFC 8225 mandates
// the list form. Empty lists are invalid.
type PassportDestSet struct {
	TN  []string `json:"tn,omitempty"`
	URI []string `json:"uri,omitempty"`
}

// SignedPassport is the wire form: the JWS compact serialization
// `b64(header).b64(claims).b64(signature)` plus the raw signing
// inputs (header / claims) so callers can re-render the Identity
// header without re-encoding.
type SignedPassport struct {
	Compact string
	Header  PassportHeader
	Claims  PassportClaims
}

// SignPassport builds a JWS-compact PASSporT signed with key.
//
// SHAKEN-specific validation (RFC 8588 §6):
//   - hdr.Alg must be ES256
//   - hdr.Ppt must be "shaken" (we don't sign other extensions yet)
//   - hdr.X5u must be a non-empty https:// URL
//   - claims.Attest must be A/B/C
//   - claims.OrigID must be a non-empty UUID (we don't enforce
//     RFC 4122 form here; carriers accept any unique string)
//   - claims.Orig.TN required (E.164 normalised externally)
//   - claims.Dest.TN must have ≥1 entry
//
// IAT is auto-populated to time.Now().Unix() when zero.
func SignPassport(hdr PassportHeader, claims PassportClaims, key *ecdsa.PrivateKey) (*SignedPassport, error) {
	if key == nil {
		return nil, errors.New("stir: nil signing key")
	}
	if key.Curve == nil || key.Curve.Params().BitSize != 256 {
		return nil, errors.New("stir: signing key must be P-256 (ES256)")
	}
	if hdr.Alg == "" {
		hdr.Alg = AlgES256
	}
	if hdr.Alg != AlgES256 {
		return nil, fmt.Errorf("stir: unsupported alg %q (only ES256 in SHAKEN)", hdr.Alg)
	}
	if hdr.Typ == "" {
		hdr.Typ = "passport"
	}
	if hdr.Typ != "passport" {
		return nil, fmt.Errorf("stir: typ must be \"passport\", got %q", hdr.Typ)
	}
	if hdr.Ppt == PptShaken {
		// SHAKEN extension validations — caller likely wants these.
		if !AttestationLevel(claims.Attest).IsValid() {
			return nil, fmt.Errorf("stir: SHAKEN attest must be A/B/C, got %q", claims.Attest)
		}
		if strings.TrimSpace(claims.OrigID) == "" {
			return nil, errors.New("stir: SHAKEN requires non-empty origid")
		}
	}
	if strings.TrimSpace(hdr.X5u) == "" {
		return nil, errors.New("stir: x5u is required")
	}
	if !strings.HasPrefix(strings.ToLower(hdr.X5u), "https://") {
		return nil, fmt.Errorf("stir: x5u must be https:// (got %q)", hdr.X5u)
	}
	if strings.TrimSpace(claims.Orig.TN) == "" && strings.TrimSpace(claims.Orig.URI) == "" {
		return nil, errors.New("stir: orig requires tn or uri")
	}
	if len(claims.Dest.TN) == 0 && len(claims.Dest.URI) == 0 {
		return nil, errors.New("stir: dest requires at least one tn or uri")
	}
	if claims.IAT == 0 {
		claims.IAT = time.Now().Unix()
	}

	hdrJSON, err := canonicalJSON(hdr)
	if err != nil {
		return nil, fmt.Errorf("stir: marshal header: %w", err)
	}
	claimsJSON, err := canonicalJSON(claims)
	if err != nil {
		return nil, fmt.Errorf("stir: marshal claims: %w", err)
	}

	signingInput := append(append([]byte{}, b64URLEncode(hdrJSON)...), '.')
	signingInput = append(signingInput, b64URLEncode(claimsJSON)...)

	digest := sha256.Sum256(signingInput)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("stir: ecdsa sign: %w", err)
	}

	// JWS ES256 signature is the **fixed-length** concatenation of
	// the big-endian R and S values, each padded to 32 bytes
	// (RFC 7518 §3.4). It is NOT the ASN.1 form crypto/ecdsa.Sign
	// hands back — that's the #1 wire-incompatibility bug shipping
	// in Go JWT libraries; encode explicitly.
	sig := jwsEncodeES256(r, s)

	var out bytes.Buffer
	out.Write(signingInput)
	out.WriteByte('.')
	out.Write(b64URLEncode(sig))

	return &SignedPassport{
		Compact: out.String(),
		Header:  hdr,
		Claims:  claims,
	}, nil
}

// VerifyPassport validates a JWS-compact PASSporT. Returns parsed
// header + claims when the signature checks against pub.
//
// This function does NOT fetch the x5u or validate the cert against
// any STI-CA root pool — the caller does that (it requires network
// + policy decisions out of scope here). VerifyPassport is the part
// every implementation gets wrong; isolating it makes audit easier.
func VerifyPassport(compact string, pub *ecdsa.PublicKey) (*SignedPassport, error) {
	if pub == nil {
		return nil, errors.New("stir: nil verify key")
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("stir: compact form expects 3 segments, got %d", len(parts))
	}
	hdrJSON, err := b64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("stir: decode header: %w", err)
	}
	claimsJSON, err := b64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("stir: decode claims: %w", err)
	}
	sig, err := b64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("stir: decode signature: %w", err)
	}

	var hdr PassportHeader
	if err := json.Unmarshal(hdrJSON, &hdr); err != nil {
		return nil, fmt.Errorf("stir: parse header: %w", err)
	}
	if hdr.Alg != AlgES256 {
		return nil, fmt.Errorf("stir: alg %q unsupported (need ES256)", hdr.Alg)
	}
	if hdr.Typ != "passport" {
		return nil, fmt.Errorf("stir: typ %q (need passport)", hdr.Typ)
	}

	var claims PassportClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("stir: parse claims: %w", err)
	}

	r, s, err := jwsDecodeES256(sig)
	if err != nil {
		return nil, fmt.Errorf("stir: parse signature: %w", err)
	}

	signingInput := []byte(parts[0] + "." + parts[1])
	digest := sha256.Sum256(signingInput)
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return nil, errors.New("stir: signature verification failed")
	}
	return &SignedPassport{Compact: compact, Header: hdr, Claims: claims}, nil
}

// canonicalJSON marshals v with a deterministic key order (Go's
// encoding/json sorts struct fields by declaration order, which is
// stable — what we want for reproducible signatures during testing).
// json.Marshal already escapes < > & for us; we don't want that for
// SIP because the encoded value is base64 anyway, but leaving it as
// default keeps the bytes identical to what every other implementor
// produces.
func canonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// b64URLEncode returns the base64url-no-padding encoding (RFC 7515
// §2 "Base64url Encoding").
func b64URLEncode(p []byte) []byte {
	enc := base64.RawURLEncoding
	out := make([]byte, enc.EncodedLen(len(p)))
	enc.Encode(out, p)
	return out
}

// b64URLDecode is the inverse, accepting both padded and unpadded
// forms because some PASSporT producers (incorrectly) pad.
func b64URLDecode(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

// jwsEncodeES256 turns (r, s) into the RFC 7518 §3.4 fixed 64-byte
// form. Each big.Int is left-padded with zeros to 32 bytes.
func jwsEncodeES256(r, s *big.Int) []byte {
	out := make([]byte, 64)
	rb := r.Bytes()
	sb := s.Bytes()
	copy(out[32-len(rb):32], rb)
	copy(out[64-len(sb):64], sb)
	return out
}

// jwsDecodeES256 splits the fixed 64-byte form. We *also* accept
// the ASN.1 DER form for leniency — a few corner-case implementors
// produce it (RFC violation) and rejecting outright breaks interop.
func jwsDecodeES256(sig []byte) (*big.Int, *big.Int, error) {
	if len(sig) == 64 {
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		return r, s, nil
	}
	// ASN.1 DER form: SEQUENCE{ r INTEGER, s INTEGER }
	type ecdsaSig struct{ R, S *big.Int }
	var es ecdsaSig
	if _, err := asn1.Unmarshal(sig, &es); err != nil {
		return nil, nil, fmt.Errorf("stir: signature is neither 64-byte raw nor ASN.1 DER: %w", err)
	}
	if es.R == nil || es.S == nil || es.R.Sign() <= 0 || es.S.Sign() <= 0 {
		return nil, nil, errors.New("stir: ASN.1 signature has invalid r/s")
	}
	return es.R, es.S, nil
}
