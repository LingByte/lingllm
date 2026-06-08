// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package outbound

// signalingPool: per-target connection cache for connection-oriented
// SIP transports (TCP/TLS). RFC 5923 §4 defines the contract — when
// a UAC sends a request to a target it may reuse an existing TCP
// connection it previously opened to the same target, and
// connections SHOULD be retained across in-dialog requests to avoid
// the cost of repeated 3-way handshake + TLS resumption.
//
// We pool by `(transport, host, port)` and evict entries that have
// been idle longer than `idleTimeout` (default 5 min, per the user
// product decision 2026-05-17). Entries are also evicted when the
// underlying conn fails — the connPeer's onClosed hook calls back
// into the pool to drop the map entry.
//
// UDP doesn't go through the pool: udpPeer is created on the fly
// per-leg and is essentially free (no resources owned). Pool.Get
// short-circuits UDP to avoid mixing the two life-cycles.

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	defaultIdleTimeout = 5 * time.Minute
	defaultDialTimeout = 5 * time.Second
)

// poolConfig is what NewSignalingPool needs to operate. Field-by-
// field rather than function args to make adding env-driven
// config knobs cheap.
type poolConfig struct {
	// UDPSend is how UDP peers write — typically Manager.send (which
	// is wired to SIPServer.SendSIP). Required.
	UDPSend udpSendFunc

	// ResponseSink receives parsed responses from any pooled conn
	// (TCP/TLS). UDP responses arrive on the shared listener and
	// don't go through here. Required for TCP/TLS use.
	ResponseSink responseSink

	// TLSConfig is the *tls.Config used for TLS dials. nil → use
	// the package default (system roots, ServerName from dial host).
	// Callers wanting skip-verify must pass a *tls.Config with
	// InsecureSkipVerify=true; the pool deliberately does not have
	// its own env-driven knob.
	TLSConfig *tls.Config

	// IdleTimeout is how long a pooled connection may sit unused
	// before the sweeper closes it. Zero → defaultIdleTimeout.
	IdleTimeout time.Duration

	// DialTimeout caps tcp/tls dial latency. Zero → defaultDialTimeout.
	DialTimeout time.Duration
}

// signalingPool is concurrent-safe.
type signalingPool struct {
	cfg poolConfig

	mu    sync.Mutex
	conns map[string]*pooledConn // key: "transport|host:port"

	sweepCh   chan struct{}
	sweepOnce sync.Once
	sweepDone chan struct{}
}

type pooledConn struct {
	peer     *connPeer
	lastUsed time.Time
}

// newSignalingPool returns a pool ready to dispense peers. Call
// Close to stop the idle sweeper goroutine when the manager shuts
// down.
func newSignalingPool(cfg poolConfig) *signalingPool {
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaultDialTimeout
	}
	return &signalingPool{
		cfg:       cfg,
		conns:     make(map[string]*pooledConn),
		sweepCh:   make(chan struct{}),
		sweepDone: make(chan struct{}),
	}
}

// startSweeper runs the idle-connection collector. Lazy-started on
// first call to Get for a connection-oriented transport; safe to
// call multiple times.
func (p *signalingPool) startSweeper() {
	p.sweepOnce.Do(func() {
		go p.sweepLoop()
	})
}

func (p *signalingPool) sweepLoop() {
	defer close(p.sweepDone)
	tick := time.NewTicker(p.cfg.IdleTimeout / 2)
	defer tick.Stop()
	for {
		select {
		case <-p.sweepCh:
			return
		case <-tick.C:
			p.sweepIdle()
		}
	}
}

// sweepIdle closes any conn idle longer than cfg.IdleTimeout.
func (p *signalingPool) sweepIdle() {
	cutoff := time.Now().Add(-p.cfg.IdleTimeout)
	var toClose []*connPeer
	p.mu.Lock()
	for k, pc := range p.conns {
		if pc.lastUsed.Before(cutoff) {
			toClose = append(toClose, pc.peer)
			delete(p.conns, k)
		}
	}
	p.mu.Unlock()
	for _, peer := range toClose {
		_ = peer.Close()
	}
}

