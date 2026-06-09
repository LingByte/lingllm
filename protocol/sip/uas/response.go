package uas

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

// NewResponse builds a SIP response with common headers copied from the request (From, To, Call-ID, CSeq, Via).
// body and contentType may be empty for no body (Content-Length: 0).
func NewResponse(req *stack.Message, status int, reason, body, contentType string) (*stack.Message, error) {
	if req == nil || !req.IsRequest {
		return nil, fmt.Errorf("sip/uas: need a SIP request")
	}
	if status < 100 || status > 699 {
		return nil, fmt.Errorf("sip/uas: invalid status %d", status)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "OK"
	}
	resp := &stack.Message{
		IsRequest:    false,
		Version: stack.SIPVersion,
		StatusCode:   status,
		StatusText:   reason,
		Headers:      map[string]string{},
		HeadersMulti: map[string][]string{},
	}
	// Via may appear multiple times; echo all in order (RFC 3261).
	if vias := req.GetHeaders(stack.HeaderVia); len(vias) > 0 {
		resp.SetHeader(stack.HeaderVia, vias[0])
		for i := 1; i < len(vias); i++ {
			resp.AddHeader(stack.HeaderVia, vias[i])
		}
	}
	for _, h := range stack.CorrelationHeaders {
		if h == stack.HeaderVia {
			continue
		}
		if v := req.GetHeader(h); v != "" {
			resp.SetHeader(h, v)
		}
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	resp.Body = body
	if strings.TrimSpace(contentType) != "" {
		resp.SetHeader(stack.HeaderContentType, strings.TrimSpace(contentType))
	}
	resp.SetHeader(stack.HeaderContentLength, strconv.Itoa(stack.BodyBytesLen(body)))
	return resp, nil
}

// ErrorResponse returns a minimal final error response (3xx–6xx) with optional Reason header text in StatusText.
func ErrorResponse(req *stack.Message, status int, reason string) (*stack.Message, error) {
	if reason == "" {
		switch status {
		case 400:
			reason = "Bad Request"
		case 403:
			reason = "Forbidden"
		case 404:
			reason = "Not Found"
		case 405:
			reason = "Method Not Allowed"
		case 486:
			reason = "Busy Here"
		case 488:
			reason = "Not Acceptable Here"
		case 500:
			reason = "Server Internal Error"
		case 503:
			reason = "Service Unavailable"
		default:
			reason = "Error"
		}
	}
	return NewResponse(req, status, reason, "", "")
}

// FormatContact builds a SIP Contact header field value.
// user may be empty for the host-only form <sip:host:port>.
func FormatContact(host string, port int, user string) string {
	if port <= 0 {
		port = 5060
	}
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	if user != "" {
		return fmt.Sprintf("<sip:%s@%s:%d>", user, host, port)
	}
	return fmt.Sprintf("<sip:%s:%d>", host, port)
}
