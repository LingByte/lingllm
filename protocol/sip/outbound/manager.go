package outbound

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/sirupsen/logrus"
)

// SignalingSender sends SIP on a shared UDP socket.
type SignalingSender interface {
	SendSIP(msg *stack.Message, addr *net.UDPAddr) error
}

// ManagerConfig configures outbound signaling (no RTP/media).
type ManagerConfig struct {
	LocalIP         string // SDP c= line
	SIPHost         string // Via / Contact host
	SIPPort         int
	FromUser        string
	FromDisplayName string
	DefaultRTPPort  int

	OnEstablished         func(EstablishedLeg)
	OnEvent               func(DialEvent)
	OnDialogCallIDAdopted func(oldID, newID, correlationID string)
	OnSignalingSent       func(*stack.Message, *net.UDPAddr)
	TLSConfig             *tls.Config
}

// Manager owns outbound SIP legs keyed by Call-ID.
type Manager struct {
	cfg  ManagerConfig
	send func(*stack.Message, *net.UDPAddr) error

	poolMu sync.Mutex
	pool   *signalingPool

	mu       sync.Mutex
	legs     map[string]*outLeg
	legsByTx map[string]*outLeg
}

// NewManager constructs a manager; call BindSender before Dial.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.FromUser == "" {
		cfg.FromUser = "lingllm"
	}
	if cfg.DefaultRTPPort <= 0 {
		cfg.DefaultRTPPort = 10000
	}
	return &Manager{
		cfg:      cfg,
		legs:     make(map[string]*outLeg),
		legsByTx: make(map[string]*outLeg),
	}
}

// BindSender wires the UDP signaling path (required for Dial).
func (m *Manager) BindSender(s SignalingSender) {
	if m == nil || s == nil {
		return
	}
	m.send = func(msg *stack.Message, addr *net.UDPAddr) error {
		if m.cfg.OnSignalingSent != nil {
			m.cfg.OnSignalingSent(msg, addr)
		}
		return s.SendSIP(msg, addr)
	}
}

func (m *Manager) signalingPoolForDial() *signalingPool {
	if m == nil {
		return nil
	}
	m.poolMu.Lock()
	defer m.poolMu.Unlock()
	if m.pool != nil {
		return m.pool
	}
	m.pool = newSignalingPool(poolConfig{
		UDPSend:      m.send,
		ResponseSink: m.HandleSIPResponse,
		TLSConfig:    m.cfg.TLSConfig,
	})
	return m.pool
}

// ClosePool shuts pooled TCP/TLS connections.
func (m *Manager) ClosePool() error {
	if m == nil {
		return nil
	}
	m.poolMu.Lock()
	pool := m.pool
	m.pool = nil
	m.poolMu.Unlock()
	if pool == nil {
		return nil
	}
	return pool.Close()
}

// HandleSIPResponse routes inbound responses to outbound legs.
func (m *Manager) HandleSIPResponse(resp *stack.Message, addr *net.UDPAddr) {
	if m == nil || resp == nil {
		return
	}
	txKey := txKeyFromResponse(resp)
	callID := strings.TrimSpace(resp.GetHeader("Call-ID"))
	m.mu.Lock()
	leg := (*outLeg)(nil)
	if txKey != "" {
		leg = m.legsByTx[txKey]
	}
	if leg == nil && callID != "" {
		leg = m.legs[callID]
	}
	m.mu.Unlock()
	if leg == nil {
		logrus.WithFields(logrus.Fields{
			"call_id": callID,
			"tx_key":  txKey,
			"status":  resp.StatusCode,
			"remote":  udpAddrString(addr),
		}).Debug("sip outbound unmatched response")
		return
	}
	cseqHdr := strings.ToUpper(strings.TrimSpace(resp.GetHeader("CSeq")))
	if strings.Contains(cseqHdr, "INVITE") && resp.StatusCode < 300 {
		m.adoptOutboundDialogCallIDIfNeeded(leg, resp)
	}
	leg.handleResponse(context.Background(), resp, addr)
}

