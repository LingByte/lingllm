// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

// Identity header (RFC 8224 §4) carries a PASSporT plus three
// parameters identifying how to verify it. Wire form examples:
//
//   Identity: eyJhbGciOi...zNiJ9.eyJhdHRl...In19.MEUCIQD...JZQ;\
//     info=<https://sti-cert.example/cert.pem>;\
//     alg=ES256;ppt=shaken
//
// Pre-RFC 8224 drafts (RFC 4474) wrapped the JWT in double quotes;
// the current standard does NOT. Older PBX populations may still
// emit the quoted form, so the parser is lenient about it.
//
// `info=` is the cert-fetch URL — typically duplicates the `x5u`
// claim in the JWS header but exists separately so an SBC can
// route the verifier without parsing the JWS first.
//
// We do not implement `ppt=div` (RFC 8946 diverted-call extension)
// or `ppt=rph` (RFC 8443 resource-priority) yet. Add when needed.

import (
	"errors"
	"fmt"
	"strings"
)

// IdentityHeader is the parsed Identity header (RFC 8224 §4).
type IdentityHeader struct {
	// Passport is the compact-form JWS (b64.b64.b64). Caller passes
	// this to VerifyPassport after fetching the cert.
	Passport string
	// Info is the x5u URL (cert fetch). Required per RFC 8224 §4.1.
	Info string
	// Alg is the JWS algorithm hint duplicated from the JOSE header.
	// "ES256" is the only value SHAKEN supports.
	Alg string
	// Ppt is the PASSporT extension type ("shaken", "div", "rph").
	// Empty when the PASSporT is a base RFC 8225 token.
	Ppt string
}

// FormatIdentityHeader renders the Identity header value (the part
// after `Identity: `). Produces the unquoted RFC 8224 form. info
// and alg are required; ppt is included only when non-empty.
//
// Returns an error rather than emitting a malformed header — every
// downstream verifier rejects malformed input as a 438 Invalid
// Identity Header response (RFC 8224 §6.2.4).
func FormatIdentityHeader(h IdentityHeader) (string, error) {
	if strings.TrimSpace(h.Passport) == "" {
		return "", errors.New("stir: empty passport in Identity header")
	}
	if strings.ContainsAny(h.Passport, " \t\r\n,;") {
		return "", errors.New("stir: passport contains illegal whitespace/separator")
	}
	if strings.TrimSpace(h.Info) == "" {
		return "", errors.New("stir: info (x5u URL) required")
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(h.Info)), "https://") {
		return "", fmt.Errorf("stir: info must be https URL, got %q", h.Info)
	}
	if strings.TrimSpace(h.Alg) == "" {
		return "", errors.New("stir: alg required")
	}
	var b strings.Builder
	b.WriteString(h.Passport)
	b.WriteString(";info=<")
	b.WriteString(strings.TrimSpace(h.Info))
	b.WriteString(">;alg=")
	b.WriteString(strings.TrimSpace(h.Alg))
	if p := strings.TrimSpace(h.Ppt); p != "" {
		b.WriteString(";ppt=")
		b.WriteString(p)
	}
	return b.String(), nil
}

// ParseIdentityHeader parses an Identity header value (the part
// after `Identity: `, with surrounding whitespace trimmed). We
// accept:
//
//   * RFC 8224 unquoted form (current): JWT;info=<url>;alg=ES256;ppt=shaken
//   * RFC 4474 quoted form (legacy):    "JWT";info=<url>;alg=ES256
//   * Mixed-case parameter names (RFC 3261 §7.3.1 says they're
//     case-insensitive).
//
// Returns a wrapped error when required params are missing so the
// caller can decide between 438 / 437 / 436 (RFC 8224 §6).
func ParseIdentityHeader(raw string) (IdentityHeader, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return IdentityHeader{}, errors.New("stir: empty Identity header")
	}
	// Split on the first ';' separating the JWT from its params.
	semi := strings.IndexByte(s, ';')
	var jwt, paramsPart string
	if semi < 0 {
		jwt = s
	} else {
		jwt = strings.TrimSpace(s[:semi])
		paramsPart = s[semi+1:]
	}
	// Strip legacy RFC 4474 double-quotes if present.
	if len(jwt) >= 2 && jwt[0] == '"' && jwt[len(jwt)-1] == '"' {
		jwt = jwt[1 : len(jwt)-1]
	}
	if strings.Count(jwt, ".") != 2 {
		return IdentityHeader{}, fmt.Errorf("stir: passport must have 3 base64 segments, got %q", jwt)
	}

	h := IdentityHeader{Passport: jwt}
	for _, p := range splitSIPParams(paramsPart) {
		key, val := splitKV(p)
		switch strings.ToLower(key) {
		case "info":
			h.Info = trimAngle(val)
		case "alg":
			h.Alg = strings.TrimSpace(val)
		case "ppt":
			h.Ppt = strings.TrimSpace(val)
		}
	}
	if strings.TrimSpace(h.Info) == "" {
		return h, errors.New("stir: Identity header missing info= parameter")
	}
	if strings.TrimSpace(h.Alg) == "" {
		return h, errors.New("stir: Identity header missing alg= parameter")
	}
	return h, nil
}

// splitSIPParams splits a `;a=1;b=2` tail into ["a=1","b=2"]. SIP
// parameters can't contain unquoted ';' (RFC 3261 §25.1 hostport /
// generic-param), so naive split is correct here. Drops empties.
func splitSIPParams(s string) []string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitKV(s string) (string, string) {
	if i := strings.IndexByte(s, '='); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return strings.TrimSpace(s), ""
}

// trimAngle strips the RFC 3261 §25.1 angle-quoted absoluteURI form.
// `<https://x.example/>` → `https://x.example/`. Leaves a bare URL
// untouched.
func trimAngle(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '<' && s[len(s)-1] == '>' {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}
