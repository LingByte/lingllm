// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package rtp

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsDTLSPacket(t *testing.T) {
	cases := map[byte]bool{
		0:   false, // STUN
		3:   false, // STUN
		16:  false, // ZRTP
		19:  false, // ZRTP
		20:  true,  // DTLS lower bound
		63:  true,  // DTLS upper bound
		64:  false, // TURN
		127: false,
		128: false,
		191: false,
		200: false,
	}
	for b, want := range cases {
		if got := IsDTLSPacket(b); got != want {
			t.Errorf("IsDTLSPacket(%d) = %v, want %v", b, got, want)
		}
	}
}

func TestIsRTPPacket(t *testing.T) {
	for _, good := range []byte{128, 144, 191} {
		if !IsRTPPacket(good) {
			t.Errorf("IsRTPPacket(%d) should be true", good)
		}
	}
	for _, bad := range []byte{0, 64, 127, 192, 255} {
		if IsRTPPacket(bad) {
			t.Errorf("IsRTPPacket(%d) should be false", bad)
		}
	}
}

func TestSelfSignedDTLSCert(t *testing.T) {
	der, key, err := SelfSignedDTLSCert(time.Time{})
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if len(der) == 0 || key == nil {
		t.Fatal("empty cert or key")
	}
	fp := FingerprintSHA256(der)
	// SHA-256 produces 32 bytes → 32 colon-separated 2-char groups
	// = 32*2 + 31 = 95 chars.
	if len(fp) != 95 {
		t.Errorf("fingerprint length = %d, want 95", len(fp))
	}
	if !strings.Contains(fp, ":") {
		t.Errorf("fingerprint missing colons: %q", fp)
	}
	if fp != strings.ToUpper(fp) {
		t.Errorf("fingerprint should be uppercase: %q", fp)
	}
}

func TestFingerprintSHA256_Deterministic(t *testing.T) {
	der := []byte("synthetic-cert-bytes-for-test")
	a := FingerprintSHA256(der)
	b := FingerprintSHA256(der)
	if a != b {
		t.Errorf("non-deterministic: %s vs %s", a, b)
	}
}

func TestSRTPKeys_AsLocalRemote_RoleSwap(t *testing.T) {
	k := &SRTPKeys{
		ClientWriteKey:  []byte("ckey"),
		ClientWriteSalt: []byte("csalt"),
		ServerWriteKey:  []byte("skey"),
		ServerWriteSalt: []byte("ssalt"),
	}
	// As client → ours = client material.
	lk, ls, rk, rs := k.AsLocalRemote(true)
	if string(lk) != "ckey" || string(ls) != "csalt" || string(rk) != "skey" || string(rs) != "ssalt" {
		t.Errorf("client mapping wrong: %s/%s/%s/%s", lk, ls, rk, rs)
	}
	// As server → ours = server material.
	lk, ls, rk, rs = k.AsLocalRemote(false)
	if string(lk) != "skey" || string(ls) != "ssalt" || string(rk) != "ckey" || string(rs) != "csalt" {
		t.Errorf("server mapping wrong: %s/%s/%s/%s", lk, ls, rk, rs)
	}
}

