// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package outbound

// signalingPeer is the runtime half of the outbound transport
// abstraction. Where transport.go decides "which transport for
// this leg", this file implements "actually write a SIP message
// onto the wire and route the response back".
//
// We have three concrete impls:
//
//   * udpPeer  — wraps the existing shared UDP sender. No state of
//                its own; cheap to create per-leg. RFC 3261 §18.1.
//   * tcpPeer  — owns a *net.TCPConn plus a bg goroutine reading
//                responses. Long-lived; pooled by signalingPool.
//                RFC 3261 §18.2 + RFC 5923 conn reuse.
//   * tlsPeer  — same shape as tcpPeer but on top of *tls.Conn.
//                RFC 3261 §26.2 + sips: scheme.
//
// The interface deliberately exposes a *net.UDPAddr for "remote
// address" even when the conn is TCP/TLS — this lets the existing
// outbound response path (which is heavily UDP-shaped: NAT
// fallback in handleResponse, addr.String() logging) keep working
// without a wider type rewrite. We synthesise the *net.UDPAddr
// from the TCP RemoteAddr's IP+Port.

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/sirupsen/logrus"
)

// signalingPeer is what outLeg holds instead of a raw *net.UDPAddr.
// Send is safe for concurrent calls from multiple goroutines (e.g.
// INVITE and a parallel BYE). Implementations serialise writes
// internally where the underlying transport requires it (TCP/TLS).
type signalingPeer interface {
	// Send writes one SIP message. The message is rendered via its
	// String() method; framing (CRLF + Content-Length) is already in
	// the rendered form so we don't add anything. Returns wrapped
	// errors with enough context to log.
	Send(msg *stack.Message) error

	// Remote returns the peer's network endpoint for logging and
	// NAT detection. For TCP/TLS this is a *net.UDPAddr synthesised
	// from the conn's TCP RemoteAddr — the protocol family is a
	// lie but downstream code only reads IP+Port.
	Remote() *net.UDPAddr

	// Transport reports the wire transport, used for Via header
	// rendering on the outgoing request.
	Transport() Transport

	// Close releases resources. UDP impl is a no-op; TCP/TLS shut
	// down the conn and stop the read goroutine. Idempotent.
	Close() error
}

// udpSendFunc matches the signature already exposed by
// SignalingSender.SendSIP: write a fully-rendered SIP message onto
// the shared UDP listen socket.
type udpSendFunc func(*stack.Message, *net.UDPAddr) error

// responseSink is the callback all peer impls feed received
// responses into. The Manager implements this via HandleSIPResponse;
// keeping it as a function pointer lets pool tests inject a fake.
type responseSink func(resp *stack.Message, addr *net.UDPAddr)

// ---------------------------------------------------------------------------
// UDP peer — wraps the shared UDP socket; zero owned state
// ---------------------------------------------------------------------------

type udpPeer struct {
	send udpSendFunc
	dst  *net.UDPAddr
}

func newUDPPeer(send udpSendFunc, dst *net.UDPAddr) *udpPeer {
	return &udpPeer{send: send, dst: dst}
}

func (p *udpPeer) Send(msg *stack.Message) error {
	if p == nil || p.send == nil {
		return errors.New("sip/outbound: udp peer not bound")
	}
	return p.send(msg, p.dst)
}

func (p *udpPeer) Remote() *net.UDPAddr { return p.dst }
func (p *udpPeer) Transport() Transport { return TransportUDP }
func (p *udpPeer) Close() error         { return nil }

// ---------------------------------------------------------------------------
// TCP / TLS peer — owns conn + read goroutine
// ---------------------------------------------------------------------------

// connPeer is shared between TCP and TLS — both use net.Conn semantics.
// The transport tag distinguishes for Via rendering; TLS impl just
// dials with tls.Dial instead of net.Dial.
type connPeer struct {
	transport Transport
	conn      net.Conn
	br        *bufio.Reader
	remote    *net.UDPAddr // synthesised from conn.RemoteAddr()
	sink      responseSink

	writeMu sync.Mutex // serialises Write across goroutines

	closeOnce sync.Once
	closed    int32
	doneCh    chan struct{}
	onClosed  func() // pool eviction hook; called once when conn is fully closed
}

