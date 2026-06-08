// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package rtp

// dtls.go: DTLS-SRTP keying for media (RFC 5763 + RFC 5764).
//
// This file ships the *standalone* DTLS endpoint — handshake +
// `EXTRACTOR-dtls_srtp` keying-material derivation — along with
// helpers that produce the SDP `a=fingerprint` digest. The actual
// muxing of DTLS / RTP / RTCP on a single UDP socket (RFC 5764 §5.1)
// lives in dtls_session.go (slice A-3) — once we know what the
// public API looks like in tests, hooking into Session is mostly
// mechanical.
//
// Why not just use pion/webrtc directly? webrtc/v3 forces an ICE
// agent + interceptor stack we don't want for SIP. The dtls/v2
// dependency is already in the module graph (transitive via
// webrtc), so adopting it adds ~zero binary cost.
//
// ---- Threat model recap (why DTLS-SRTP at all) ----
//
// SDES (RFC 4568, our existing path) carries the SRTP master key in
// plaintext SDP. That's fine over SIP-TLS but disastrous over plain
// SIP/UDP. DTLS-SRTP instead proves cert ownership with a real
// handshake; the key never leaves either endpoint. The fingerprint
// in SDP commits each side to a cert, so a MITM can't substitute
// their own.
//
// The companion guarantees in the standards:
//
//   * RFC 5763 §3 — fingerprint MUST be over the cert that's
//     actually presented in the handshake.
//   * RFC 5764 §4.1 — ClientHello carries a `use_srtp` extension
//     listing acceptable SRTP profiles; server picks one.
//   * RFC 5764 §4.2 — both sides derive client_write_SRTP_master_key
//     + server_write_SRTP_master_key from the master secret using
//     `EXTRACTOR-dtls_srtp` as the TLS-PRF label.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/pion/dtls/v2"
	"github.com/pion/srtp/v2"
)

// IsDTLSPacket reports whether the first byte of a UDP datagram on
// a multiplexed RTP socket indicates DTLS (RFC 5764 §5.1.2). The
// type-byte ranges are:
//
//   - 0..3   STUN
//   - 16..19 ZRTP
//   - 20..63 DTLS  ← us
//   - 64..79 TURN  channel
//   - 128..191 RTP / RTCP
//
// Ranges are disjoint by design so a demux can route by first byte.
func IsDTLSPacket(b byte) bool {
	return b >= 20 && b <= 63
}

// IsRTPPacket reports whether the first byte indicates RTP/RTCP.
// Useful for the muxer's "default route" check.
func IsRTPPacket(b byte) bool {
	return b >= 128 && b <= 191
}

// SRTPProfile names what we negotiated in the DTLS handshake. We
// only support the two RFC 5764 §4.1.2 must-implement profiles —
// adding more (AEAD_AES_128_GCM, AEAD_AES_256_GCM) is a follow-up
// once we hit a peer that requires them.
type SRTPProfile string

const (
	// ProfileAES128CMHMACSHA180: AES_128_CM_HMAC_SHA1_80, 128-bit
	// confidentiality + 80-bit auth tag. The default everywhere.
	ProfileAES128CMHMACSHA180 SRTPProfile = "AES_CM_128_HMAC_SHA1_80"
	// ProfileAES128CMHMACSHA132: same cipher, 32-bit tag. Used by a
	// few legacy SBCs to save bandwidth on every packet.
	ProfileAES128CMHMACSHA132 SRTPProfile = "AES_CM_128_HMAC_SHA1_32"
)

// dtlsSRTPProfileMap translates the pion enum into our typed name
// (and rejects unsupported profiles up-front).
var dtlsSRTPProfileMap = map[dtls.SRTPProtectionProfile]SRTPProfile{
	dtls.SRTP_AES128_CM_HMAC_SHA1_80: ProfileAES128CMHMACSHA180,
	dtls.SRTP_AES128_CM_HMAC_SHA1_32: ProfileAES128CMHMACSHA132,
}