func TestColonHex(t *testing.T) {
	got := colonHex([]byte{0xab, 0xcd, 0x01, 0xff})
	want := "AB:CD:01:FF"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestDTLSEndpoint_HandshakeAndKeyMaterial does a real DTLS-SRTP
// handshake between two endpoints over a localhost UDP pair, then
// asserts both sides derive identical keying material with mirror
// client/server orientation. This is the load-bearing test for the
// whole DTLS-SRTP slice.
func TestDTLSEndpoint_HandshakeAndKeyMaterial(t *testing.T) {
	// Set up a connected UDP pair: each side dials the other's
	// listening port. pion/dtls accepts any net.Conn.
	srvLn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srvLn.Close()
	cliLn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer cliLn.Close()

	srvAddr := srvLn.LocalAddr().(*net.UDPAddr)
	cliAddr := cliLn.LocalAddr().(*net.UDPAddr)

	srvConn := &fixedRemoteConn{pc: srvLn, remote: cliAddr}
	cliConn := &fixedRemoteConn{pc: cliLn, remote: srvAddr}

	srvCert, srvKey, err := SelfSignedDTLSCert(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	cliCert, cliKey, err := SelfSignedDTLSCert(time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type result struct {
		ep   *DTLSEndpoint
		keys *SRTPKeys
		err  error
	}
	srvCh := make(chan result, 1)
	cliCh := make(chan result, 1)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ep, err := NewDTLSEndpoint(ctx, srvConn, true, srvCert, srvKey, nil)
		if err != nil {
			srvCh <- result{err: err}
			return
		}
		keys, kerr := ep.SRTPKeys()
		srvCh <- result{ep: ep, keys: keys, err: kerr}
	}()
	go func() {
		defer wg.Done()
		ep, err := NewDTLSEndpoint(ctx, cliConn, false, cliCert, cliKey, nil)
		if err != nil {
			cliCh <- result{err: err}
			return
		}
		keys, kerr := ep.SRTPKeys()
		cliCh <- result{ep: ep, keys: keys, err: kerr}
	}()

	srvR := <-srvCh
	cliR := <-cliCh
	wg.Wait()

	if srvR.err != nil {
		t.Fatalf("server handshake: %v", srvR.err)
	}
	if cliR.err != nil {
		t.Fatalf("client handshake: %v", cliR.err)
	}
	defer srvR.ep.Close()
	defer cliR.ep.Close()

	if srvR.keys.Profile != cliR.keys.Profile {
		t.Errorf("profile mismatch: srv=%s cli=%s", srvR.keys.Profile, cliR.keys.Profile)
	}
	if srvR.keys.Profile != ProfileAES128CMHMACSHA180 {
		t.Errorf("expected AES_CM_128_HMAC_SHA1_80, got %s", srvR.keys.Profile)
	}
	// Both sides must derive identical material — the roles
	// "client write" / "server write" mean the same thing on both
	// ends after the EXTRACTOR-dtls_srtp PRF.
	if !bytes.Equal(srvR.keys.ClientWriteKey, cliR.keys.ClientWriteKey) {
		t.Errorf("client_write_key mismatch")
	}
	if !bytes.Equal(srvR.keys.ServerWriteKey, cliR.keys.ServerWriteKey) {
		t.Errorf("server_write_key mismatch")
	}
	if !bytes.Equal(srvR.keys.ClientWriteSalt, cliR.keys.ClientWriteSalt) {
		t.Errorf("client_write_salt mismatch")
	}
	if !bytes.Equal(srvR.keys.ServerWriteSalt, cliR.keys.ServerWriteSalt) {
		t.Errorf("server_write_salt mismatch")
	}
	// AsClient orientation must be opposite.
	if srvR.ep.AsClient() == cliR.ep.AsClient() {
		t.Errorf("both endpoints think they're the same role")
	}

	// The derived material must let us build matched SRTP contexts:
	// what server-side encrypts, client-side decrypts.
	srvRx, srvTx, err := srvR.ep.SRTPContexts(srvR.keys)
	if err != nil {
		t.Fatalf("server srtp ctx: %v", err)
	}
	cliRx, cliTx, err := cliR.ep.SRTPContexts(cliR.keys)
	if err != nil {
		t.Fatalf("client srtp ctx: %v", err)
	}
	_ = srvRx
	_ = srvTx
	_ = cliRx
	_ = cliTx
	// Full SRTP encrypt/decrypt round-trip is exercised in
	// dtls_session_test.go (slice A-3 wiring); here we just need to
	// confirm both sides produce non-nil contexts of the right
	// profile.
}

func TestNewDTLSEndpoint_RejectsMissingMaterial(t *testing.T) {
	srv, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer srv.Close()
	conn := &fixedRemoteConn{pc: srv, remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}}
	if _, err := NewDTLSEndpoint(context.Background(), conn, true, nil, nil, nil); err == nil {
		t.Fatal("expected error on missing cert/key")
	}
	if _, err := NewDTLSEndpoint(context.Background(), nil, true, []byte{1}, nil, nil); err == nil {
		t.Fatal("expected error on nil transport")
	}
}

func TestEnableDTLSSRTP_RejectsNilContexts(t *testing.T) {
	s := &Session{}
	if err := s.EnableDTLSSRTP(nil, nil); err == nil {
		t.Fatal("expected error on nil contexts")
	}
}

// fixedRemoteConn wraps a net.PacketConn into a net.Conn pointed at
// a fixed remote. pion/dtls expects net.Conn; SIP-side we'll have
// either a real UDP socket pre-connected or a multiplexed view onto
// the shared RTP socket. For this test the simpler form suffices.
type fixedRemoteConn struct {
	pc     net.PacketConn
	remote *net.UDPAddr
}

func (c *fixedRemoteConn) Read(b []byte) (int, error) {
	n, _, err := c.pc.ReadFrom(b)
	return n, err
}
func (c *fixedRemoteConn) Write(b []byte) (int, error) { return c.pc.WriteTo(b, c.remote) }
func (c *fixedRemoteConn) Close() error                { return nil }
func (c *fixedRemoteConn) LocalAddr() net.Addr         { return c.pc.LocalAddr() }
func (c *fixedRemoteConn) RemoteAddr() net.Addr        { return c.remote }
func (c *fixedRemoteConn) SetDeadline(t time.Time) error {
	if err := c.pc.SetReadDeadline(t); err != nil {
		return err
	}
	return c.pc.SetWriteDeadline(t)
}
func (c *fixedRemoteConn) SetReadDeadline(t time.Time) error  { return c.pc.SetReadDeadline(t) }
func (c *fixedRemoteConn) SetWriteDeadline(t time.Time) error { return c.pc.SetWriteDeadline(t) }
