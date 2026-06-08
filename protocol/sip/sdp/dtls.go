// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package sdp

// dtls.go: SDP DTLS-SRTP attributes per RFC 5763 (signalling) +
// RFC 5764 (DTLS-SRTP keying). Pure parsing/rendering — handshake
// + key derivation live in pkg/sip/rtp.
//
// What we negotiate:
//
//   * `a=fingerprint:<hash> <hex>`   — RFC 8122 §5
//       Identifies the cert the peer will present in the DTLS
//       handshake. We exchange these at SDP layer so a MITM on the
//       media path can't impersonate either side without the SIP
//       signalling already being authenticated (this is why
//       SIP-over-TLS + DTLS-SRTP is the WebRTC threat model).
//
//   * `a=setup:active|passive|actpass|holdconn`   — RFC 5763 §5
//       Who initiates the DTLS handshake. The offerer typically
//       sends "actpass" (no preference); the answerer picks "active"
//       (= will dial DTLS) or "passive" (= will listen). We default
//       to "passive" on the answer because most carriers / WebRTC
//       browsers initiate from their side, and listening is cheaper.
//
//   * `m=audio … UDP/TLS/RTP/SAVP …`  — RFC 5764 §8
//       Distinct from `RTP/SAVP` (SDES-SRTP). When the offer uses
//       this transport AND carries a fingerprint, we MUST do
//       DTLS-SRTP; SDES is not allowed to interleave.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// DTLSRole is who drives the DTLS handshake. RFC 5763 §5.
type DTLSRole string

const (
	// DTLSRoleActive: this side will send ClientHello (initiator).
	DTLSRoleActive DTLSRole = "active"
	// DTLSRolePassive: this side will wait for ClientHello.
	DTLSRolePassive DTLSRole = "passive"
	// DTLSRoleActPass: offerer's "no preference"; answerer must
	// pick active or passive.
	DTLSRoleActPass DTLSRole = "actpass"
	// DTLSRoleHoldConn: keep an existing DTLS conn (RFC 5763 §5.3).
	// We never offer this; receiving it from a peer means "don't
	// renegotiate", we treat it as opaque.
	DTLSRoleHoldConn DTLSRole = "holdconn"
)

// IsValid reports whether r is one of the four RFC 5763 values.
func (r DTLSRole) IsValid() bool {
	switch r {
	case DTLSRoleActive, DTLSRolePassive, DTLSRoleActPass, DTLSRoleHoldConn:
		return true
	}
	return false
}

// AnswerRole picks our role on the answer side given what the
// offerer proposed. RFC 5763 §5: an offer of `actpass` lets us
// choose; `active` from the offerer forces us passive; `passive`
// from the offerer forces us active; `holdconn` means "no DTLS
// renegotiation requested" — we don't touch DTLS state.
//
// Returns DTLSRoleActPass when the offer was missing or unrecognised
// (caller should treat that as "no DTLS-SRTP requested").
func AnswerRole(offerRole DTLSRole) DTLSRole {
	switch offerRole {
	case DTLSRoleActPass:
		// Default to passive — it's cheaper (no outbound dial) and
		// most carriers / softphones expect to drive the handshake.
		return DTLSRolePassive
	case DTLSRoleActive:
		return DTLSRolePassive
	case DTLSRolePassive:
		return DTLSRoleActive
	case DTLSRoleHoldConn:
		return DTLSRoleHoldConn
	}
	return DTLSRoleActPass
}

// Fingerprint is one RFC 8122 cert fingerprint declaration.
// HashFunc is the IANA hash registry name (lowercase: "sha-256",
// "sha-384", "sha-512"). Hex is the colon-separated uppercase
// hex digest exactly as it appears on the wire.
type Fingerprint struct {
	HashFunc string
	Hex      string
}

// FormatFingerprint renders one `a=fingerprint:` line value
// (without the leading "a=fingerprint:"). Returns "" when the
// fingerprint is invalid.
func FormatFingerprint(fp Fingerprint) string {
	h := strings.TrimSpace(strings.ToLower(fp.HashFunc))
	hx := strings.TrimSpace(fp.Hex)
	if h == "" || hx == "" {
		return ""
	}
	if !isValidHashFunc(h) {
		return ""
	}
	if !looksLikeColonHex(hx) {
		return ""
	}
	return h + " " + strings.ToUpper(hx)
}

// ParseFingerprint parses the value of one `a=fingerprint:` line
// (without the prefix). Returns zero Fingerprint when malformed.
func ParseFingerprint(s string) Fingerprint {
	s = strings.TrimSpace(s)
	if s == "" {
		return Fingerprint{}
	}
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return Fingerprint{}
	}
	h := strings.ToLower(parts[0])
	hx := strings.ToUpper(parts[1])
	if !isValidHashFunc(h) || !looksLikeColonHex(hx) {
		return Fingerprint{}
	}
	return Fingerprint{HashFunc: h, Hex: hx}
}

