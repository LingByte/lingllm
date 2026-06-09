package stack

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// SIP/2.0 200 OK
// Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK12345
// To: Bob <sip:bob@example.com>;tag=as12345
// From: Alice <sip:alice@example.com>;tag=1928301774
// Call-ID: a84b4c76e66710@pc33.atlanta.com
// CSeq: 314159 INVITE
// Contact: <sip:bob@192.168.1.100:5060>
// Content-Type: application/sdp
// Content-Length: 158
// v=0
// o=user2 2890844526 2890844526 IN IP4 192.168.1.100
// s=Session SDP
// c=IN IP4 192.168.1.100
// t=0 0
// m=audio 3456 RTP/AVP 0
// a=rtpmap:0 PCMU/8000

// Message is an in-memory representation of one SIP/2.0 request or response.
//
// Requests set IsRequest=true and populate Method, RequestURI, and Version
// (typically "SIP/2.0"). Responses set IsRequest=false and populate StatusCode
// and StatusText (e.g. 200, "OK").
//
// Headers are stored under lowercase canonical keys (see canonicalHeaderKey).
// HeadersMulti preserves every value for multi-value headers such as Via,
// Record-Route, and Contact. Headers holds only the first value per name for
// quick lookup via GetHeader.
//
// Body holds the raw message body (usually SDP for INVITE/200, or empty).
// Call PrepareForSend before transmission to set Content-Length from Body.
type Message struct {
	Method     string
	RequestURI string
	StatusCode int    // 200
	StatusText string // "OK"
	Version    string // "SIP/2.0"
	// Headers = map[string]string{
	//    "Via":        "SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK12345",
	//    "To":         "Bob <sip:bob@example.com>;tag=as12345",
	//    "From":       "Alice <sip:alice@example.com>;tag=1928301774",
	//    "Call-ID":    "a84b4c76e66710@pc33.atlanta.com",
	//    "CSeq":       "314159 INVITE",
	//    "Contact":    "<sip:bob@192.168.1.100:5060>",
	//    "Content-Type": "application/sdp",
	//    "Content-Length": "158",
	//}
	Headers map[string]string // first value per canonical header name
	// HeadersMulti = map[string][]string{
	//    "Via":        {"SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK12345"},
	//    "To":         {"Bob <sip:bob@example.com>;tag=as12345"},
	//    "From":       {"Alice <sip:alice@example.com>;tag=1928301774"},
	//    "Call-ID":    {"a84b4c76e66710@pc33.atlanta.com"},
	//    "CSeq":       {"314159 INVITE"},
	//    "Contact":    {"<sip:bob@192.168.1.100:5060>"},
	//    "Content-Type": {"application/sdp"},
	//    "Content-Length": {"158"},
	//}
	HeadersMulti map[string][]string // all values per canonical header name (e.g. Via)
	// Body input SDP 会话描述协议
	// v=0  => SDP Version
	// o=user2 2890844526 2890844526 	IN 		IP4 	192.168.1.100 会话所有者 / 会话 ID
	// 用户名 	会话ID 		版本号 	  网络类型  地址类型 		主机地址
	// s=Session SDP 会话名称
	// c=IN IP4 192.168.1.100 连接信息-指定媒体流收发地址
	// t=0 0	会话时长
	// m=audio 3456 RTP/AVP 0	媒体类型 端口 传输协议 编码格式
	// a=rtpmap:0 PCMU/8000
	// rtpmap：RTP 映射说明
	// 0：对应上面 m 行的负载类型 ID
	// PCMU：编码格式（G.711 μ 律，传统电话语音编码）
	// 8000：采样率 8000Hz（标准电话音质）
	Body      string
	IsRequest bool
}

func canonicalHeaderKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Parse decodes a complete SIP message from its on-the-wire text form.
//
// The parser accepts CRLF or bare LF, unfolds RFC 3261 header continuation lines,
// and trims the body to Content-Length when that header is present.
// Malformed header lines without ":" are silently skipped.
func Parse(raw string) (*Message, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s: empty message", errPrefix)
	}

	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("%s: empty message lines", errPrefix)
	}

	firstLine := strings.TrimSpace(lines[0])
	if firstLine == "" {
		return nil, fmt.Errorf("%s: empty first line", errPrefix)
	}

	msg := &Message{
		Headers:      make(map[string]string),
		HeadersMulti: make(map[string][]string),
	}

	if strings.HasPrefix(firstLine, "SIP/") {
		msg.IsRequest = false
		parts := strings.SplitN(firstLine, " ", 3)
		if len(parts) < 2 {
			return nil, fmt.Errorf("%s: invalid response line: %s", errPrefix, firstLine)
		}
		msg.Version = strings.TrimSpace(parts[0])
		code, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("%s: invalid status code in %q: %w", errPrefix, firstLine, err)
		}
		msg.StatusCode = code
		if len(parts) >= 3 {
			msg.StatusText = strings.TrimSpace(parts[2])
		}
	} else {
		msg.IsRequest = true
		parts := strings.SplitN(firstLine, " ", 3)
		if len(parts) < 2 {
			return nil, fmt.Errorf("%s: invalid request line: %s", errPrefix, firstLine)
		}
		msg.Method = strings.ToUpper(strings.TrimSpace(parts[0]))
		msg.RequestURI = strings.TrimSpace(parts[1])
		if len(parts) >= 3 {
			msg.Version = strings.TrimSpace(parts[2])
		} else {
			msg.Version = SIPVersion
		}
	}

	bodyStart := -1
	var headerSection []string
	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			bodyStart = i + 1
			break
		}
		headerSection = append(headerSection, lines[i])
	}
	headerLines := unfoldHeaderLines(headerSection)
	cl, hasCL, err := contentLengthFromHeaders(headerLines)
	if err != nil {
		return nil, err
	}
	applyHeadersToMessage(msg, headerLines)

	if bodyStart > 0 && bodyStart < len(lines) {
		msg.Body = strings.Join(lines[bodyStart:], "\n")
	}
	if hasCL {
		msg.Body = trimBodyToContentLength(msg.Body, cl)
	}

	return msg, nil
}

// PrepareForSend sets Content-Length from the normalized body byte length.
// Call before String() or Endpoint.Send when the body may have changed.
func (m *Message) PrepareForSend() {
	if m == nil {
		return
	}
	m.SetHeader(HeaderContentLength, strconv.Itoa(BodyBytesLen(m.Body)))
}

// String encodes the message for transmission. Line endings are CRLF.
// Well-known headers are emitted in a stable, SIP-friendly order (Via, From,
// To, Call-ID, CSeq, …) followed by remaining headers sorted lexicographically.
// Endpoint.Send calls PrepareForSend automatically; use PrepareForSend yourself
// when serializing outside Endpoint.
func (m *Message) String() string {
	if m == nil {
		return ""
	}

	var b strings.Builder
	if m.IsRequest {
		b.WriteString(fmt.Sprintf("%s %s %s\r\n", m.Method, m.RequestURI, m.Version))
	} else {
		b.WriteString(fmt.Sprintf("%s %d %s\r\n", m.Version, m.StatusCode, m.StatusText))
	}

	emitted := make(map[string]struct{}, 32)
	for _, k := range preferredHeaderOrder {
		vals := m.HeadersMulti[k]
		if len(vals) == 0 {
			if v, ok := m.Headers[k]; ok && v != "" {
				vals = []string{v}
			}
		}
		if len(vals) == 0 {
			continue
		}
		for _, v := range vals {
			b.WriteString(fmt.Sprintf("%s: %s\r\n", prettyHeaderName(k), v))
		}
		emitted[k] = struct{}{}
	}

	restKeys := make([]string, 0, len(m.HeadersMulti))
	for k := range m.HeadersMulti {
		if _, ok := emitted[k]; ok {
			continue
		}
		restKeys = append(restKeys, k)
	}
	for k := range m.Headers {
		if _, ok := emitted[k]; ok {
			continue
		}
		found := false
		for _, rk := range restKeys {
			if rk == k {
				found = true
				break
			}
		}
		if !found {
			restKeys = append(restKeys, k)
		}
	}
	sort.Strings(restKeys)
	for _, k := range restKeys {
		vals := m.HeadersMulti[k]
		if len(vals) == 0 {
			if v, ok := m.Headers[k]; ok && v != "" {
				vals = []string{v}
			}
		}
		for _, v := range vals {
			b.WriteString(fmt.Sprintf("%s: %s\r\n", prettyHeaderName(k), v))
		}
	}

	b.WriteString("\r\n")
	if m.Body != "" {
		b.WriteString(normalizeCRLF(m.Body))
	}

	return b.String()
}

