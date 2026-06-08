// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Package historyinfo implements RFC 7044 History-Info and RFC 5806
// Diversion header processing for the SIP call-transfer / retargeting
// path.
//
// Why we need this:
//
//   - When a call arrives at us on trunk-number T and the business
//     layer (AI tool / DTMF / inbound REFER) decides to retarget to
//     agent A, we run a B2BUA bridge (see docs/sip_gap_analysis.md §
//     "转接架构说明"). That means the agent's UAS sees an INVITE
//     whose Request-URI is the agent, From is OUR trunk's CLI, To is
//     the agent — there is no surface signal that this call was
//     originally targeted at T or who retargeted it.
//   - Downstream PBXs / phones use History-Info (modern, RFC 7044) or
//     Diversion (legacy, RFC 5806) to render "original called number
//     was T" and to drive routing rules ("if Diversion is present,
//     do not deflect again"). Without these headers, transferred
//     calls look indistinguishable from primary calls and the agent
//     can't tell the customer "您拨打的 trunk T 已为您转人工".
//   - We emit BOTH headers because real-world PBX populations are
//     mixed: Avaya/Cisco modern firmware honor History-Info, but
//     Yealink/Polycom phones and older Asterisk read Diversion.
//
// This package does parsing/formatting only. The wiring into INVITE
// emission lives in pkg/sip/outbound; the wiring into the transfer
// chain lives in pkg/sip/conversation.
package historyinfo

import (
	"net/url"
	"strconv"
	"strings"
)

// Entry is one entry in the History-Info chain. RFC 7044 §4: each
// Request-URI the request has traversed becomes one entry, indexed
// in dotted decimal so forking proxies can model branches. We do not
// fork (we're a B2BUA), so our indices are always flat integers.
type Entry struct {
	// URI is the bare target URI (e.g. "sip:+8613800138000@trust.example").
	// Whether/not angle-bracketed, we normalise on parse to no brackets.
	URI string

	// Index is the dotted-decimal index per RFC 7044 §4.1. We always
	// emit flat integers like "1", "2", "3" (no forking).
	Index string

	// ReasonHeader optionally carries a SIP Reason header value
	// embedded in the Request-URI's hi-target-param `Reason=...`. RFC
	// 7044 §4.2: the param is percent-encoded; we decode on parse and
	// re-encode on format. Common values for our case:
	//   "SIP;cause=302;text=\"Moved Temporarily\""  — generic retarget
	//   "SIP;cause=480;text=\"AI Transfer\""         — transfer to agent
	//   "SIP;cause=487;text=\"Cancelled\""           — caller hung up
	// Empty = no reason param (legal, just less informative downstream).
	ReasonHeader string

	// PrivacyHeader: history-info-targeted-toparam Privacy value
	// (RFC 7044 §4.3). Rare; we currently don't emit but parse for
	// round-tripping.
	PrivacyHeader string

	// Extra opaque params we encountered on parse but don't model.
	// Carried through on format so we don't strip carrier annotations.
	Extra []string
}

// Format renders one entry in wire format:
//
//	<sip:+8613800138000@trust.example?Reason=SIP%3Bcause%3D302>;index=1
//
// Per RFC 7044 §4: the Reason / Privacy params live inside the URI
// hi-target as URI-headers (after `?`), separated by `&`, percent
// encoded; the `;index=` and other hi-target-params live OUTSIDE the
// angle brackets on the entry.
func (e Entry) Format() string {
	uri := strings.TrimSpace(e.URI)
	if uri == "" {
		return ""
	}
	uri = strings.TrimPrefix(uri, "<")
	uri = strings.TrimSuffix(uri, ">")

	// Build embedded URI-headers (Reason / Privacy) percent-encoded.
	var uriHeaders []string
	if r := strings.TrimSpace(e.ReasonHeader); r != "" {
		uriHeaders = append(uriHeaders, "Reason="+url.QueryEscape(r))
	}
	if p := strings.TrimSpace(e.PrivacyHeader); p != "" {
		uriHeaders = append(uriHeaders, "Privacy="+url.QueryEscape(p))
	}
	if len(uriHeaders) > 0 {
		// Avoid double-? if the bare URI already had headers.
		sep := "?"
		if strings.Contains(uri, "?") {
			sep = "&"
		}
		uri = uri + sep + strings.Join(uriHeaders, "&")
	}

	out := "<" + uri + ">"
	if idx := strings.TrimSpace(e.Index); idx != "" {
		out += ";index=" + idx
	}
	for _, p := range e.Extra {
		if t := strings.TrimSpace(p); t != "" {
			out += ";" + t
		}
	}
	return out
}

