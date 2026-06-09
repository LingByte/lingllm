package transfer

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/LingByte/lingllm/protocol/sip/historyinfo"
	"github.com/LingByte/lingllm/protocol/sip/outbound"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/LingByte/lingllm/protocol/sip/uas"
	"github.com/sirupsen/logrus"
)

// RetargetSource supplies inbound History-Info / Diversion context for outbound retarget INVITEs.
// Implementations typically read from protocol/sipmedia/session.CallSession metadata.
type RetargetSource func(inboundCallID string) (toHdr, historyInfo, diversion string)

// Coordinator wires REFER signaling and outbound transfer dial (signaling only).
// Media bridging lives in protocol/sipmedia/transferbridge.
type Coordinator struct {
	Dialogs  *DialogStore
	LocalIP  string
	SIPPort  int
	Dial     func(ctx context.Context, req outbound.DialRequest) (string, error)
	SendSIP  func(msg *stack.Message, addr *net.UDPAddr) error
	Retarget RetargetSource

	mu              sync.Mutex
	started         map[string]bool
	referNotifyWait map[string]func(sipfrag, subState string)
}

// Config initializes a signaling-only transfer coordinator.
type Config struct {
	Dialogs  *DialogStore
	LocalIP  string
	SIPPort  int
	Dial     func(ctx context.Context, req outbound.DialRequest) (string, error)
	SendSIP  func(msg *stack.Message, addr *net.UDPAddr) error
	Retarget RetargetSource
}

// NewCoordinator builds a transfer coordinator.
func NewCoordinator(cfg Config) *Coordinator {
	dl := cfg.Dialogs
	if dl == nil {
		dl = NewDialogStore()
	}
	port := cfg.SIPPort
	if port <= 0 {
		port = 5060
	}
	return &Coordinator{
		Dialogs:         dl,
		LocalIP:         cfg.LocalIP,
		SIPPort:         port,
		Dial:            cfg.Dial,
		SendSIP:         cfg.SendSIP,
		Retarget:        cfg.Retarget,
		started:         make(map[string]bool),
		referNotifyWait: make(map[string]func(string, string)),
	}
}

// HandleRefer answers inbound REFER with 202 and starts outbound transfer asynchronously.
func (c *Coordinator) HandleRefer(req *stack.Message, addr *net.UDPAddr) (*stack.Message, error) {
	if c == nil || req == nil {
		return nil, fmt.Errorf("sip/transfer: nil refer handler")
	}
	callID := normCallID(req.GetHeader(stack.HeaderCallID))
	if callID == "" {
		return uas.ErrorResponse(req, 400, "Bad Request")
	}
	if c.Dialogs.Get(callID) == nil {
		return uas.ErrorResponse(req, 481, "Call/Transaction Does Not Exist")
	}
	referTo := strings.TrimSpace(req.GetHeader(stack.HeaderReferTo))
	if referTo == "" {
		return uas.ErrorResponse(req, 400, "Bad Request")
	}
	go c.runReferSequence(context.Background(), callID, referTo)
	resp, err := uas.NewResponse(req, 202, "Accepted", "", "")
	if err != nil {
		return nil, err
	}
	resp.SetHeader(stack.HeaderContentLength, "0")
	return resp, nil
}

func (c *Coordinator) runReferSequence(ctx context.Context, inboundCallID, referTo string) {
	c.sendReferNotify(inboundCallID, "SIP/2.0 100 Trying", "active;expires=60")
	c.TriggerFromReferTo(ctx, inboundCallID, referTo, func(frag, subState string) {
		c.sendReferNotify(inboundCallID, frag, subState)
	})
}

func (c *Coordinator) sendReferNotify(callID, frag, subState string) {
	if c == nil || c.SendSIP == nil || c.Dialogs == nil {
		return
	}
	msg, dst, err := c.Dialogs.BuildNotify(callID, c.LocalIP, c.SIPPort, frag, subState)
	if err != nil || msg == nil || dst == nil {
		logrus.WithFields(logrus.Fields{"call_id": callID, "error": err}).Warn("sip refer: notify failed")
		return
	}
	_ = c.SendSIP(msg, dst)
}

// TriggerFromReferTo dials Refer-To target (signaling); bridge separately via sipmedia.
func (c *Coordinator) TriggerFromReferTo(ctx context.Context, inboundCallID, referToHeader string, onTerminalNotify func(sipfrag, subState string)) {
	if c == nil {
		return
	}
	referToHeader = strings.TrimSpace(referToHeader)
	inboundCallID = normCallID(inboundCallID)
	if inboundCallID == "" || referToHeader == "" {
		return
	}
	tgt, err := outbound.DialTargetFromReferTo(referToHeader)
	if err != nil {
		logrus.WithFields(logrus.Fields{"call_id": inboundCallID, "error": err}).Warn("sip refer: bad Refer-To")
		if onTerminalNotify != nil {
			onTerminalNotify("SIP/2.0 400 Bad Request", "terminated;reason=giveup")
		}
		return
	}
	c.triggerOutbound(ctx, inboundCallID, tgt, `SIP;cause=302;text="REFER"`, historyinfo.DiversionDeflection, onTerminalNotify)
}

