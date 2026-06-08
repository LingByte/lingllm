package rtp

import (
	"fmt"

	"github.com/pion/srtp/v2"
)

// EnableSDESSRTP is a backwards-compatible shim that defaults to
// AES_CM_128_HMAC_SHA1_80. New callers should use
// EnableSDESSRTPWithProfile so they can carry the negotiated suite
// (RFC 3711 supports multiple auth tag lengths in addition to _80).
func (s *Session) EnableSDESSRTP(peerOutboundKey, peerOutboundSalt, localOutboundKey, localOutboundSalt []byte) error {
	return s.EnableSDESSRTPWithProfile(
		srtp.ProtectionProfileAes128CmHmacSha1_80,
		peerOutboundKey, peerOutboundSalt, localOutboundKey, localOutboundSalt,
	)
}

// EnableSDESSRTPWithProfile configures SRTP using RFC 4568 SDES
// material with an explicit pion protection profile so callers
// that negotiated AES_CM_128_HMAC_SHA1_32 (Cisco / Avaya interop)
// don't silently fall back to _80 and end up with auth-tag length
// mismatches on the wire.
//
//   - rx context: decrypts the peer's outbound RTP (their tx → our rx)
//   - tx context: encrypts our outbound RTP (our tx → their rx)
//
// Both contexts MUST use the same profile — RFC 4568 §6.1 forbids
// asymmetric suites within one m=audio block.
func (s *Session) EnableSDESSRTPWithProfile(prof srtp.ProtectionProfile,
	peerOutboundKey, peerOutboundSalt, localOutboundKey, localOutboundSalt []byte) error {
	if s == nil {
		return fmt.Errorf("rtp: nil session")
	}
	rx, err := srtp.CreateContext(peerOutboundKey, peerOutboundSalt, prof)
	if err != nil {
		return fmt.Errorf("rtp: srtp rx context: %w", err)
	}
	tx, err := srtp.CreateContext(localOutboundKey, localOutboundSalt, prof)
	if err != nil {
		return fmt.Errorf("rtp: srtp tx context: %w", err)
	}
	s.srtpMu.Lock()
	s.srtpDecrypt = rx
	s.srtpEncrypt = tx
	s.srtpMu.Unlock()
	return nil
}