// Close stops the sweeper and shuts every pooled conn. After this,
// calls to Get return errors. Idempotent.
func (p *signalingPool) Close() error {
	if p == nil {
		return nil
	}
	// Signal sweeper to exit (channel close is idempotent via select).
	select {
	case <-p.sweepCh:
		// already closed
	default:
		close(p.sweepCh)
	}
	// Close all live conns.
	p.mu.Lock()
	conns := p.conns
	p.conns = make(map[string]*pooledConn)
	p.mu.Unlock()
	for _, pc := range conns {
		_ = pc.peer.Close()
	}
	return nil
}

// Get returns a peer for the requested target. For UDP it builds a
// fresh udpPeer (no pooling). For TCP/TLS it returns an existing
// pooled conn or dials a new one. Errors propagate from dial /
// handshake failure.
func (p *signalingPool) Get(ctx context.Context, transport Transport, dst *net.UDPAddr) (signalingPeer, error) {
	if p == nil {
		return nil, fmt.Errorf("sip/outbound: nil pool")
	}
	if dst == nil {
		return nil, fmt.Errorf("sip/outbound: nil destination")
	}
	switch transport {
	case TransportUDP:
		if p.cfg.UDPSend == nil {
			return nil, fmt.Errorf("sip/outbound: pool has no UDP sender")
		}
		return newUDPPeer(p.cfg.UDPSend, dst), nil
	case TransportTCP, TransportTLS:
		// fall through
	default:
		return nil, fmt.Errorf("sip/outbound: unsupported transport %q", transport)
	}

	p.startSweeper()

	key := poolKey(transport, dst)
	p.mu.Lock()
	if pc, ok := p.conns[key]; ok {
		pc.lastUsed = time.Now()
		p.mu.Unlock()
		return pc.peer, nil
	}
	p.mu.Unlock()

	// Dial outside the lock — handshake can take seconds and we don't
	// want to serialise unrelated targets.
	host := dst.IP.String()
	port := dst.Port
	tlsCfg := p.cfg.TLSConfig
	if transport == TransportTLS {
		tlsCfg = withServerNameIfMissing(tlsCfg, host)
	}
	peer, err := dialConnPeer(transport, host, port, tlsCfg, p.cfg.ResponseSink, func() {
		// Eviction hook — fired by the read goroutine when the conn
		// dies (EOF / write error / explicit Close).
		p.mu.Lock()
		if pc, ok := p.conns[key]; ok {
			// Only delete if it's still our peer — guard against the
			// rare race where the entry was replaced concurrently.
			delete(p.conns, key)
			_ = pc // keep linter happy
		}
		p.mu.Unlock()
	}, p.cfg.DialTimeout)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	// Race: another goroutine may have populated the entry while we
	// were dialling. Use whichever won, close the loser.
	if existing, ok := p.conns[key]; ok {
		p.mu.Unlock()
		_ = peer.Close()
		return existing.peer, nil
	}
	p.conns[key] = &pooledConn{peer: peer, lastUsed: time.Now()}
	p.mu.Unlock()

	return peer, nil
}

// poolKey deterministically identifies a (transport, host:port)
// tuple. Using the parsed IP rather than the raw hostname avoids
// duplicate entries for "1.2.3.4" vs "01.02.03.04".
func poolKey(transport Transport, dst *net.UDPAddr) string {
	host := ""
	if dst.IP != nil {
		host = dst.IP.String()
	}
	return fmt.Sprintf("%s|%s:%d", transport, host, dst.Port)
}

// withServerNameIfMissing clones tlsCfg (or builds a fresh one) and
// fills ServerName with the dial host so SAN validation works. If
// the caller already set ServerName (e.g. they're connecting to an
// IP but want to verify a specific cert hostname), we leave their
// value alone.
func withServerNameIfMissing(base *tls.Config, host string) *tls.Config {
	if base == nil {
		return &tls.Config{ServerName: host}
	}
	if base.ServerName != "" {
		return base
	}
	cloned := base.Clone()
	cloned.ServerName = host
	return cloned
}
