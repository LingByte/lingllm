// Package signalinglog provides optional logrus hooks for SIP signaling audit trails.
// Persistence (GORM/DB) is intentionally omitted — applications can wrap the hook.
package signalinglog

import (
	"fmt"
	"net"
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/sirupsen/logrus"
)

// Entry is one signaling message suitable for structured logging or external persistence.
type Entry struct {
	Direction string // "in" | "out"
	Transport string // "udp" | "tcp" | "tls"
	Method    string
	Status    int
	CallID    string
	Remote    string
	Summary   string
}

// Hook logs SIP messages via logrus at Debug level.
type Hook struct {
	Transport string
	Logger    *logrus.Logger
}

// NewHook returns a hook that logs to logrus StandardLogger when logger is nil.
func NewHook(transport string) *Hook {
	return &Hook{Transport: transport}
}

func (h *Hook) logger() *logrus.Logger {
	if h != nil && h.Logger != nil {
		return h.Logger
	}
	return logrus.StandardLogger()
}

// LogRequest records an inbound SIP request.
func (h *Hook) LogRequest(msg *stack.Message, remote *net.UDPAddr) {
	if h == nil || msg == nil || !msg.IsRequest {
		return
	}
	e := Entry{
		Direction: "in",
		Transport: h.Transport,
		Method:    msg.Method,
		CallID:    strings.TrimSpace(msg.GetHeader(stack.HeaderCallID)),
		Remote:    addrString(remote),
		Summary:   fmt.Sprintf("%s %s", msg.Method, strings.TrimSpace(msg.RequestURI)),
	}
	h.emit(e)
}

// LogResponse records an inbound SIP response.
func (h *Hook) LogResponse(msg *stack.Message, remote *net.UDPAddr) {
	if h == nil || msg == nil || msg.IsRequest {
		return
	}
	e := Entry{
		Direction: "in",
		Transport: h.Transport,
		Status:    msg.StatusCode,
		CallID:    strings.TrimSpace(msg.GetHeader(stack.HeaderCallID)),
		Remote:    addrString(remote),
		Summary:   fmt.Sprintf("SIP/2.0 %d %s", msg.StatusCode, strings.TrimSpace(msg.StatusText)),
	}
	h.emit(e)
}

// LogSent records an outbound SIP message.
func (h *Hook) LogSent(msg *stack.Message, remote *net.UDPAddr) {
	if h == nil || msg == nil {
		return
	}
	e := Entry{
		Direction: "out",
		Transport: h.Transport,
		CallID:    strings.TrimSpace(msg.GetHeader(stack.HeaderCallID)),
		Remote:    addrString(remote),
	}
	if msg.IsRequest {
		e.Method = msg.Method
		e.Summary = fmt.Sprintf("%s %s", msg.Method, strings.TrimSpace(msg.RequestURI))
	} else {
		e.Status = msg.StatusCode
		e.Summary = fmt.Sprintf("SIP/2.0 %d %s", msg.StatusCode, strings.TrimSpace(msg.StatusText))
	}
	h.emit(e)
}

func (h *Hook) emit(e Entry) {
	h.logger().WithFields(logrus.Fields{
		"sip_direction": e.Direction,
		"sip_transport": e.Transport,
		"sip_method":    e.Method,
		"sip_status":    e.Status,
		"call_id":       e.CallID,
		"remote":        e.Remote,
	}).Debug(e.Summary)
}

func addrString(a *net.UDPAddr) string {
	if a == nil {
		return ""
	}
	return a.String()
}
