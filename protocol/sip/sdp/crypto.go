package sdp

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/pion/srtp/v2"
)

// CryptoOffer is one a=crypto line from SDP (RFC 4568), scoped to the current audio m-line.
type CryptoOffer struct {
	Tag       uint32
	Suite     string
	KeyParams string // raw key-params field (e.g. "inline:...")
}

const (
	// SuiteAESCM128HMACSHA180 is the common SDES suite string for SRTP (RFC 4568).
	// 128-bit AES-CM confidentiality + 80-bit HMAC-SHA1 auth tag — the
	// default everywhere. WebRTC, Asterisk, FreeSWITCH all default to this.
	SuiteAESCM128HMACSHA180 = "AES_CM_128_HMAC_SHA1_80"
	// SuiteAESCM128HMACSHA132 is the bandwidth-saving variant: same cipher
	// + key length, but 32-bit auth tag (RFC 4568 / RFC 3711 §4.2.1).
	// Common on Cisco CUCM and some Avaya softphones — interop requires
	// us to accept it on offer and produce it on answer when the peer
	// didn't list HMAC_SHA1_80.
	SuiteAESCM128HMACSHA132 = "AES_CM_128_HMAC_SHA1_32"
)

// PionProfileForSuite maps an SDES suite string to the pion/srtp
// profile enum. Returns ok=false when unsupported.
//
// The two AES_CM_128 suites use identical key/salt lengths (16 / 14
// bytes — RFC 3711 §4.1.1); only the auth tag length differs. So
// SDP key-material parsing/rendering is suite-agnostic; only the
// SRTP context creation needs the right profile.
func PionProfileForSuite(suite string) (srtp.ProtectionProfile, bool) {
	switch strings.ToUpper(strings.TrimSpace(suite)) {
	case SuiteAESCM128HMACSHA180:
		return srtp.ProtectionProfileAes128CmHmacSha1_80, true
	case SuiteAESCM128HMACSHA132:
		return srtp.ProtectionProfileAes128CmHmacSha1_32, true
	}
	return 0, false
}

// DecodeSDESInline extracts SRTP master key and salt from an RFC 4568 key-params field
// whose first token is inline:BASE64... Optional lifetime/MKI suffix after '|' is ignored.
func DecodeSDESInline(keyParams string) (masterKey, masterSalt []byte, err error) {
	keyParams = strings.TrimSpace(keyParams)
	if keyParams == "" {
		return nil, nil, fmt.Errorf("sip/sdp: empty crypto key-params")
	}
	parts := strings.Fields(keyParams)
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("sip/sdp: empty crypto key-params")
	}
	inline := strings.TrimSpace(parts[0])
	if !strings.HasPrefix(strings.ToLower(inline), "inline:") {
		return nil, nil, fmt.Errorf("sip/sdp: crypto key-params missing inline:")
	}
	val := strings.TrimPrefix(inline, "inline:")
	val = strings.TrimPrefix(val, "INLINE:")
	if bar := strings.IndexByte(val, '|'); bar >= 0 {
		val = val[:bar]
	}
	raw, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return nil, nil, fmt.Errorf("sip/sdp: inline base64: %w", err)
	}
	prof := srtp.ProtectionProfileAes128CmHmacSha1_80
	kl, err := prof.KeyLen()
	if err != nil {
		return nil, nil, err
	}
	sl, err := prof.SaltLen()
	if err != nil {
		return nil, nil, err
	}
	if len(raw) < kl+sl {
		return nil, nil, fmt.Errorf("sip/sdp: inline material too short: got %d need %d", len(raw), kl+sl)
	}
	return append([]byte(nil), raw[:kl]...), append([]byte(nil), raw[kl:kl+sl]...), nil
}

// FormatCryptoLine builds one complete SDP line: a=crypto:<tag> <suite> inline:<base64(key||salt)>.
func FormatCryptoLine(tag uint32, suite string, masterKey, masterSalt []byte) (string, error) {
	suite = strings.TrimSpace(suite)
	if suite == "" {
		return "", fmt.Errorf("sip/sdp: empty suite")
	}
	prof := srtp.ProtectionProfileAes128CmHmacSha1_80
	kl, err := prof.KeyLen()
	if err != nil {
		return "", err
	}
	sl, err := prof.SaltLen()
	if err != nil {
		return "", err
	}
	if len(masterKey) != kl || len(masterSalt) != sl {
		return "", fmt.Errorf("sip/sdp: key/salt length mismatch for %s", suite)
	}
	combined := append(append([]byte(nil), masterKey...), masterSalt...)
	b64 := base64.StdEncoding.EncodeToString(combined)
	return fmt.Sprintf("a=crypto:%d %s inline:%s", tag, suite, b64), nil
}

// PickAESCM128Offer returns the first crypto offer using
// AES_CM_128_HMAC_SHA1_80, if any. Kept as a thin shim for callers
// that ONLY want the WebRTC-preferred suite — most production code
// should use PickSupportedSDESOffer instead.
func PickAESCM128Offer(offers []CryptoOffer) (CryptoOffer, bool) {
	for _, o := range offers {
		if strings.EqualFold(strings.TrimSpace(o.Suite), SuiteAESCM128HMACSHA180) {
			return o, true
		}
	}
	return CryptoOffer{}, false
}

// PickSupportedSDESOffer picks the most-preferred SDES offer this
// stack can handle from a peer's a=crypto list. Preference order
// matches our offer side:
//
//  1. AES_CM_128_HMAC_SHA1_80 — interop default, WebRTC-compatible
//  2. AES_CM_128_HMAC_SHA1_32 — Cisco / Avaya bandwidth-saving form
//
// The first match by preference wins, regardless of the order the
// peer listed them. Returns ok=false when nothing is acceptable —
// caller MUST 488 the call (we can't downgrade a SAVP m= block to
// plain RTP per RFC 4568 §6.2).
func PickSupportedSDESOffer(offers []CryptoOffer) (CryptoOffer, bool) {
	prefs := []string{SuiteAESCM128HMACSHA180, SuiteAESCM128HMACSHA132}
	for _, want := range prefs {
		for _, o := range offers {
			if strings.EqualFold(strings.TrimSpace(o.Suite), want) {
				return o, true
			}
		}
	}
	return CryptoOffer{}, false
}
