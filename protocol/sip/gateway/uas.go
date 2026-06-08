package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/outbound"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/LingByte/lingllm/protocol/sip/transaction"
	"github.com/LingByte/lingllm/protocol/sip/uas"
	"github.com/sirupsen/logrus"
)

// UASConfig configures a UDP SIP user agent server.
type UASConfig struct {
	Host string // listen IP, default 0.0.0.0
	Port int    // UDP port, default 5060

	// Handlers are chained through the transaction layer (retransmissions, CANCEL, ACK).
	Handlers uas.Handlers

	// LocalIP is advertised in generated SDP answers when empty gateway picks the UDP bind address.
	LocalIP string
}

// UAS is a minimal inbound SIP server on UDP.
type UAS struct {
	cfg      UASConfig
	ep       *stack.Endpoint
	mgr      *transaction.Manager
	outbound *outbound.Manager
	send     transaction.SendFunc
}

// NewUAS builds a server; call Open before Serve.
func NewUAS(cfg UASConfig) *UAS {
	if strings.TrimSpace(cfg.Host) == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port <= 0 {
		cfg.Port = 5060
	}
	s := &UAS{cfg: cfg}
	s.mgr = transaction.NewManager()
	s.send = func(msg *stack.Message, addr *net.UDPAddr) error {
		return s.ep.Send(msg, addr)
	}
	return s
}

// AttachOutbound wires outbound response handling and shared Send on this UAS socket.
func (s *UAS) AttachOutbound(m *outbound.Manager) {
	if s == nil {
		return
	}
	s.outbound = m
}

// Manager returns the transaction manager (for advanced wiring or tests).
func (s *UAS) Manager() *transaction.Manager {
	if s == nil {
		return nil
	}
	return s.mgr
}

// Endpoint returns the underlying stack endpoint after Open.
func (s *UAS) Endpoint() *stack.Endpoint {
	if s == nil {
		return nil
	}
	return s.ep
}

// LocalIP returns the configured or detected IPv4 used in SDP answers.
func (s *UAS) LocalIP() string {
	if s == nil {
		return ""
	}
	if ip := strings.TrimSpace(s.cfg.LocalIP); ip != "" {
		return ip
	}
	if s.ep != nil {
		if la := s.ep.ListenAddr(); la != nil {
			if host, _, err := net.SplitHostPort(la.String()); err == nil {
				if host != "" && host != "0.0.0.0" && host != "::" {
					return host
				}
			}
		}
	}
	return "127.0.0.1"
}

// Open binds the UDP socket and registers handlers.
func (s *UAS) Open() error {
	if s == nil {
		return fmt.Errorf("sip/gateway: nil UAS")
	}
	epCfg := stack.EndpointConfig{
		Host: s.cfg.Host,
		Port: s.cfg.Port,
		OnReadError: func(err error) {
			logrus.WithError(err).Error("sip: udp read error")
		},
		OnParseErr: func(raw []byte, addr *net.UDPAddr, err error) {
			logrus.WithFields(logrus.Fields{
				"remote": addr.String(),
				"error":  err.Error(),
				"bytes":  len(raw),
			}).Warn("sip: parse error")
		},
		OnRequest: func(req *stack.Message, addr *net.UDPAddr) {
			if req == nil {
				return
			}
			logrus.WithFields(logrus.Fields{
				"method":  req.Method,
				"call_id": req.GetHeader("Call-ID"),
				"remote":  addr.String(),
			}).Info("sip: request")
		},
		OnResponse: func(req, resp *stack.Message, addr *net.UDPAddr) {
			if resp == nil {
				return
			}
			logrus.WithFields(logrus.Fields{
				"status":  resp.StatusCode,
				"call_id": resp.GetHeader("Call-ID"),
				"remote":  addr.String(),
			}).Debug("sip: response received")
		},
		OnResponseSent: func(req, resp *stack.Message, addr *net.UDPAddr) {
			if resp == nil {
				return
			}
			fields := logrus.Fields{
				"status": resp.StatusCode,
				"remote": addr.String(),
			}
			if req != nil {
				fields["method"] = req.Method
				fields["call_id"] = req.GetHeader("Call-ID")
			}
			logrus.WithFields(fields).Info("sip: response sent")
		},
		OnSIPResponse: func(resp *stack.Message, addr *net.UDPAddr) {
			if s.outbound != nil {
				s.outbound.HandleSIPResponse(resp, addr)
			}
			if s.mgr != nil {
				_ = s.mgr.HandleResponse(resp, addr)
			}
		},
	}
	s.ep = stack.NewEndpoint(epCfg)
	if err := s.ep.Open(); err != nil {
		return err
	}
	if s.cfg.LocalIP == "" {
		if ip := detectOutboundIP(); ip != "" {
			s.cfg.LocalIP = ip
		}
	}
	binding := uas.TransactionBinding{
		Mgr:  s.mgr,
		Send: s.send,
	}
	if err := s.cfg.Handlers.AttachWithTransaction(s.ep, binding); err != nil {
		_ = s.ep.Close()
		return err
	}
	logrus.WithFields(logrus.Fields{
		"listen": s.ep.ListenAddr().String(),
		"local":  s.LocalIP(),
	}).Info("sip: uas listening")
	return nil
}

// Serve runs until ctx is cancelled or a fatal read error occurs.
func (s *UAS) Serve(ctx context.Context) error {
	if s == nil || s.ep == nil {
		return fmt.Errorf("sip/gateway: not open")
	}
	return s.ep.Serve(ctx)
}

// Send writes a SIP message to addr on the bound UDP socket.
func (s *UAS) Send(msg *stack.Message, addr *net.UDPAddr) error {
	if s == nil || s.ep == nil {
		return fmt.Errorf("sip/gateway: not open")
	}
	return s.ep.Send(msg, addr)
}

// Close shuts down the UDP socket.
func (s *UAS) Close() error {
	if s == nil || s.ep == nil {
		return nil
	}
	return s.ep.Close()
}

// NewTag returns a random SIP dialog tag (RFC 3261 style).
func NewTag() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "lingllmtag"
	}
	return hex.EncodeToString(b[:])
}

func detectOutboundIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return ""
	}
	defer conn.Close()
	la, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || la.IP == nil || la.IP.IsUnspecified() {
		return ""
	}
	return la.IP.String()
}