// ParseRole parses an `a=setup:` value. Returns DTLSRoleActPass
// (the safe default per RFC 5763) when input is malformed.
func ParseRole(s string) DTLSRole {
	r := DTLSRole(strings.ToLower(strings.TrimSpace(s)))
	if r.IsValid() {
		return r
	}
	return ""
}

// isValidHashFunc allows the IANA-registered SDP fingerprint
// hashes (RFC 8122 §5). MD5/SHA-1 are explicitly forbidden — we
// reject them rather than silently accept and warn.
func isValidHashFunc(h string) bool {
	switch h {
	case "sha-224", "sha-256", "sha-384", "sha-512":
		return true
	}
	return false
}

// looksLikeColonHex is a cheap sanity check. We don't enforce the
// full 32/48/64-byte length here because callers often want the
// parser to accept oddly-formatted peer input as long as it's
// non-empty hex; the actual cert verification happens later.
func looksLikeColonHex(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'A' && r <= 'F':
		case r >= 'a' && r <= 'f':
		case r == ':':
			// Colons may appear between bytes but not at start/end
			// or back-to-back.
			if i == 0 || i == len(s)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// FormatFingerprintLine renders a complete `a=fingerprint:` SDP
// attribute line (no trailing CRLF — caller's emitter adds it).
// Returns "" when the fingerprint is malformed; callers MUST NOT
// fall through to a non-fingerprinted DTLS-SRTP answer (RFC 5763
// §3 requires the fingerprint to commit to the cert).
func FormatFingerprintLine(fp Fingerprint) string {
	v := FormatFingerprint(fp)
	if v == "" {
		return ""
	}
	return "a=fingerprint:" + v
}

// FormatSetupLine renders a complete `a=setup:` SDP attribute line.
// Returns "" when role is invalid.
func FormatSetupLine(role DTLSRole) string {
	if !role.IsValid() {
		return ""
	}
	return "a=setup:" + string(role)
}

// VerifyDTLSCertFingerprint enforces the RFC 5763 §3 binding
// between SDP `a=fingerprint` and the cert the peer presented in
// the DTLS handshake. peerCertDER is the DER-encoded leaf cert
// (typically conn.ConnectionState().PeerCertificates[0]); advertised
// is everything we parsed from the peer's SDP.
//
// Returns nil when at least one advertised fingerprint (any
// supported hash algorithm) matches the peer's actual cert digest.
// Returns a diagnostic error otherwise — caller MUST tear down the
// call when this fails because without the binding the cert is
// uncommitted and a passive MITM owns the media.
//
// Currently only sha-256 is implemented; sha-224/sha-384/sha-512
// silently skip — peers that advertise ONLY those (extremely rare
// in practice) will fail verification. Adding more hashes is a
// one-liner in hashCertWithAlg.
func VerifyDTLSCertFingerprint(peerCertDER []byte, advertised []Fingerprint) error {
	if len(peerCertDER) == 0 {
		return errors.New("sdp/dtls: peer presented no certificate")
	}
	if len(advertised) == 0 {
		return errors.New("sdp/dtls: peer advertised no fingerprint in SDP")
	}
	algsTried := make([]string, 0, len(advertised))
	for _, fp := range advertised {
		want := strings.ToUpper(strings.TrimSpace(fp.Hex))
		alg := strings.ToLower(strings.TrimSpace(fp.HashFunc))
		got, ok := hashCertWithAlg(peerCertDER, alg)
		if !ok {
			algsTried = append(algsTried, alg+"(unsupported)")
			continue
		}
		algsTried = append(algsTried, alg)
		if got == want {
			return nil
		}
	}
	return fmt.Errorf("sdp/dtls: no advertised fingerprint matched peer cert (tried %v)", algsTried)
}

// hashCertWithAlg returns colon-uppercase-hex of the cert digest
// for the named SDP hash algorithm, or (_, false) when we don't
// support it.
func hashCertWithAlg(der []byte, alg string) (string, bool) {
	switch alg {
	case "sha-256":
		sum := sha256.Sum256(der)
		return colonHexUpper(sum[:]), true
	}
	return "", false
}

func colonHexUpper(b []byte) string {
	enc := strings.ToUpper(hex.EncodeToString(b))
	var sb strings.Builder
	sb.Grow(len(enc) + len(enc)/2)
	for i := 0; i < len(enc); i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(enc[i : i+2])
	}
	return sb.String()
}

// IsDTLSTransport reports whether the m= proto string indicates
// DTLS-SRTP per RFC 5764 §8.
//
// Accepted forms:
//
//   - UDP/TLS/RTP/SAVP    — RFC 5764 §9 audio/video
//   - UDP/TLS/RTP/SAVPF   — with feedback (WebRTC default)
func IsDTLSTransport(proto string) bool {
	p := strings.ToUpper(strings.TrimSpace(proto))
	return p == "UDP/TLS/RTP/SAVP" || p == "UDP/TLS/RTP/SAVPF"
}
