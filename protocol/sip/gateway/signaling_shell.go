package gateway

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/LingByte/lingllm/protocol/sip/transaction"
)

// SignalingShell is the shared UDP signaling core used by gateway.UAS and
// VoiceServer SIPServer: one stack.Endpoint + transaction.Manager without
// pre-registered uas.Handlers (callers register their own methods).
type SignalingShell struct {
	cfg UASConfig
	hooks stack.EndpointConfig

	ep  *stack.Endpoint
	mgr *transaction.Manager
}

// SignalingShellConfig wires listen addresses and stack.Endpoint callbacks.
// Host/Port/LocalIP come from UASConfig; other EndpointConfig fields
// (OnSIPResponse, OnEvent, OnMessageSent, …) are copied from Hooks.
type SignalingShellConfig struct {
	UASConfig
	Hooks stack.EndpointConfig
}

// NewSignalingShell builds the endpoint (handlers can be registered before Open).
func NewSignalingShell(cfg SignalingShellConfig) *SignalingShell {
	if strings.TrimSpace(cfg.Host) == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port <= 0 {
		cfg.Port = 5060
	}
	epCfg := cfg.Hooks
	epCfg.Host = cfg.Host
	epCfg.Port = cfg.Port
	return &SignalingShell{
		cfg:   cfg.UASConfig,
		hooks: cfg.Hooks,
		ep:    stack.NewEndpoint(epCfg),
		mgr:   transaction.NewManager(),
	}
}

// Open binds the UDP socket.
func (s *SignalingShell) Open() error {
	if s == nil {
		return fmt.Errorf("sip/gateway: nil SignalingShell")
	}
	if s.ep == nil {
		return fmt.Errorf("sip/gateway: SignalingShell missing endpoint")
	}
	if err := s.ep.Open(); err != nil {
		return err
	}
	if s.cfg.LocalIP == "" {
		if ip := detectOutboundIP(); ip != "" {
			s.cfg.LocalIP = ip
		}
	}
	return nil
}

// Endpoint returns the stack endpoint after Open.
func (s *SignalingShell) Endpoint() *stack.Endpoint {
	if s == nil {
		return nil
	}
	return s.ep
}

// TxManager returns the transaction manager.
func (s *SignalingShell) TxManager() *transaction.Manager {
	if s == nil {
		return nil
	}
	return s.mgr
}

// Send writes on the bound UDP socket.
func (s *SignalingShell) Send(msg *stack.Message, addr *net.UDPAddr) error {
	if s == nil || s.ep == nil {
		return fmt.Errorf("sip/gateway: SignalingShell not open")
	}
	return s.ep.Send(msg, addr)
}

// Serve runs the UDP read loop until ctx is cancelled.
func (s *SignalingShell) Serve(ctx context.Context) error {
	if s == nil || s.ep == nil {
		return fmt.Errorf("sip/gateway: SignalingShell not open")
	}
	return s.ep.Serve(ctx)
}

// Close shuts down the UDP socket.
func (s *SignalingShell) Close() error {
	if s == nil || s.ep == nil {
		return nil
	}
	return s.ep.Close()
}

// ListenAddr returns the bound UDP address.
func (s *SignalingShell) ListenAddr() net.Addr {
	if s == nil || s.ep == nil {
		return nil
	}
	return s.ep.ListenAddr()
}

// SIPPort returns the configured or bound signaling port.
func (s *SignalingShell) SIPPort() int {
	if s == nil {
		return 5060
	}
	if s.cfg.Port > 0 {
		return s.cfg.Port
	}
	if s.ep != nil {
		if la := s.ep.ListenAddr(); la != nil {
			if _, portStr, err := net.SplitHostPort(la.String()); err == nil {
				if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
					return p
				}
			}
		}
	}
	return 5060
}

// LocalIP returns the configured or detected signaling IP.
func (s *SignalingShell) LocalIP() string {
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