// FormatChain renders an entire History-Info chain as a comma-
// separated header value. Pass to msg.SetHeader("History-Info", ...).
// Empty input → "".
func FormatChain(chain []Entry) string {
	parts := make([]string, 0, len(chain))
	for _, e := range chain {
		if s := e.Format(); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// ParseChain parses a (possibly folded multi-instance) History-Info
// header value into entries. We are LENIENT on input — the goal is
// to forward whatever upstream produced, not validate carrier
// behaviour. Unparseable rows are skipped.
func ParseChain(raw string) []Entry {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	// Handle multi-instance folding: stack.Message.GetHeader joins
	// repeated headers with \r\n; treat that as another comma.
	s = strings.ReplaceAll(s, "\r\n", ",")
	rows := splitTopLevelCommas(s)
	var out []Entry
	for _, row := range rows {
		if e, ok := parseEntry(row); ok {
			out = append(out, e)
		}
	}
	return out
}

// splitTopLevelCommas splits on commas not inside <...> or quoted
// strings. Same pattern as pkg/sip/identity but inlined here so the
// two packages stay independent.
func splitTopLevelCommas(s string) []string {
	var (
		out     []string
		buf     strings.Builder
		inAngle bool
		inQuote bool
	)
	flush := func() {
		if t := strings.TrimSpace(buf.String()); t != "" {
			out = append(out, t)
		}
		buf.Reset()
	}
	for _, r := range s {
		switch r {
		case '"':
			if !inAngle {
				inQuote = !inQuote
			}
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
			if inAngle || inQuote {
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

func parseEntry(row string) (Entry, bool) {
	row = strings.TrimSpace(row)
	if row == "" {
		return Entry{}, false
	}
	// Must contain a URI; History-Info entries always do.
	lt := strings.Index(row, "<")
	gt := strings.Index(row, ">")
	if lt < 0 || gt <= lt {
		return Entry{}, false
	}
	uriPart := row[lt+1 : gt]
	rest := strings.TrimSpace(row[gt+1:])

	e := Entry{}

	// Pull embedded URI-headers (Reason / Privacy) out of uriPart.
	if q := strings.Index(uriPart, "?"); q >= 0 {
		hdrs := uriPart[q+1:]
		uriPart = uriPart[:q]
		for _, kv := range strings.Split(hdrs, "&") {
			eq := strings.IndexByte(kv, '=')
			if eq <= 0 {
				continue
			}
			k := strings.TrimSpace(kv[:eq])
			v, err := url.QueryUnescape(strings.TrimSpace(kv[eq+1:]))
			if err != nil {
				// Best-effort: keep raw on decode failure rather than
				// dropping the entry.
				v = strings.TrimSpace(kv[eq+1:])
			}
			switch strings.ToLower(k) {
			case "reason":
				e.ReasonHeader = v
			case "privacy":
				e.PrivacyHeader = v
			}
		}
	}
	e.URI = strings.TrimSpace(uriPart)

	// Walk hi-target-params on `rest`.
	for _, p := range strings.Split(rest, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(p), "index=") {
			e.Index = strings.TrimSpace(p[len("index="):])
			continue
		}
		e.Extra = append(e.Extra, p)
	}
	return e, e.URI != ""
}

// NextIndex returns "N+1" where N is the largest top-level integer
// index in chain. Used when we extend the chain on retarget — RFC
// 7044 §4.1 says the new entry's index must be at the same depth as
// the prior entry plus one. For our B2BUA we always extend flat.
//
// Examples (chain index sequence → returned next):
//
//	(empty)               → "1"
//	"1"                   → "2"
//	"1", "2"              → "3"
//	"1", "1.1"            → "2"   (siblings of root)
//	"1", "2", "2.1"       → "3"
func NextIndex(chain []Entry) string {
	max := 0
	for _, e := range chain {
		idx := strings.TrimSpace(e.Index)
		if idx == "" {
			continue
		}
		// Top-level component only.
		if dot := strings.IndexByte(idx, '.'); dot >= 0 {
			idx = idx[:dot]
		}
		if n, err := strconv.Atoi(idx); err == nil && n > max {
			max = n
		}
	}
	return strconv.Itoa(max + 1)
}

// ─────────────────────────────────────────────────────────────────
// RFC 5806 Diversion (legacy)
// ─────────────────────────────────────────────────────────────────

// DiversionReason values per RFC 5806 §4.1.1.
const (
	DiversionUnknown       = "unknown"
	DiversionUserBusy      = "user-busy"
	DiversionNoAnswer      = "no-answer"
	DiversionUnavailable   = "unavailable"
	DiversionUnconditional = "unconditional"
	DiversionTimeOfDay     = "time-of-day"
	DiversionDoNotDisturb  = "do-not-disturb"
	DiversionDeflection    = "deflection"
	DiversionFollowMe      = "follow-me"
	DiversionOutOfService  = "out-of-service"
	DiversionAway          = "away"
)

// Diversion is one row of an RFC 5806 Diversion header chain. Unlike
// History-Info, Diversion does NOT carry indices — chain order is by
// header appearance order, oldest first.
type Diversion struct {
	URI         string
	Reason      string // free-form per RFC 5806; commonly one of DiversionXxx
	Counter     int    // hop counter; missing → 0; we increment when extending
	Privacy     string // "full" / "name" / "uri" / "off" / ...
	Limit       int    // hop limit (counter <= limit before drop); 0 = unset
	Screen      string // "yes" / "no" — was the diversion source verified
	ExtraParams []string
}

// Format renders one Diversion entry. RFC 5806 §4.1: the URI is
// angle-bracketed; params live outside, semicolon-separated.
func (d Diversion) Format() string {
	uri := strings.TrimSpace(d.URI)
	if uri == "" {
		return ""
	}
	uri = strings.TrimPrefix(uri, "<")
	uri = strings.TrimSuffix(uri, ">")
	out := "<" + uri + ">"
	if r := strings.TrimSpace(d.Reason); r != "" {
		out += ";reason=" + r
	}
	if d.Counter > 0 {
		out += ";counter=" + strconv.Itoa(d.Counter)
	}
	if d.Limit > 0 {
		out += ";limit=" + strconv.Itoa(d.Limit)
	}
	if p := strings.TrimSpace(d.Privacy); p != "" {
		out += ";privacy=" + p
	}
	if s := strings.TrimSpace(d.Screen); s != "" {
		out += ";screen=" + s
	}
	for _, p := range d.ExtraParams {
		if t := strings.TrimSpace(p); t != "" {
			out += ";" + t
		}
	}
	return out
}

// FormatDiversionChain renders the Diversion header value (multiple
// rows comma-separated, RFC 5806 §4.1).
func FormatDiversionChain(chain []Diversion) string {
	parts := make([]string, 0, len(chain))
	for _, d := range chain {
		if s := d.Format(); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// ParseDiversionChain parses Diversion header value(s). Lenient — same
// philosophy as ParseChain.
func ParseDiversionChain(raw string) []Diversion {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", ",")
	rows := splitTopLevelCommas(s)
	var out []Diversion
	for _, row := range rows {
		if d, ok := parseDiversion(row); ok {
			out = append(out, d)
		}
	}
	return out
}

func parseDiversion(row string) (Diversion, bool) {
	row = strings.TrimSpace(row)
	if row == "" {
		return Diversion{}, false
	}
	lt := strings.Index(row, "<")
	gt := strings.Index(row, ">")
	if lt < 0 || gt <= lt {
		return Diversion{}, false
	}
	d := Diversion{URI: strings.TrimSpace(row[lt+1 : gt])}
	rest := strings.TrimSpace(row[gt+1:])
	for _, p := range strings.Split(rest, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			d.ExtraParams = append(d.ExtraParams, p)
			continue
		}
		k := strings.ToLower(strings.TrimSpace(p[:eq]))
		v := strings.TrimSpace(p[eq+1:])
		switch k {
		case "reason":
			d.Reason = v
		case "counter":
			if n, err := strconv.Atoi(v); err == nil {
				d.Counter = n
			}
		case "limit":
			if n, err := strconv.Atoi(v); err == nil {
				d.Limit = n
			}
		case "privacy":
			d.Privacy = v
		case "screen":
			d.Screen = v
		default:
			d.ExtraParams = append(d.ExtraParams, p)
		}
	}
	return d, d.URI != ""
}

// MaxCounter returns the largest counter value in the chain; used
// when extending so the new entry can be counter=Max+1.
func MaxCounter(chain []Diversion) int {
	max := 0
	for _, d := range chain {
		if d.Counter > max {
			max = d.Counter
		}
	}
	return max
}

// ─────────────────────────────────────────────────────────────────
// Transfer helpers — used by the conversation layer when extending
// the chain on a B2BUA retarget.
// ─────────────────────────────────────────────────────────────────

// AppendTransferEntry returns a new History-Info chain with one
// additional entry representing the retarget. originalToURI is the
// To URI from the inbound INVITE; newTargetURI is what we are about
// to send the outbound INVITE to. reasonHeader is a SIP Reason
// header value (or "" to omit).
//
// On first call with an empty inbound chain we synthesise a root
// entry for originalToURI as well, so the resulting chain represents
// the full path (original → new) instead of just (new).
func AppendTransferEntry(inboundChain []Entry, originalToURI, newTargetURI, reasonHeader string) []Entry {
	originalToURI = strings.TrimSpace(originalToURI)
	newTargetURI = strings.TrimSpace(newTargetURI)
	if newTargetURI == "" {
		return inboundChain
	}
	out := make([]Entry, 0, len(inboundChain)+2)
	out = append(out, inboundChain...)
	if len(out) == 0 && originalToURI != "" {
		out = append(out, Entry{URI: originalToURI, Index: "1"})
	}
	out = append(out, Entry{
		URI:          newTargetURI,
		Index:        NextIndex(out),
		ReasonHeader: reasonHeader,
	})
	return out
}

// AppendDiversionEntry returns a new Diversion chain with one
// additional entry pointing at originalToURI (RFC 5806 semantics:
// "the call was diverted FROM originalToURI"). The new entry's
// counter is incremented by 1 over the chain's current max.
//
// We default reason to "unconditional" since our retarget is
// platform-initiated (AI / pool / REFER), not a phone busy/no-answer.
// Callers can override (e.g. DiversionDeflection for REFER-driven).
func AppendDiversionEntry(inboundChain []Diversion, originalToURI, reason string) []Diversion {
	originalToURI = strings.TrimSpace(originalToURI)
	if originalToURI == "" {
		return inboundChain
	}
	r := strings.TrimSpace(reason)
	if r == "" {
		r = DiversionUnconditional
	}
	out := make([]Diversion, 0, len(inboundChain)+1)
	out = append(out, inboundChain...)
	out = append(out, Diversion{
		URI:     originalToURI,
		Reason:  r,
		Counter: MaxCounter(out) + 1,
	})
	return out
}

// ExtractURIFromToHeader strips the display-name and angle brackets
// from a To header value, returning just the URI ("sip:foo@bar"). On
// malformed input returns "" so callers can short-circuit before
// emitting a malformed History-Info entry.
//
// Lives here because the only consumers are this package and the
// conversation transfer code that calls AppendTransferEntry.
func ExtractURIFromToHeader(toHeader string) string {
	s := strings.TrimSpace(toHeader)
	if s == "" {
		return ""
	}
	if lt := strings.Index(s, "<"); lt >= 0 {
		if gt := strings.Index(s[lt:], ">"); gt > 0 {
			return strings.TrimSpace(s[lt+1 : lt+gt])
		}
	}
	// No angle brackets; trim header parameters (tag=, etc.).
	if semi := strings.IndexByte(s, ';'); semi >= 0 {
		s = s[:semi]
	}
	return strings.TrimSpace(s)
}