// SRTPKeys is the post-handshake material derived from the DTLS
// master secret per RFC 5764 §4.2. The "client" and "server"
// designations come from the handshake roles; downstream SRTP code
// uses them as "outbound from this side" / "inbound to this side"
// based on whether we drove the handshake (active = client).
type SRTPKeys struct {
	Profile         SRTPProfile
	ClientWriteKey  []byte // 16 B
	ClientWriteSalt []byte // 14 B
	ServerWriteKey  []byte // 16 B
	ServerWriteSalt []byte // 14 B
}

// AsLocalRemote returns (localKey, localSalt, remoteKey, remoteSalt)
// based on whether this side drove the handshake.
//
//   - iWasClient = true  → we are the client; clientWrite is OUR tx,
//     serverWrite is the peer's tx (our rx)
//   - iWasClient = false → roles flipped
func (k *SRTPKeys) AsLocalRemote(iWasClient bool) (localKey, localSalt, remoteKey, remoteSalt []byte) {
	if iWasClient {
		return k.ClientWriteKey, k.ClientWriteSalt, k.ServerWriteKey, k.ServerWriteSalt
	}
	return k.ServerWriteKey, k.ServerWriteSalt, k.ClientWriteKey, k.ClientWriteSalt
}

// SelfSignedDTLSCert generates a fresh ECDSA P-256 cert for one
// DTLS endpoint. RFC 5763 §6.5 says the cert SHOULD be self-signed
// because trust comes from the SDP fingerprint, not a CA — minting
// per-call certs reduces compromise blast radius.
//
// notAfter defaults to 30 days when zero. We don't need to rotate
// the cert often because each call brings its own.
func SelfSignedDTLSCert(notAfter time.Time) ([]byte, *ecdsa.PrivateKey, error) {
	if notAfter.IsZero() {
		notAfter = time.Now().Add(30 * 24 * time.Hour)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("rtp: dtls cert keygen: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serialFromTime(),
		Subject:      pkix.Name{CommonName: "lingbyte-dtls-srtp"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("rtp: dtls cert sign: %w", err)
	}
	return der, key, nil
}

// FingerprintSHA256 computes the SDP `a=fingerprint:sha-256 HEX`
// digest for a DER-encoded cert. Returns the colon-separated
// uppercase hex form (32 bytes → 95-char string).
func FingerprintSHA256(certDER []byte) string {
	sum := sha256.Sum256(certDER)
	return colonHex(sum[:])
}

func colonHex(b []byte) string {
	enc := hex.EncodeToString(b)
	enc = strings.ToUpper(enc)
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

// DTLSEndpoint wraps a single DTLS-SRTP handshake on a UDP socket.
// Construct via NewDTLSEndpoint, run Handshake (server or client),
// then call SRTPKeys to get the derived material. Close releases
// the underlying conn.
//
// This is the standalone form — when integrating with Session, we
// pass a `net.Conn` that's actually a DTLS-only multiplexed view of
// the shared RTP socket (built in dtls_session.go, slice A-3).
type DTLSEndpoint struct {
	conn      *dtls.Conn
	asClient  bool
	closeOnce func() error
}

// NewDTLSEndpoint wraps an underlying transport (any net.Conn — for
// our purposes a UDP socket already pre-set with the remote addr,
// or a packet-conn-backed pseudo-stream) and runs a DTLS handshake.
//
// Profiles are the SRTP profiles to advertise in the use_srtp
// extension (RFC 5764 §4.1.1). Empty = our default order.
//
// certDER + key identify our side; the peer commits to its own via
// the SDP fingerprint that the SIP layer validated separately
// against the cert presented in the handshake (RFC 5763 §3).
//
// asServer = true when we're the DTLS server (a=setup:passive).
func NewDTLSEndpoint(ctx context.Context, transport net.Conn, asServer bool, certDER []byte, key *ecdsa.PrivateKey, profiles []SRTPProfile) (*DTLSEndpoint, error) {
	if transport == nil {
		return nil, errors.New("rtp: nil transport")
	}
	if len(certDER) == 0 || key == nil {
		return nil, errors.New("rtp: dtls endpoint missing cert/key")
	}
	dtlsCfg := &dtls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{certDER},
			PrivateKey:  key,
		}},
		// We trust the peer because the SDP fingerprint already
		// committed them to their cert (RFC 5763 §6.5). Skip the
		// path validation that DTLS would otherwise attempt.
		InsecureSkipVerify:     true,
		ExtendedMasterSecret:   dtls.RequireExtendedMasterSecret,
		SRTPProtectionProfiles: chooseDTLSProfiles(profiles),
	}
	var (
		conn *dtls.Conn
		err  error
	)
	if asServer {
		conn, err = dtls.ServerWithContext(ctx, transport, dtlsCfg)
	} else {
		conn, err = dtls.ClientWithContext(ctx, transport, dtlsCfg)
	}
	if err != nil {
		return nil, fmt.Errorf("rtp: dtls handshake: %w", err)
	}
	return &DTLSEndpoint{
		conn:      conn,
		asClient:  !asServer,
		closeOnce: conn.Close,
	}, nil
}

