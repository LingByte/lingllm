// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Package identity implements RFC 3325 P-Asserted-Identity (PAI) and
// Privacy header processing for both outbound (UAC) and inbound (UAS)
// signaling paths.
//
// Why a dedicated package:
//
//   - The same parser is needed in pkg/sip/server (extract PAI from
//     inbound INVITE) and pkg/sip/outbound (emit PAI on outbound INVITE),
//     and a future pkg/sip/identity8224 extension (STIR/SHAKEN) wants
//     to chain off the same trust-domain config.
//   - PAI semantics differ from From/Contact in two non-obvious ways:
//     (1) it carries an *asserted* (carrier-validated) identity, not the
//     UAS-claimed one, and (2) by RFC 3325 §4 it MUST be stripped at
//     trust-domain boundaries unless an explicit relationship exists.
//     Centralising the validation keeps that boundary rule in one place.
//
// Out of scope here: signing the assertion (that's RFC 8224 Identity,
// batch 3B). PAI is the unsigned, hop-by-hop "trust us, we validated
// the caller" header — only safe within a trust domain.
package identity

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

// PrivacyToken values defined by RFC 3323 / RFC 3325. We expose the
// subset we actually emit / consume; "header" / "session" / "user" /
// "critical" are accepted on input but treated like "id" for the purpose
// of UI redaction.
const (
	PrivacyNone     = ""        // no Privacy header
	PrivacyID       = "id"      // hide PAI identity (RFC 3325 §7)
	PrivacyNoChange = "none"    // explicit opt-out (RFC 3323)
	PrivacyHeader   = "header"  // strip Via/Contact for privacy
	PrivacyUser     = "user"    // anonymize From
	PrivacySession  = "session" // anonymize SDP
	PrivacyCritical = "critical"
)

// Asserted is one parsed P-Asserted-Identity row. RFC 3325 allows two
// rows max: one sip:/sips: URI and one tel: URI. We keep them in URI /
// DisplayName form because that's what downstream business code wants
// to log / display / persist.
type Asserted struct {
	// URI is the bare URI without angle brackets ("sip:+8613800138000@trust.example"
	// or "tel:+8613800138000"). Empty means the header was absent or
	// stripped by the trust-domain filter.
	URI string
	// DisplayName is the optional unquoted display-name. Empty when the
	// header had no display-name part.
	DisplayName string
	// Scheme is "sip" / "sips" / "tel" for quick predicate checks; lower
	// case, never empty when URI != "".
	Scheme string
}

// IsEmpty reports whether the parsed value is meaningful.
func (a Asserted) IsEmpty() bool { return strings.TrimSpace(a.URI) == "" }

// FormatHeader renders one PAI row in wire format. Display-names are
// wrapped in quotes per RFC 3261 (RFC 3325 does not relax the BNF). Use
// FormatPAIDisplayName from the outbound package if you need MIME
// encoded-word support for non-ASCII display names; PAI is *operator to
// operator* and almost always ASCII, so we keep this minimal.
func (a Asserted) FormatHeader() string {
	u := strings.TrimSpace(a.URI)
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "<") {
		u = "<" + u + ">"
	}
	dn := strings.TrimSpace(a.DisplayName)
	if dn == "" {
		return u
	}
	// Ensure quotes around display-name; double quotes inside the
	// display-name get backslash-escaped per RFC 3261 quoted-string BNF.
	esc := strings.ReplaceAll(dn, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	return `"` + esc + `" ` + u
}

// trustDomains is the read-write cache of trusted upstream signaling
// peers from which we accept (and forward) PAI. Populated lazily from
// the SIP_PAI_TRUST_DOMAINS env var on first use.
//
// Format: comma-separated list of host:port or bare host. Empty list
// (or env unset) means "accept PAI from any peer" — appropriate for
// dev / loopback, NOT for production carrier interop where you should
// pin the carrier SBC IPs.
var (
	trustDomainsMu   sync.RWMutex
	trustDomainsList []string
	trustDomainsLoad sync.Once
)

func loadTrustDomains() {
	raw := strings.TrimSpace(os.Getenv("SIP_PAI_TRUST_DOMAINS"))
	if raw == "" {
		return
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, strings.ToLower(p))
	}
	trustDomainsMu.Lock()
	trustDomainsList = out
	trustDomainsMu.Unlock()
}

// SetTrustDomainsForTest is used by unit tests to override the env
// without touching process state. Pass nil to revert to env-driven.
func SetTrustDomainsForTest(list []string) {
	trustDomainsMu.Lock()
	if list == nil {
		trustDomainsList = nil
	} else {
		cp := make([]string, len(list))
		for i, s := range list {
			cp[i] = strings.ToLower(strings.TrimSpace(s))
		}
		trustDomainsList = cp
	}
	trustDomainsMu.Unlock()
}

// PeerIsTrusted reports whether an inbound signaling peer is in the
// trust domain. Empty allow-list => trust any peer (dev mode).
//
// We match on host alone (port stripped) because operators almost
// always rotate SBC ports while keeping the host stable. If you need
// strict host:port matching, configure with explicit ports — we still
// compare those exactly when the env value carries a port.
func PeerIsTrusted(remoteAddr *net.UDPAddr) bool {
	trustDomainsLoad.Do(loadTrustDomains)
	trustDomainsMu.RLock()
	list := trustDomainsList
	trustDomainsMu.RUnlock()
	if len(list) == 0 {
		return true
	}
	if remoteAddr == nil {
		return false
	}
	host := strings.ToLower(remoteAddr.IP.String())
	hostPort := fmt.Sprintf("%s:%d", host, remoteAddr.Port)
	for _, e := range list {
		if e == host || e == hostPort {
			return true
		}
	}
	return false
}