// newConnPeer wraps a freshly-dialled net.Conn (TCP or TLS) and
// starts the response-reading goroutine. onClosed runs when the
// conn dies (read EOF, write error, explicit Close). The pool uses
// it to evict the entry from the conn map.
func newConnPeer(transport Transport, conn net.Conn, sink responseSink, onClosed func()) *connPeer {
	if !transport.IsConnectionOriented() {
		// Programming error — caller should have used newUDPPeer.
		conn.Close()
		return nil
	}
	rem := remoteAsUDPAddr(conn.RemoteAddr())
	cp := &connPeer{
		transport: transport,
		conn:      conn,
		br:        bufio.NewReader(conn),
		remote:    rem,
		sink:      sink,
		doneCh:    make(chan struct{}),
		onClosed:  onClosed,
	}
	go cp.readLoop()
	return cp
}

func (p *connPeer) Send(msg *stack.Message) error {
	if p == nil {
		return errors.New("sip/outbound: nil conn peer")
	}
	if atomic.LoadInt32(&p.closed) != 0 {
		return fmt.Errorf("sip/outbound: %s peer closed", p.transport)
	}
	raw := msg.String()
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	// Per-write deadline: if the conn's write side stalls (carrier
	// pause), we don't want the SIP transaction to hang forever.
	// 5s is plenty for one SIP message even on a saturated link.
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = p.conn.SetWriteDeadline(time.Time{}) }()
	if _, err := p.conn.Write([]byte(raw)); err != nil {
		// Treat any write error as fatal for this conn; the read
		// goroutine will pick up EOF shortly and clean up.
		p.shutdown()
		return fmt.Errorf("sip/outbound: %s write: %w", p.transport, err)
	}
	return nil
}

func (p *connPeer) Remote() *net.UDPAddr { return p.remote }
func (p *connPeer) Transport() Transport { return p.transport }

func (p *connPeer) Close() error {
	p.shutdown()
	<-p.doneCh
	return nil
}

func (p *connPeer) shutdown() {
	p.closeOnce.Do(func() {
		atomic.StoreInt32(&p.closed, 1)
		_ = p.conn.Close()
	})
}

// readLoop serves one SIP message at a time off the conn until the
// remote half-closes / errors. RFC 3261 §18.2 framing is via
// Content-Length, which stack.ReadMessage already implements.
func (p *connPeer) readLoop() {
	defer func() {
		close(p.doneCh)
		if p.onClosed != nil {
			p.onClosed()
		}
	}()
	for {
		// 90s read deadline doubles as carrier-side keep-alive
		// detection; Real RFC 5626 keep-alive (CRLF ping) is a
		// follow-up. If the remote idles longer than this, we evict
		// the conn — next outbound uses a fresh dial. Cheap.
		_ = p.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		msg, err := stack.ReadMessage(p.br)
		if err != nil {
			if atomic.LoadInt32(&p.closed) == 0 && !isClosedConnErr(err) && !errors.Is(err, io.EOF) {
				logrus.WithFields(logrus.Fields{
					"transport": string(p.transport),
					"remote":    p.remote.String(),
					"error":     err,
				}).Debug("sip outbound conn read ended")
			}
			p.shutdown()
			return
		}
		if msg == nil {
			continue
		}
		// Outbound conn sees only responses. If we ever got a request
		// here (mid-dialog from peer) we ignore — current outbound
		// architecture has no in-dialog UAS handling. That's a known
		// gap (1C-2 follow-up for UAC refresher acceptance).
		if msg.IsRequest {
			logrus.WithFields(logrus.Fields{
				"method":    msg.Method,
				"transport": string(p.transport),
				"remote":    p.remote.String(),
			}).Debug("sip outbound conn ignored mid-dialog request")
			continue
		}
		if p.sink != nil {
			p.sink(msg, p.remote)
		}
	}
}