// Dial starts an outbound INVITE. Returns Call-ID on success.
func (m *Manager) Dial(ctx context.Context, req DialRequest) (callID string, err error) {
	if m == nil {
		return "", fmt.Errorf("sip/outbound: nil manager")
	}
	if m.send == nil {
		return "", ErrNoSignalingSender
	}
	if strings.TrimSpace(req.Target.RequestURI) == "" {
		return "", fmt.Errorf("sip/outbound: empty target request URI")
	}
	if strings.TrimSpace(req.Target.SignalingAddr) == "" {
		return "", fmt.Errorf("sip/outbound: empty signaling address")
	}

	sigHost := strings.TrimSpace(m.cfg.SIPHost)
	if sigHost == "" {
		sigHost = "127.0.0.1"
	}
	localSDP := strings.TrimSpace(m.cfg.LocalIP)
	if localSDP == "" {
		localSDP = sigHost
	}
	callID = newCallID(sigHost)

	addr, err := net.ResolveUDPAddr("udp", req.Target.SignalingAddr)
	if err != nil {
		return "", fmt.Errorf("sip/outbound: resolve signaling: %w", err)
	}
	transport := ResolveTransport(req.Target)

	localPort := m.cfg.SIPPort
	if localPort <= 0 {
		localPort = 5060
	}

	rtpPort := req.RTPPort
	if rtpPort <= 0 {
		rtpPort = m.cfg.DefaultRTPPort
	}
	codecs := req.Codecs
	if len(codecs) == 0 {
		codecs = sdp.DefaultOutboundOfferCodecs()
	}

	mediaProto := "RTP/AVP"
	var sdpExtras []string
	if req.OfferSRTP {
		mediaProto = "RTP/SAVPF"
		mkey := make([]byte, 16)
		msalt := make([]byte, 14)
		if _, err := rand.Read(mkey); err != nil {
			return "", fmt.Errorf("sip/outbound: SRTP master key: %w", err)
		}
		if _, err := rand.Read(msalt); err != nil {
			return "", fmt.Errorf("sip/outbound: SRTP master salt: %w", err)
		}
		cryptoLine, cerr := sdp.FormatCryptoLine(1, sdp.SuiteAESCM128HMACSHA180, mkey, msalt)
		if cerr != nil {
			return "", cerr
		}
		sdpExtras = []string{cryptoLine}
	}
	sdpBody := sdp.GenerateWithProtoExtras(localSDP, rtpPort, mediaProto, codecs, sdpExtras)

	fromUser := m.cfg.FromUser
	fromDisp := m.cfg.FromDisplayName
	if u := strings.TrimSpace(req.CallerUser); u != "" {
		fromUser = u
		fromDisp = strings.TrimSpace(req.CallerDisplayName)
	} else if u := strings.TrimSpace(req.Target.CallerUser); u != "" {
		fromUser = u
		fromDisp = strings.TrimSpace(req.Target.CallerDisplayName)
	}

	params := inviteParams{
		LocalIP:                     localSDP,
		SIPHost:                     sigHost,
		SIPPort:                     localPort,
		RequestURI:                  strings.TrimSpace(req.Target.RequestURI),
		CallID:                      callID,
		FromTag:                     randomHex(8),
		Branch:                      randomHex(10),
		CSeq:                        1,
		LocalRTPPort:                rtpPort,
		SDPBody:                     sdpBody,
		FromUser:                    fromUser,
		FromDisplayName:             fromDisp,
		AssertedIdentityURI:         strings.TrimSpace(req.AssertedIdentityURI),
		AssertedIdentityDisplayName: strings.TrimSpace(req.AssertedIdentityDisplayName),
		PrivacyTokens:               req.PrivacyTokens,
		HistoryInfo:                 req.HistoryInfo,
		Diversion:                   req.Diversion,
		ViaTransport:                transport,
	}

	invite := buildINVITE(params)
	leg := &outLeg{
		m:         m,
		params:    params,
		req:       req,
		dst:       addr,
		transport: transport,
		txKey:     inviteTxKey(params.Branch, params.CSeq),
	}

	pool := m.signalingPoolForDial()
	if pool == nil {
		return "", fmt.Errorf("sip/outbound: signaling pool unavailable")
	}
	peer, err := pool.Get(ctx, transport, addr)
	if err != nil {
		return "", fmt.Errorf("sip/outbound: dial %s: %w", transport, err)
	}
	leg.peerMu.Lock()
	leg.peer = peer
	leg.peerMu.Unlock()

	m.mu.Lock()
	m.legs[callID] = leg
	if leg.txKey != "" {
		m.legsByTx[leg.txKey] = leg
	}
	m.mu.Unlock()

	if err := peer.Send(invite); err != nil {
		m.mu.Lock()
		delete(m.legs, callID)
		delete(m.legsByTx, leg.txKey)
		m.mu.Unlock()
		return "", fmt.Errorf("sip/outbound: send INVITE: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"call_id":        callID,
		"request_uri":    req.Target.RequestURI,
		"scenario":       req.Scenario,
		"correlation_id": req.CorrelationID,
		"dst":            addr.String(),
		"transport":      transport,
	}).Info("sip outbound INVITE sent")

	if m.cfg.OnEvent != nil {
		m.cfg.OnEvent(DialEvent{
			CallID:        callID,
			CorrelationID: req.CorrelationID,
			Scenario:      req.Scenario,
			State:         DialEventInvited,
			At:            time.Now(),
			RequestURI:    req.Target.RequestURI,
			RemoteAddr:    addr.String(),
		})
	}
	return callID, nil
}

func (m *Manager) adoptOutboundDialogCallIDIfNeeded(leg *outLeg, resp *stack.Message) {
	if m == nil || leg == nil || resp == nil {
		return
	}
	newCID := strings.TrimSpace(resp.GetHeader("Call-ID"))
	oldCID := strings.TrimSpace(leg.params.CallID)
	if newCID == "" || newCID == oldCID {
		return
	}
	var adopCb func(string, string, string)
	m.mu.Lock()
	if m.legs[oldCID] != leg {
		m.mu.Unlock()
		return
	}
	if ex := m.legs[newCID]; ex != nil && ex != leg {
		m.mu.Unlock()
		logrus.WithFields(logrus.Fields{"old": oldCID, "new": newCID}).Warn("sip outbound: refuse dialog call-id adopt (collision)")
		return
	}
	delete(m.legs, oldCID)
	leg.params.CallID = newCID
	m.legs[newCID] = leg
	adopCb = m.cfg.OnDialogCallIDAdopted
	m.mu.Unlock()
	if adopCb != nil {
		adopCb(oldCID, newCID, strings.TrimSpace(leg.req.CorrelationID))
	}
	logrus.WithFields(logrus.Fields{
		"invite_call_id": oldCID,
		"dialog_call_id": newCID,
	}).Info("sip outbound: dialog call-id adopted")
}

func (m *Manager) legByCallIDOrHostRewrite(callID string) *outLeg {
	callID = strings.TrimSpace(callID)
	if m == nil || callID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if lg := m.legs[callID]; lg != nil {
		return lg
	}
	local, ok := callIDLocalPart(callID)
	if !ok {
		return nil
	}
	for _, lg := range m.legs {
		if lg == nil {
			continue
		}
		if l2, ok2 := callIDLocalPart(lg.params.CallID); ok2 && l2 == local {
			return lg
		}
	}
	return nil
}