// TriggerToAgent dials an agent/trunk target for AI or ACD-driven transfer.
func (c *Coordinator) TriggerToAgent(ctx context.Context, inboundCallID string, tgt outbound.DialTarget, onTerminalNotify func(sipfrag, subState string)) {
	if c == nil {
		return
	}
	c.triggerOutbound(ctx, inboundCallID, tgt, `SIP;cause=302;text="Transfer"`, historyinfo.DiversionUnconditional, onTerminalNotify)
}

func (c *Coordinator) triggerOutbound(
	ctx context.Context,
	inboundCallID string,
	tgt outbound.DialTarget,
	historyReason, diversionReason string,
	onTerminalNotify func(sipfrag, subState string),
) {
	inboundCallID = normCallID(inboundCallID)
	c.mu.Lock()
	if c.started[inboundCallID] {
		c.mu.Unlock()
		logrus.WithField("call_id", inboundCallID).Info("sip transfer: already started")
		return
	}
	c.started[inboundCallID] = true
	c.mu.Unlock()

	if c.Dial == nil {
		c.clearStarted(inboundCallID)
		if onTerminalNotify != nil {
			onTerminalNotify("SIP/2.0 503 Service Unavailable", "terminated;reason=giveup")
		}
		logrus.WithField("call_id", inboundCallID).Warn("sip transfer: Dial not configured")
		return
	}

	var toHdr, hiHdr, dvHdr string
	if c.Retarget != nil {
		toHdr, hiHdr, dvHdr = c.Retarget(inboundCallID)
	}

	go func() {
		req := outbound.DialRequest{
			Scenario:      outbound.ScenarioTransferAgent,
			Target:        tgt,
			CorrelationID: inboundCallID,
		}
		ApplyRetargetHeaders(&req, toHdr, hiHdr, dvHdr, historyReason, diversionReason)
		outCID, err := c.Dial(ctx, req)
		if err != nil {
			c.clearStarted(inboundCallID)
			if onTerminalNotify != nil {
				onTerminalNotify("SIP/2.0 503 Service Unavailable", "terminated;reason=giveup")
			}
			logrus.WithFields(logrus.Fields{"inbound": inboundCallID, "error": err}).Warn("sip transfer: dial failed")
			return
		}
		if onTerminalNotify != nil && outCID != "" {
			c.mu.Lock()
			c.referNotifyWait[outCID] = onTerminalNotify
			c.mu.Unlock()
		}
		logrus.WithFields(logrus.Fields{"inbound": inboundCallID, "outbound": outCID}).Info("sip transfer: outbound INVITE sent")
	}()
}

func (c *Coordinator) clearStarted(inboundCallID string) {
	c.mu.Lock()
	delete(c.started, inboundCallID)
	c.mu.Unlock()
}

// HandleDialEvent should be wired to outbound.ManagerConfig.OnEvent.
// Fires pending REFER NOTIFY on established/failed. Media bridge is handled by sipmedia/transferbridge.
func (c *Coordinator) HandleDialEvent(evt outbound.DialEvent) {
	if c == nil || evt.Scenario != outbound.ScenarioTransferAgent {
		return
	}
	inbound := normCallID(evt.CorrelationID)
	switch evt.State {
	case outbound.DialEventEstablished, outbound.DialEventFailed:
		if evt.State == outbound.DialEventEstablished {
			c.clearStarted(inbound)
		} else {
			c.clearStarted(inbound)
		}
		c.fireReferNotify(evt)
	}
}

func (c *Coordinator) fireReferNotify(evt outbound.DialEvent) {
	cid := normCallID(evt.CallID)
	if cid == "" {
		return
	}
	c.mu.Lock()
	fn := c.referNotifyWait[cid]
	delete(c.referNotifyWait, cid)
	c.mu.Unlock()
	if fn == nil {
		return
	}
	switch evt.State {
	case outbound.DialEventEstablished:
		fn("SIP/2.0 200 OK", "terminated;reason=noresource")
	case outbound.DialEventFailed:
		code := evt.StatusCode
		reason := strings.TrimSpace(evt.StatusText)
		if code <= 0 {
			code = 503
		}
		if reason == "" {
			reason = "Service Unavailable"
		}
		fn(fmt.Sprintf("SIP/2.0 %d %s", code, reason), "terminated;reason=giveup")
	}
}

// HandleDialogCallIDAdopted rekeys pending REFER NOTIFY when SBC rewrites outbound Call-ID.
func (c *Coordinator) HandleDialogCallIDAdopted(oldID, newID, _ string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if fn := c.referNotifyWait[oldID]; fn != nil {
		delete(c.referNotifyWait, oldID)
		c.referNotifyWait[newID] = fn
	}
	c.mu.Unlock()
}

// SendInboundBye sends BYE on the inbound dialog and forgets dialog state.
func (c *Coordinator) SendInboundBye(callID string) error {
	if c == nil || c.SendSIP == nil || c.Dialogs == nil {
		return fmt.Errorf("sip/transfer: not configured")
	}
	msg, dst, err := c.Dialogs.BuildBye(callID, c.LocalIP, c.SIPPort)
	if err != nil {
		return err
	}
	if err := c.SendSIP(msg, dst); err != nil {
		return err
	}
	c.Dialogs.Forget(callID)
	return nil
}