// ParsePAI parses one or more P-Asserted-Identity header values. RFC
// 3325 §3 allows at most two: one sip:/sips: and one tel:. We tolerate
// extras gracefully (return what we parsed; ignore the rest) instead
// of failing loud — better to surface a partial identity than reject
// the call.
//
// Multi-line input ("\r\n" separator) is the form we get from the
// stack.Message GetHeader implementation when peers fold PAI.
func ParsePAI(raw string) []Asserted {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	var out []Asserted
	for _, part := range splitHeaderRows(s) {
		a := parseOnePAIRow(part)
		if !a.IsEmpty() {
			out = append(out, a)
		}
	}
	return out
}

// splitHeaderRows splits a PAI header that may have arrived as:
//
//	"\"Alice\" <sip:alice@biz.example>, <tel:+8613800138000>"
//
// or as two separate row strings joined by "\r\n" (the stack folds
// multiple instances of the same header before handing it to us).
//
// We split on commas that are OUTSIDE angle brackets AND outside
// quoted-strings; doing this with a regex is wrong (display-names can
// contain literal commas inside quotes). Hand-rolled state machine
// instead.
func splitHeaderRows(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", ",")
	var (
		out      []string
		buf      strings.Builder
		inQuote  bool
		inAngle  bool
		escNext  bool
	)
	flush := func() {
		t := strings.TrimSpace(buf.String())
		if t != "" {
			out = append(out, t)
		}
		buf.Reset()
	}
	for _, r := range s {
		if escNext {
			buf.WriteRune(r)
			escNext = false
			continue
		}
		switch r {
		case '\\':
			if inQuote {
				escNext = true
			}
			buf.WriteRune(r)
		case '"':
			inQuote = !inQuote
			buf.WriteRune(r)
		case '<':
			if !inQuote {
				inAngle = true
			}
			buf.WriteRune(r)
		case '>':
			if !inQuote {
				inAngle = false
			}
			buf.WriteRune(r)
		case ',':
			if inQuote || inAngle {
				buf.WriteRune(r)
			} else {
				flush()
			}
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

// parseOnePAIRow extracts (DisplayName, URI, Scheme) from one row.
// Handles the four shapes RFC 3261 / RFC 3325 allow:
//
//	<sip:user@host>                                     (no display-name)
//	"Display Name" <sip:user@host>                      (quoted DN)
//	Token-Only-DN <sip:user@host>                       (token DN, ASCII only)
//	sip:user@host                                       (bare URI — RFC 3261 §20.39 ABNF
//	                                                     for addr-spec; some operators emit this)
//
// On unparseable rows we return an empty Asserted (caller drops it).
func parseOnePAIRow(row string) Asserted {
	row = strings.TrimSpace(row)
	if row == "" {
		return Asserted{}
	}
	var dn, uri string
	if i := strings.Index(row, "<"); i >= 0 {
		j := strings.Index(row[i:], ">")
		if j <= 0 {
			return Asserted{}
		}
		uri = strings.TrimSpace(row[i+1 : i+j])
		dn = strings.TrimSpace(row[:i])
		// Drop surrounding quotes on display-name, unescape \\ and \".
		if len(dn) >= 2 && dn[0] == '"' && dn[len(dn)-1] == '"' {
			dn = unquoteDisplayName(dn[1 : len(dn)-1])
		}
		// Strip header parameters that some SBCs append after `>`; we
		// don't currently consume them (e.g. ;party= ;id-type=).
	} else {
		// Bare addr-spec. Trim trailing header parameters (;tag=...) —
		// strictly PAI shouldn't have any, but tolerate.
		if semi := strings.IndexByte(row, ';'); semi >= 0 {
			uri = strings.TrimSpace(row[:semi])
		} else {
			uri = row
		}
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return Asserted{}
	}
	scheme := strings.ToLower(uri)
	if i := strings.IndexByte(scheme, ':'); i > 0 {
		scheme = scheme[:i]
	} else {
		return Asserted{}
	}
	switch scheme {
	case "sip", "sips", "tel":
		// recognised
	default:
		// RFC 3325 only defines sip/sips/tel; reject others rather than
		// passing through, which would be a vector for header smuggling.
		return Asserted{}
	}
	return Asserted{URI: uri, DisplayName: dn, Scheme: scheme}
}

func unquoteDisplayName(s string) string {
	if !strings.ContainsAny(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	escNext := false
	for _, r := range s {
		if escNext {
			b.WriteRune(r)
			escNext = false
			continue
		}
		if r == '\\' {
			escNext = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ParsePrivacy parses the `Privacy:` header value (RFC 3323 §4.2).
// The header carries a semicolon-separated token list; we lowercase
// and return them in declaration order. Empty input → nil slice.
func ParsePrivacy(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	var out []string
	for _, tok := range strings.Split(s, ";") {
		t := strings.ToLower(strings.TrimSpace(tok))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// PrivacyRequestsID reports whether the parsed Privacy tokens ask us
// to suppress identity at egress. RFC 3325 §7 says any of {id, header,
// user} implies "hide PAI"; `none` overrides.
func PrivacyRequestsID(tokens []string) bool {
	hasNone, hasHide := false, false
	for _, t := range tokens {
		switch t {
		case PrivacyNoChange:
			hasNone = true
		case PrivacyID, PrivacyHeader, PrivacyUser:
			hasHide = true
		}
	}
	return hasHide && !hasNone
}

// FormatPrivacyHeader renders a Privacy header value from a token list.
// Empty / nil input returns "" (caller should omit the header entirely).
func FormatPrivacyHeader(tokens []string) string {
	var clean []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t != "" {
			clean = append(clean, strings.ToLower(t))
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return strings.Join(clean, ";")
}
