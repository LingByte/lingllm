// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package rtp

// dtls_session.go: integration of DTLSEndpoint into Session — single
// UDP socket carrying DTLS handshake bytes alongside SRTP/RTP/RTCP
// per RFC 5764 §5.1.
//
// Design:
//
//   - Session.dtlsRoute is a per-call demux conn that pion/dtls
//     reads from. ReceiveRTP at the top inspects the first byte:
//     DTLS bytes (20-63) get pushed onto dtlsRoute's channel,
//     RTP bytes (128-191) take the existing path.
//
//   - dtlsConn implements net.Conn for pion/dtls. Read blocks on
//     the inbound channel; Write goes to Session.Conn → RemoteAddr.
//     Close drops the route + unblocks Read with io.EOF.
//
//   - StartDTLS orchestrates the whole thing: install the route,
//     run the handshake on a goroutine, derive SRTP keys, install
//     SRTP contexts. Caller awaits via a returned chan.
//
// Why a chan-backed conn instead of pion's mux? Their muxer requires
// adopting the whole transport stack (interceptor, ICE, etc). For
// SIP we own both ends and the demux is dirt-simple: 30 lines vs
// pulling in the WebRTC dep tree.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"github.com/LingByte/lingllm/protocol/sipmedia/internal/siplog"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// dtlsConn is a net.Conn fed by Session's UDP demux on the read
// side and writing directly to Session.Conn on the write side.
type dtlsConn struct {
	sess    *Session
	inbound chan []byte
	closeCh chan struct{}
	closed  atomic.Bool

	// readDeadline / writeDeadline are honoured by Read/Write to
	// match what pion/dtls expects from net.Conn.
	deadlineMu    sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

// inboundQueueDepth caps the demux backlog. DTLS handshake exchanges
// ≤8 packets per side, so 32 leaves headroom for retransmits without
// any risk of memory blow-up if pion stalls.
const inboundQueueDepth = 32

func newDTLSConn(s *Session) *dtlsConn {
	return &dtlsConn{
		sess:    s,
		inbound: make(chan []byte, inboundQueueDepth),
		closeCh: make(chan struct{}),
	}
}

// Inject is called from Session.ReceiveRTP when a DTLS-typed packet
// arrives. Non-blocking: drops the packet on full backlog (DTLS
// retransmits will recover).
func (c *dtlsConn) Inject(p []byte) {
	if c == nil || c.closed.Load() {
		return
	}
	// Copy because Session reuses its read buffer.
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case c.inbound <- cp:
	default:
		// Backlog full — drop. DTLS will retransmit.
		siplog.L.Debug("rtp dtls demux dropped packet (backlog full)")
	}
}

// Read pulls one DTLS datagram from the demux queue. Honours the
// read deadline like pion/dtls expects.
func (c *dtlsConn) Read(b []byte) (int, error) {
	if c == nil {
		return 0, errors.New("rtp/dtls: nil conn")
	}
	if c.closed.Load() {
		return 0, io.EOF
	}
	c.deadlineMu.Lock()
	dl := c.readDeadline
	c.deadlineMu.Unlock()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			return 0, &timeoutError{}
		}
		timer = time.NewTimer(d)
		timeoutCh = timer.C
		defer timer.Stop()
	}
	select {
	case p := <-c.inbound:
		return copy(b, p), nil
	case <-c.closeCh:
		return 0, io.EOF
	case <-timeoutCh:
		return 0, &timeoutError{}
	}
}

// Write sends one DTLS datagram via the shared UDP socket. The
// destination is Session.RemoteAddr (which symmetric-RTP learning
// may have updated since SDP).
func (c *dtlsConn) Write(b []byte) (int, error) {
	if c == nil || c.sess == nil || c.sess.Conn == nil {
		return 0, errors.New("rtp/dtls: nil session conn")
	}
	if c.closed.Load() {
		return 0, errors.New("rtp/dtls: conn closed")
	}
	dst := c.sess.RemoteAddr
	if dst == nil {
		return 0, errors.New("rtp/dtls: no remote address yet")
	}
	c.deadlineMu.Lock()
	wd := c.writeDeadline
	c.deadlineMu.Unlock()
	if !wd.IsZero() {
		_ = c.sess.Conn.SetWriteDeadline(wd)
		defer c.sess.Conn.SetWriteDeadline(time.Time{})
	}
	return c.sess.Conn.WriteToUDP(b, dst)
}

