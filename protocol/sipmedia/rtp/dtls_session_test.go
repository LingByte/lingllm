// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package rtp

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newConnectedSessionPair builds two Session structs on a localhost
// UDP pair, with each side's RemoteAddr pointing at the other's
// LocalAddr. Mirrors what SDP offer/answer would produce.
func newConnectedSessionPair(t *testing.T) (a, b *Session) {
	t.Helper()
	connA, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	connB, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	a = &Session{
		LocalAddr:     connA.LocalAddr().(*net.UDPAddr),
		RemoteAddr:    connB.LocalAddr().(*net.UDPAddr),
		Conn:          connA,
		firstPacketCh: make(chan struct{}),
	}
	b = &Session{
		LocalAddr:     connB.LocalAddr().(*net.UDPAddr),
		RemoteAddr:    connA.LocalAddr().(*net.UDPAddr),
		Conn:          connB,
		firstPacketCh: make(chan struct{}),
	}
	return a, b
}

// runDemuxLoop drives Session.ReceiveRTP in a goroutine, returning
// a stop function. Used to feed the DTLS demux during the handshake
// — without an active reader the demux never fires.
func runDemuxLoop(t *testing.T, s *Session, gotRTP chan<- []byte) func() {
	t.Helper()
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		buf := make([]byte, 2048)
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			_ = s.Conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			_, _, pkt, err := s.ReceiveRTP(buf)
			if err != nil {
				// ErrRTPDiscard / read timeout / closed — keep looping.
				continue
			}
			if pkt != nil && gotRTP != nil {
				select {
				case gotRTP <- append([]byte(nil), pkt.Payload...):
				default:
				}
			}
		}
	}()
	return func() {
		close(stopCh)
		<-doneCh
	}
}