func normalizeCRLF(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// BodyBytesLen returns the byte length of the body after CRLF normalization.
func BodyBytesLen(body string) int {
	return len([]byte(normalizeCRLF(body)))
}

func prettyHeaderName(canonical string) string {
	ck := canonicalHeaderKey(canonical)
	if wire, ok := wireHeaderNames[ck]; ok {
		return wire
	}
	switch ck {
	default:
		parts := strings.Split(canonical, "-")
		for i := range parts {
			if parts[i] == "" {
				continue
			}
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
		return strings.Join(parts, "-")
	}
}

// GetHeader returns the first header value for name (case-insensitive).
func (m *Message) GetHeader(name string) string {
	if m == nil {
		return ""
	}
	return m.Headers[canonicalHeaderKey(name)]
}

// GetHeaders returns all values for a header name (case-insensitive).
func (m *Message) GetHeaders(name string) []string {
	if m == nil {
		return nil
	}
	return m.HeadersMulti[canonicalHeaderKey(name)]
}

// SetHeader replaces a header with a single value.
func (m *Message) SetHeader(name, value string) {
	if m == nil {
		return
	}
	if m.Headers == nil {
		m.Headers = make(map[string]string)
	}
	if m.HeadersMulti == nil {
		m.HeadersMulti = make(map[string][]string)
	}
	ck := canonicalHeaderKey(name)
	m.Headers[ck] = value
	m.HeadersMulti[ck] = []string{value}
}

// AddHeader appends a header value (multi-value headers such as Via).
func (m *Message) AddHeader(name, value string) {
	if m == nil {
		return
	}
	if m.Headers == nil {
		m.Headers = make(map[string]string)
	}
	if m.HeadersMulti == nil {
		m.HeadersMulti = make(map[string][]string)
	}
	ck := canonicalHeaderKey(name)
	if _, exists := m.Headers[ck]; !exists {
		m.Headers[ck] = value
	}
	m.HeadersMulti[ck] = append(m.HeadersMulti[ck], value)
}

// ReadMessage reads exactly one SIP message from a byte stream (TCP/TLS).
//
// It reads header lines until a blank line (unfolding continuation lines),
// validates duplicate Content-Length headers, reads exactly Content-Length
// body octets, then parses. When Content-Length is absent the body is empty.
func ReadMessage(r *bufio.Reader) (*Message, error) {
	var hdrLines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(hdrLines) == 0 {
				return nil, fmt.Errorf("%s: header continuation without prior line", errPrefix)
			}
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				hdrLines[len(hdrLines)-1] += " " + trimmed
			}
			continue
		}
		hdrLines = append(hdrLines, line)
	}
	if len(hdrLines) == 0 {
		return nil, fmt.Errorf("%s: empty message headers", errPrefix)
	}
	unfolded := unfoldHeaderLines(hdrLines)
	cl, hasCL, err := contentLengthFromHeaders(unfolded)
	if err != nil {
		return nil, err
	}
	raw := strings.Join(unfolded, "\n") + "\n\n"
	if hasCL && cl > 0 {
		body := make([]byte, cl)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		raw += string(body)
	}
	return Parse(raw)
}

// ParseRAck parses the RAck header from RFC 3262 (reliable provisional responses).
// Wire format: "<response-num> <cseq-num> <method>", e.g. "1 314159 INVITE".
// PRACK requests carry RAck so the UAS can match them to a specific 1xx response.
func ParseRAck(h string) (rseq uint32, cseqNum int, method string, err error) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, 0, "", fmt.Errorf("%s: empty RAck", errPrefix)
	}
	parts := strings.Fields(h)
	if len(parts) < 3 {
		return 0, 0, "", fmt.Errorf("%s: RAck needs rseq cseq method", errPrefix)
	}
	rs, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return 0, 0, "", fmt.Errorf("%s: RAck rseq: %w", errPrefix, err)
	}
	cs, err := strconv.Atoi(parts[1])
	if err != nil || cs < 0 {
		return 0, 0, "", fmt.Errorf("%s: RAck cseq", errPrefix)
	}
	return uint32(rs), cs, strings.ToUpper(strings.TrimSpace(parts[2])), nil
}