// chooseDTLSProfiles maps our typed profiles to pion's enum,
// keeping declaration order. Empty input → both supported profiles
// in the priority order from RFC 5764 §4.1.2.
func chooseDTLSProfiles(profiles []SRTPProfile) []dtls.SRTPProtectionProfile {
	if len(profiles) == 0 {
		return []dtls.SRTPProtectionProfile{
			dtls.SRTP_AES128_CM_HMAC_SHA1_80,
			dtls.SRTP_AES128_CM_HMAC_SHA1_32,
		}
	}
	out := make([]dtls.SRTPProtectionProfile, 0, len(profiles))
	for _, p := range profiles {
		switch p {
		case ProfileAES128CMHMACSHA180:
			out = append(out, dtls.SRTP_AES128_CM_HMAC_SHA1_80)
		case ProfileAES128CMHMACSHA132:
			out = append(out, dtls.SRTP_AES128_CM_HMAC_SHA1_32)
		}
	}
	return out
}

// Close shuts the DTLS conn (which sends close_notify and stops
// reading). Idempotent.
func (e *DTLSEndpoint) Close() error {
	if e == nil || e.closeOnce == nil {
		return nil
	}
	err := e.closeOnce()
	e.closeOnce = nil
	return err
}

// AsClient reports whether this endpoint drove the handshake. Used
// by SRTPKeys.AsLocalRemote to swap the client/server material.
func (e *DTLSEndpoint) AsClient() bool { return e != nil && e.asClient }

// PeerCertificates returns the DER-encoded certificates that the
// peer presented during the handshake. The leaf is index 0. Used
// by callers to verify against the SDP a=fingerprint per RFC 5763
// §3 — without this check the cert is uncommitted and a passive
// MITM owns the call.
//
// Returns nil before handshake completion or after Close.
func (e *DTLSEndpoint) PeerCertificates() [][]byte {
	if e == nil || e.conn == nil {
		return nil
	}
	state := e.conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil
	}
	out := make([][]byte, len(state.PeerCertificates))
	for i, c := range state.PeerCertificates {
		out[i] = append([]byte(nil), c...)
	}
	return out
}