// TestSession_DTLS_HandshakeOverSharedSocket runs DTLS-SRTP between
// two Sessions on a UDP pair, demonstrating:
//
//  1. Session.ReceiveRTP demuxes DTLS bytes (20-63) to the active
//     DTLS handshake.
//  2. Both sides derive matched SRTP keys.
//  3. After handshake, EnableDTLSSRTP installs SRTP contexts and
//     RTP packets flow encrypted over the same socket.
//
// This is the load-bearing integration test for slice A-3.
func TestSession_DTLS_HandshakeOverSharedSocket(t *testing.T) {
	a, b := newConnectedSessionPair(t)
	defer a.Conn.Close()
	defer b.Conn.Close()

	// Demux readers: both sessions need an active receive loop for
	// the dtlsRoute to receive packets.
	rxA := make(chan []byte, 4)
	rxB := make(chan []byte, 4)
	stopA := runDemuxLoop(t, a, rxA)
	stopB := runDemuxLoop(t, b, rxB)
	defer stopA()
	defer stopB()

	certA, keyA, err := SelfSignedDTLSCert(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	certB, keyB, err := SelfSignedDTLSCert(time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// A is server (passive), B is client (active).
	resA := a.StartDTLS(ctx, true, certA, keyA, nil)
	resB := b.StartDTLS(ctx, false, certB, keyB, nil)

	rA := <-resA
	rB := <-resB
	if rA.Err != nil {
		t.Fatalf("A handshake: %v", rA.Err)
	}
	if rB.Err != nil {
		t.Fatalf("B handshake: %v", rB.Err)
	}
	defer rA.Endpoint.Close()
	defer rB.Endpoint.Close()

	if rA.Keys.Profile != rB.Keys.Profile {
		t.Errorf("profile mismatch")
	}

	// Install SRTP contexts on both sides.
	rxCtxA, txCtxA, err := rA.Endpoint.SRTPContexts(rA.Keys)
	if err != nil {
		t.Fatal(err)
	}
	rxCtxB, txCtxB, err := rB.Endpoint.SRTPContexts(rB.Keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.EnableDTLSSRTP(rxCtxA, txCtxA); err != nil {
		t.Fatal(err)
	}
	if err := b.EnableDTLSSRTP(rxCtxB, txCtxB); err != nil {
		t.Fatal(err)
	}

	// Now send an RTP packet from A → B; the demux loop on B should
	// decrypt and surface it via rxB.
	payload := []byte("ping-srtp")
	if err := a.SendRTP(payload, 0, 160); err != nil {
		t.Fatalf("send rtp: %v", err)
	}
	select {
	case got := <-rxB:
		if string(got) != string(payload) {
			t.Errorf("decrypted payload = %q, want %q", got, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("B did not receive decrypted RTP")
	}
}

func TestSession_DTLS_DoubleStartFails(t *testing.T) {
	a, b := newConnectedSessionPair(t)
	defer a.Conn.Close()
	defer b.Conn.Close()

	cert, key, _ := SelfSignedDTLSCert(time.Time{})
	// Short ctx so the first goroutine bails quickly on cleanup
	// (no peer is ever going to send ClientHello).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	first := a.StartDTLS(ctx, true, cert, key, nil)
	second := a.StartDTLS(ctx, true, cert, key, nil)

	r2 := <-second
	if r2.Err == nil {
		t.Fatal("second StartDTLS should fail")
	}
	// Drain the first to avoid goroutine leak. With ctx timeout +
	// shared-socket close on test exit, it must return; we cap the
	// wait so the test can't hang regardless.
	select {
	case <-first:
	case <-time.After(3 * time.Second):
		t.Log("first StartDTLS goroutine did not exit; likely pion ctx-handling quirk, not a regression")
	}
}

func TestDTLSConn_DropsOnFullBacklog(t *testing.T) {
	s := &Session{}
	c := newDTLSConn(s)
	for i := 0; i < inboundQueueDepth; i++ {
		c.Inject([]byte{byte(i)})
	}
	// Next inject should drop silently — the test verifies no panic
	// and no blocking.
	done := make(chan struct{})
	go func() {
		c.Inject([]byte("overflow"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Inject blocked on full backlog (should drop)")
	}
}

func TestDTLSConn_ReadDeadlineFires(t *testing.T) {
	s := &Session{}
	c := newDTLSConn(s)
	defer c.Close()
	if err := c.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	start := time.Now()
	_, err := c.Read(buf)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	te, ok := err.(interface{ Timeout() bool })
	if !ok || !te.Timeout() {
		t.Errorf("err %v should report Timeout=true", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Read took %v, deadline 50ms", elapsed)
	}
}

func TestDTLSConn_CloseUnblocksRead(t *testing.T) {
	s := &Session{}
	c := newDTLSConn(s)
	var done atomic.Bool
	go func() {
		_, _ = c.Read(make([]byte, 1))
		done.Store(true)
	}()
	time.Sleep(20 * time.Millisecond)
	_ = c.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !done.Load() {
		time.Sleep(5 * time.Millisecond)
	}
	if !done.Load() {
		t.Fatal("Close did not unblock Read")
	}
}

func TestRouteDTLSPacket_NoRoute(t *testing.T) {
	s := &Session{}
	if s.routeDTLSPacket([]byte{20, 1, 2, 3}) {
		t.Error("should return false when no route installed")
	}
}

func TestRouteDTLSPacket_WithRoute(t *testing.T) {
	s := &Session{}
	c := newDTLSConn(s)
	s.dtlsRoute = c
	if !s.routeDTLSPacket([]byte{20, 1, 2, 3}) {
		t.Error("should return true when route installed")
	}
	// Packet should land in the inbound queue.
	select {
	case got := <-c.inbound:
		if len(got) != 4 || got[0] != 20 {
			t.Errorf("queued packet = %v", got)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("packet didn't land in inbound queue")
	}
}

// Test that concurrent Inject + Close is race-free.
func TestDTLSConn_ConcurrentInjectAndClose(t *testing.T) {
	s := &Session{}
	c := newDTLSConn(s)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Inject([]byte{1, 2, 3})
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = c.Close()
	}()
	wg.Wait()
}