func (c *dtlsConn) Close() error {
	if c == nil || !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.closeCh)
	return nil
}

func (c *dtlsConn) LocalAddr() net.Addr {
	if c == nil || c.sess == nil {
		return nil
	}
	return c.sess.LocalAddr
}
func (c *dtlsConn) RemoteAddr() net.Addr {
	if c == nil || c.sess == nil {
		return nil
	}
	return c.sess.RemoteAddr
}

func (c *dtlsConn) SetDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.writeDeadline = t
	c.deadlineMu.Unlock()
	return nil
}
func (c *dtlsConn) SetReadDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.deadlineMu.Unlock()
	return nil
}
func (c *dtlsConn) SetWriteDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.writeDeadline = t
	c.deadlineMu.Unlock()
	return nil
}

// timeoutError satisfies net.Error's Timeout() bool — pion/dtls
// retries on Timeout but bails on other errors.
type timeoutError struct{}

func (timeoutError) Error() string   { return "rtp/dtls: i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// DTLSResult is what StartDTLS hands back when the handshake either
// completes successfully or fails. Either Endpoint+Keys are non-nil
// (success), or Err is non-nil (failure). Caller is responsible for
// closing the Endpoint when the call ends.
type DTLSResult struct {
	Endpoint *DTLSEndpoint
	Keys     *SRTPKeys
	Err      error
}

// StartDTLS installs a DTLS demux route on this Session, then runs
// the handshake on a goroutine. The returned channel receives the
// result exactly once. Caller passes the cert/key it committed to
// in SDP (`a=fingerprint`); peer presents its own and the SDP layer
// is responsible for verifying the cert digest matches what was
// advertised (RFC 5763 §3) — that check is wired in slice A-4
// alongside SDP offer/answer rendering.
//
// asServer = true when our SDP `a=setup` resolved to "passive".
//
// Concurrent StartDTLS calls on the same Session are an error;
// teardown happens via DTLSEndpoint.Close (the route is dropped
// from Session automatically).
func (s *Session) StartDTLS(ctx context.Context, asServer bool, certDER []byte, key *ecdsa.PrivateKey, profiles []SRTPProfile) <-chan DTLSResult {
	out := make(chan DTLSResult, 1)
	if s == nil {
		out <- DTLSResult{Err: errors.New("rtp: nil session")}
		return out
	}
	s.dtlsMu.Lock()
	if s.dtlsRoute != nil {
		s.dtlsMu.Unlock()
		out <- DTLSResult{Err: errors.New("rtp: dtls already in progress")}
		return out
	}
	conn := newDTLSConn(s)
	s.dtlsRoute = conn
	s.dtlsMu.Unlock()

	go func() {
		defer func() {
			// Always tear down the route — successful endpoints
			// own conn lifecycle through DTLSEndpoint.Close;
			// failed handshakes leave nothing usable behind.
			s.dtlsMu.Lock()
			if s.dtlsRoute == conn {
				s.dtlsRoute = nil
			}
			s.dtlsMu.Unlock()
		}()
		ep, err := NewDTLSEndpoint(ctx, conn, asServer, certDER, key, profiles)
		if err != nil {
			_ = conn.Close()
			out <- DTLSResult{Err: fmt.Errorf("rtp/dtls: handshake: %w", err)}
			return
		}
		keys, kerr := ep.SRTPKeys()
		if kerr != nil {
			_ = ep.Close()
			out <- DTLSResult{Err: fmt.Errorf("rtp/dtls: keying material: %w", kerr)}
			return
		}
		out <- DTLSResult{Endpoint: ep, Keys: keys}
	}()
	return out
}

// routeDTLSPacket is called from ReceiveRTP when the first byte
// indicates DTLS. Returns true when the packet was consumed (caller
// should treat it like a no-op read), false when there's no active
// DTLS route (caller likely got a stray packet — drop and keep
// reading).
func (s *Session) routeDTLSPacket(p []byte) bool {
	if s == nil {
		return false
	}
	s.dtlsMu.Lock()
	r := s.dtlsRoute
	s.dtlsMu.Unlock()
	if r == nil {
		return false
	}
	r.Inject(p)
	return true
}