// SRTPKeys returns the post-handshake keying material per RFC 5764
// §4.2. Must be called after Handshake (i.e. after construction)
// and before Close.
func (e *DTLSEndpoint) SRTPKeys() (*SRTPKeys, error) {
	if e == nil || e.conn == nil {
		return nil, errors.New("rtp: nil dtls endpoint")
	}
	prof, ok := e.conn.SelectedSRTPProtectionProfile()
	if !ok {
		return nil, errors.New("rtp: dtls handshake completed without SRTP profile")
	}
	mapped, ok := dtlsSRTPProfileMap[prof]
	if !ok {
		return nil, fmt.Errorf("rtp: unsupported SRTP profile %d", prof)
	}
	// RFC 5764 §4.2: SRTP master key is 128 bits, salt is 112 bits.
	// Total 240 bits per side, 480 bits overall.
	state := e.conn.ConnectionState()
	const masterKeyLen = 16
	const masterSaltLen = 14
	material, err := state.ExportKeyingMaterial("EXTRACTOR-dtls_srtp", nil, 2*(masterKeyLen+masterSaltLen))
	if err != nil {
		return nil, fmt.Errorf("rtp: dtls export keying material: %w", err)
	}
	if len(material) != 60 {
		return nil, fmt.Errorf("rtp: dtls keying material wrong length %d", len(material))
	}
	// Layout per RFC 5764 §4.2: client_write_SRTP_master_key (16) ||
	// server_write_SRTP_master_key (16) || client_write_SRTP_master_salt (14)
	// || server_write_SRTP_master_salt (14).
	out := &SRTPKeys{
		Profile:         mapped,
		ClientWriteKey:  append([]byte(nil), material[0:16]...),
		ServerWriteKey:  append([]byte(nil), material[16:32]...),
		ClientWriteSalt: append([]byte(nil), material[32:46]...),
		ServerWriteSalt: append([]byte(nil), material[46:60]...),
	}
	return out, nil
}

// SRTPContexts builds inbound + outbound *srtp.Context from derived
// keying material, matched to this endpoint's role. Convenience for
// callers that want to enable SRTP on a Session immediately.
func (e *DTLSEndpoint) SRTPContexts(keys *SRTPKeys) (rx, tx *srtp.Context, err error) {
	if keys == nil {
		return nil, nil, errors.New("rtp: nil SRTP keys")
	}
	prof, err := pionProfileForOurName(keys.Profile)
	if err != nil {
		return nil, nil, err
	}
	localKey, localSalt, remoteKey, remoteSalt := keys.AsLocalRemote(e.AsClient())
	rx, err = srtp.CreateContext(remoteKey, remoteSalt, prof)
	if err != nil {
		return nil, nil, fmt.Errorf("rtp: srtp rx context: %w", err)
	}
	tx, err = srtp.CreateContext(localKey, localSalt, prof)
	if err != nil {
		return nil, nil, fmt.Errorf("rtp: srtp tx context: %w", err)
	}
	return rx, tx, nil
}

func pionProfileForOurName(p SRTPProfile) (srtp.ProtectionProfile, error) {
	switch p {
	case ProfileAES128CMHMACSHA180:
		return srtp.ProtectionProfileAes128CmHmacSha1_80, nil
	case ProfileAES128CMHMACSHA132:
		return srtp.ProtectionProfileAes128CmHmacSha1_32, nil
	}
	return 0, fmt.Errorf("rtp: unknown SRTP profile %q", p)
}

// serialFromTime mints a per-cert serial number. UnixNano gives
// adequate uniqueness for self-signed certs whose lifetime is one
// call.
func serialFromTime() *big.Int {
	return big.NewInt(time.Now().UnixNano())
}

// EnableDTLSSRTP installs DTLS-derived SRTP contexts onto a Session.
// Mirrors EnableSDESSRTP but takes pre-built contexts to avoid
// duplicating the role-mapping logic between the two key sources.
func (s *Session) EnableDTLSSRTP(rx, tx *srtp.Context) error {
	if s == nil {
		return fmt.Errorf("rtp: nil session")
	}
	if rx == nil || tx == nil {
		return fmt.Errorf("rtp: dtls srtp needs both rx and tx contexts")
	}
	s.srtpMu.Lock()
	s.srtpDecrypt = rx
	s.srtpEncrypt = tx
	s.srtpMu.Unlock()
	return nil
}
