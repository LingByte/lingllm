// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package outbound

// This file contains the **transport-selection** half of the outbound
// SIP transport abstraction (RFC 3261 §18 + §26.2 SIPS, RFC 5923 conn
// reuse). The concrete net.Conn / tls.Conn lifecycle and the per-
// target connection pool live in transport_udp.go / transport_tcp.go
// / transport_tls.go (filed in a follow-up slice — see
// docs/sip_gap_analysis.md §2A-out).
//
// Why split? Selection is pure-logic and easily unit-tested. Mixing
// it with net I/O in one file makes both harder to reason about, and
// transport selection is the bit that runs on every outbound INVITE.
//
// Selection precedence (matches user product decision 2026-05-17):
//
//   1. ;transport= parameter on the Request-URI (highest priority —
//      the routing layer / SIP redirect can override per-call)
//   2. Trunk configuration's Transport field (DialTarget.Transport,
//      typically populated by the routing layer from the DB row)
//   3. Default = TransportUDP (legacy behaviour, no surprise)

import (
	"strings"
)

// Transport identifies the SIP signaling transport for an outbound
// leg. Stringer values are the lowercase RFC 3261 §7.1 form used in
// Via headers and Request-URI ;transport= parameters.
type Transport string

const (
	// TransportUDP is unreliable datagram (RFC 3261 §18.1). Default.
	TransportUDP Transport = "udp"
	// TransportTCP is connection-oriented byte stream framed by
	// Content-Length (RFC 3261 §18.2). Connection reuse follows
	// RFC 5923.
	TransportTCP Transport = "tcp"
	// TransportTLS is TCP wrapped in TLS, paired with the SIPS URI
	// scheme on Contact / Via (RFC 3261 §26.2.1).
	TransportTLS Transport = "tls"
	// TransportUnset means "no preference" — selection falls through
	// to the next precedence layer or to TransportUDP.
	TransportUnset Transport = ""
)

// IsValid reports whether t is one of the recognised transports.
// TransportUnset is NOT valid (caller should resolve before calling).
func (t Transport) IsValid() bool {
	switch t {
	case TransportUDP, TransportTCP, TransportTLS:
		return true
	}
	return false
}

// IsTLS reports whether the transport requires TLS handshaking. The
// caller should also use the `sips:` URI scheme in Contact / Via
// when this returns true (RFC 3261 §26.2.1).
func (t Transport) IsTLS() bool { return t == TransportTLS }

// IsConnectionOriented reports whether the transport requires
// per-target conn lifecycle management (TCP/TLS) versus the
// shared-socket model (UDP).
func (t Transport) IsConnectionOriented() bool {
	return t == TransportTCP || t == TransportTLS
}

// ViaToken returns the Via header transport token, e.g.
// "SIP/2.0/UDP". Always uppercase per RFC 3261 §7.1.
func (t Transport) ViaToken() string {
	switch t {
	case TransportUDP:
		return "SIP/2.0/UDP"
	case TransportTCP:
		return "SIP/2.0/TCP"
	case TransportTLS:
		return "SIP/2.0/TLS"
	default:
		return "SIP/2.0/UDP"
	}
}

// parseTransportToken normalises a single ;transport= value. Empty
// or unrecognised input → TransportUnset (so the caller can fall
// through to the next precedence layer).
func parseTransportToken(s string) Transport {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "udp":
		return TransportUDP
	case "tcp":
		return TransportTCP
	case "tls":
		return TransportTLS
	}
	return TransportUnset
}

// transportFromRequestURI extracts the ;transport= parameter from a
// SIP Request-URI. Examples it must handle:
//
//	sip:user@host:port;transport=tcp
//	sip:user@host:port;transport=tls;lr
//	sip:user@host:port;lr;transport=TCP   (case insensitive)
//	sips:user@host:port                   (no transport= → infer TLS via scheme)
//	sip:user@host:port                    (none → Unset)
//
// We do NOT do full RFC 3261 URI parsing; just look for ;transport=
// or sips: scheme. Bad URIs return Unset.
func transportFromRequestURI(uri string) Transport {
	u := strings.TrimSpace(uri)
	if u == "" {
		return TransportUnset
	}
	low := strings.ToLower(u)

	// SIPS scheme implies TLS unless explicitly overridden by
	// ;transport=. Per RFC 3261 §26.2.2 sips: MUST be TLS, so we
	// would reject explicit ;transport=udp, but stay lenient here
	// and let the explicit param win (caller's problem).
	sipsScheme := strings.HasPrefix(low, "sips:")

	// Find ;transport=...
	const key = ";transport="
	if i := strings.Index(low, key); i >= 0 {
		rest := low[i+len(key):]
		// Param value runs up to next ';' or end-of-string.
		end := strings.IndexByte(rest, ';')
		if end < 0 {
			end = len(rest)
		}
		if t := parseTransportToken(rest[:end]); t.IsValid() {
			return t
		}
	}
	if sipsScheme {
		return TransportTLS
	}
	return TransportUnset
}

// ResolveTransport applies the precedence rules above:
//
//	URI param > target.Transport (trunk config) > TransportUDP
//
// Always returns a valid (non-Unset) transport.
func ResolveTransport(target DialTarget) Transport {
	if t := transportFromRequestURI(target.RequestURI); t.IsValid() {
		return t
	}
	if target.Transport.IsValid() {
		return target.Transport
	}
	return TransportUDP
}
