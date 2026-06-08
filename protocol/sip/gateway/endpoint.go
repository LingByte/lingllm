package gateway

import (
	"context"
	"fmt"
	"net"

	"github.com/LingByte/lingllm/protocol/sip/outbound"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

// EndpointConfig combines inbound UAS and outbound UAC on one UDP socket.
type EndpointConfig struct {
	UASConfig
	Outbound outbound.ManagerConfig
}

// Endpoint is a bidirectional SIP signaling endpoint (UAS + UAC).
type Endpoint struct {
	uas      *UAS
	outbound *outbound.Manager
}

// NewEndpoint builds a combined signaling endpoint.
func NewEndpoint(cfg EndpointConfig) *Endpoint {
	u := NewUAS(cfg.UASConfig)
	obCfg := cfg.Outbound
	if obCfg.SIPHost == "" {
		obCfg.SIPHost = cfg.UASConfig.Host
	}
	if obCfg.SIPPort <= 0 {
		obCfg.SIPPort = cfg.UASConfig.Port
	}
	if obCfg.LocalIP == "" {
		obCfg.LocalIP = cfg.UASConfig.LocalIP
	}
	ob := outbound.NewManager(obCfg)
	u.AttachOutbound(ob)
	return &Endpoint{uas: u, outbound: ob}
}

// UAS returns the inbound server wrapper.
func (e *Endpoint) UAS() *UAS { return e.uas }

// Outbound returns the outbound manager.
func (e *Endpoint) Outbound() *outbound.Manager { return e.outbound }

// LocalIP returns the SDP/signaling local IP.
func (e *Endpoint) LocalIP() string {
	if e == nil || e.uas == nil {
		return ""
	}
	return e.uas.LocalIP()
}

// Open binds UDP and wires outbound send on the shared socket.
func (e *Endpoint) Open() error {
	if e == nil || e.uas == nil || e.outbound == nil {
		return fmt.Errorf("sip/gateway: nil endpoint")
	}
	if err := e.uas.Open(); err != nil {
		return err
	}
	ep := e.uas.Endpoint()
	e.outbound.BindSender(sharedSender{ep: ep})
	return nil
}

type sharedSender struct{ ep *stack.Endpoint }

func (s sharedSender) SendSIP(msg *stack.Message, addr *net.UDPAddr) error {
	return s.ep.Send(msg, addr)
}

// Serve runs the UDP read loop until ctx is cancelled.
func (e *Endpoint) Serve(ctx context.Context) error {
	if e == nil || e.uas == nil {
		return fmt.Errorf("sip/gateway: not open")
	}
	return e.uas.Serve(ctx)
}

// Close shuts down UDP and outbound connection pool.
func (e *Endpoint) Close() error {
	if e == nil {
		return nil
	}
	if e.outbound != nil {
		_ = e.outbound.ClosePool()
	}
	if e.uas != nil {
		return e.uas.Close()
	}
	return nil
}

// Dial starts an outbound INVITE using the shared socket.
func (e *Endpoint) Dial(ctx context.Context, req outbound.DialRequest) (string, error) {
	if e == nil || e.outbound == nil {
		return "", fmt.Errorf("sip/gateway: not open")
	}
	return e.outbound.Dial(ctx, req)
}

// Hangup sends BYE for an established outbound leg.
func (e *Endpoint) Hangup(callID string) error {
	if e == nil || e.outbound == nil {
		return fmt.Errorf("sip/gateway: not open")
	}
	return e.outbound.SendBYE(callID)
}

// Cancel sends CANCEL for a ringing outbound leg.
func (e *Endpoint) Cancel(callID string) error {
	if e == nil || e.outbound == nil {
		return fmt.Errorf("sip/gateway: not open")
	}
	return e.outbound.SendCANCEL(callID)
}
