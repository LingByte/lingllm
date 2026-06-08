package outbound

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// DialTargetFromReferTo parses a Refer-To header value (possibly angle-bracketed)
// into a DialTarget. Host and port for SignalingAddr come from the SIP URI authority;
// default port is 5060 when omitted.
func DialTargetFromReferTo(referTo string) (DialTarget, error) {
	raw := strings.TrimSpace(referTo)
	if raw == "" {
		return DialTarget{}, fmt.Errorf("refer-to empty")
	}
	if i := strings.Index(raw, "<"); i >= 0 {
		if j := strings.Index(raw[i+1:], ">"); j >= 0 {
			raw = strings.TrimSpace(raw[i+1 : i+1+j])
		}
	}
	raw = strings.TrimSpace(raw)
	sem := strings.Index(raw, ";")
	if sem > 0 {
		raw = strings.TrimSpace(raw[:sem])
	}
	lower := strings.ToLower(raw)
	var scheme string
	var rest string
	switch {
	case strings.HasPrefix(lower, "sips:"):
		scheme = "sips:"
		rest = raw[5:]
	case strings.HasPrefix(lower, "sip:"):
		scheme = "sip:"
		rest = raw[4:]
	default:
		return DialTarget{}, fmt.Errorf("refer-to must be sip(s): URI")
	}
	at := strings.LastIndex(rest, "@")
	if at <= 0 || at >= len(rest)-1 {
		return DialTarget{}, fmt.Errorf("refer-to missing user@host")
	}
	user := rest[:at]
	hostport := rest[at+1:]
	if hostport == "" {
		return DialTarget{}, fmt.Errorf("refer-to missing host")
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
		portStr = "5060"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return DialTarget{}, fmt.Errorf("refer-to bad port")
	}
	hp := net.JoinHostPort(host, strconv.Itoa(port))
	return DialTarget{
		RequestURI:    scheme + user + "@" + hp,
		SignalingAddr: hp,
	}, nil
}