// remoteAsUDPAddr extracts IP+Port from a net.Addr (typically
// *net.TCPAddr) and synthesises a *net.UDPAddr. Returns nil on
// unrecognised addr types; callers should treat that as "no peer
// addr known" rather than crashing.
func remoteAsUDPAddr(a net.Addr) *net.UDPAddr {
	switch v := a.(type) {
	case *net.TCPAddr:
		return &net.UDPAddr{IP: v.IP, Port: v.Port, Zone: v.Zone}
	case *net.UDPAddr:
		return v
	}
	// tls.Conn.RemoteAddr usually returns the underlying *net.TCPAddr;
	// if it doesn't, fall back to parsing the string form.
	if a == nil {
		return nil
	}
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return nil
	}
	pn, err := strconv.Atoi(port)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	return &net.UDPAddr{IP: ip, Port: pn}
}

// isClosedConnErr distinguishes "we shut down on purpose" from real
// I/O errors so the log noise stays low. Go's net package doesn't
// export the sentinel directly; matching on the well-known string
// is the conventional workaround.
func isClosedConnErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "connection reset by peer")
}

// dialConnPeer dials a TCP or TLS conn to dst:port and returns a
// connPeer that's ready to Send. tlsCfg is nil for TCP and a fully-
// formed *tls.Config for TLS (callers provide ServerName).
func dialConnPeer(transport Transport, host string, port int, tlsCfg *tls.Config, sink responseSink, onClosed func(), dialTimeout time.Duration) (*connPeer, error) {
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	hostPort := net.JoinHostPort(host, strconv.Itoa(port))
	d := &net.Dialer{Timeout: dialTimeout}
	switch transport {
	case TransportTCP:
		conn, err := d.Dial("tcp", hostPort)
		if err != nil {
			return nil, fmt.Errorf("sip/outbound: tcp dial %s: %w", hostPort, err)
		}
		_ = setTCPKeepAlive(conn)
		return newConnPeer(TransportTCP, conn, sink, onClosed), nil
	case TransportTLS:
		conn, err := tls.DialWithDialer(d, "tcp", hostPort, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("sip/outbound: tls dial %s: %w", hostPort, err)
		}
		// Force handshake before returning so dial errors surface here
		// rather than on first write.
		if err := conn.Handshake(); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("sip/outbound: tls handshake %s: %w", hostPort, err)
		}
		_ = setTCPKeepAlive(conn.NetConn())
		return newConnPeer(TransportTLS, conn, sink, onClosed), nil
	default:
		return nil, fmt.Errorf("sip/outbound: dialConnPeer: unsupported transport %q", transport)
	}
}

// sendOnPeer writes msg via the leg's signaling peer. fallbackDst is
// used only when the peer is nil (legacy fast-path during partial
// migration) — it routes through Manager.send (UDP). Returns an
// error when both peer and fallback are unavailable.
func (leg *outLeg) sendOnPeer(msg *stack.Message, fallbackDst *net.UDPAddr) error {
	if leg == nil {
		return errors.New("sip/outbound: nil leg")
	}
	leg.peerMu.Lock()
	p := leg.peer
	leg.peerMu.Unlock()
	if p != nil {
		return p.Send(msg)
	}
	if leg.m == nil || leg.m.send == nil {
		return errors.New("sip/outbound: leg has no peer and no fallback sender")
	}
	dst := fallbackDst
	if dst == nil {
		dst = leg.dst
	}
	if dst == nil {
		return errors.New("sip/outbound: no destination for fallback send")
	}
	return leg.m.send(msg, dst)
}

// setTCPKeepAlive enables TCP-level keep-alive (15s probe) on the
// underlying socket so a NAT'd carrier-side dropping the conn shows
// up as a read error instead of zombieing forever. SIP-level keep-
// alive (CRLF ping per RFC 5626) is a separate concern.
func setTCPKeepAlive(c net.Conn) error {
	tc, ok := c.(*net.TCPConn)
	if !ok {
		return nil
	}
	if err := tc.SetKeepAlive(true); err != nil {
		return err
	}
	return tc.SetKeepAlivePeriod(15 * time.Second)
}
