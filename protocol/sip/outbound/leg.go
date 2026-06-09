package outbound

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sipMetrics "github.com/LingByte/lingllm/protocol/sip/metrics"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/session_timer"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/sirupsen/logrus"
)

type outLeg struct {
	m      *Manager
	params inviteParams
	req    DialRequest
	dst    *net.UDPAddr

	transport Transport
	peerMu    sync.Mutex
	peer      signalingPeer

	mu          sync.Mutex
	established bool
	answer      *sdp.Info

	sigMu         sync.Mutex
	byeToHeader   string
	byeRequestURI string
	byeRemote     *net.UDPAddr
	byeCSeqNext   int
	txKey         string

	refreshMu sync.Mutex
	refresher *outRefresher

	gotProvisional atomic.Bool
	pendingCancel  atomic.Bool
	cancelSent     atomic.Bool
	cancelStopMu   sync.Mutex
	cancelStop     chan struct{}
}

func (leg *outLeg) handleResponse(ctx context.Context, resp *stack.Message, from *net.UDPAddr) {
	if leg == nil || resp == nil {
		return
	}
	st := resp.StatusCode
	cseqAll := strings.ToUpper(resp.GetHeader(stack.HeaderCSeq))

	if strings.Contains(cseqAll, "BYE") {
		if st >= 200 && st < 300 {
			sipMetrics.Bye(sipMetrics.ByeByLocal, sipMetrics.ByeReasonNormal)
			leg.cleanupLeg()
		}
		return
	}

	if strings.Contains(cseqAll, "INVITE") {
		sipMetrics.InviteResult(sipMetrics.DirectionOutbound, st)
	}

	if strings.Contains(cseqAll, stack.MethodUpdate) {
		leg.refreshMu.Lock()
		r := leg.refresher
		leg.refreshMu.Unlock()
		if r != nil && !r.handleUPDATEResponse(resp) {
			leg.stopRefresher()
		}
		return
	}

	if st >= 100 && st < 200 {
		phrase := strings.TrimSpace(resp.StatusText)
		if leg.gotProvisional.CompareAndSwap(false, true) {
			if leg.pendingCancel.Load() {
				go leg.fireDeferredCANCEL()
			}
		}
		logrus.WithFields(logrus.Fields{
			"call_id":        leg.params.CallID,
			"status":         st,
			"reason_phrase":  phrase,
			"remote":         udpAddrString(from),
			"scenario":       leg.req.Scenario,
			"correlation_id": leg.req.CorrelationID,
		}).Info("sip outbound provisional")
		leg.emitEvent(DialEventProvisional, st, phrase, from)
		return
	}

	if st != 200 {
		reason := strings.TrimSpace(resp.StatusText)
		if reason == "" {
			reason = "non_200"
		}
		leg.stopCANCELRetransmit()
		logrus.WithFields(logrus.Fields{
			"call_id":        leg.params.CallID,
			"status":         st,
			"reason_phrase":  reason,
			"remote":         udpAddrString(from),
			"request_uri":    leg.params.RequestURI,
			"correlation_id": leg.req.CorrelationID,
			"body_preview":   previewBody(resp.Body, 200),
		}).Warn("sip outbound non-200")
		leg.emitEvent(DialEventFailed, st, reason, from)
		leg.cleanupLeg()
		return
	}

	if !strings.Contains(cseqAll, "INVITE") {
		return
	}

	leg.mu.Lock()
	if leg.established {
		leg.mu.Unlock()
		return
	}
	leg.mu.Unlock()

	if strings.TrimSpace(resp.Body) == "" {
		logrus.WithField("call_id", leg.params.CallID).Warn("sip outbound 200 OK without SDP")
		leg.cleanupLeg()
		return
	}

	answer, err := sdp.Parse(resp.Body)
	if err != nil {
		logrus.WithFields(logrus.Fields{"call_id": leg.params.CallID, "error": err}).Warn("sip outbound bad answer SDP")
		leg.cleanupLeg()
		return
	}
	if answer.Port <= 0 {
		logrus.WithField("call_id", leg.params.CallID).Warn("sip outbound invalid RTP in answer")
		leg.cleanupLeg()
		return
	}

	leg.m.adoptOutboundDialogCallIDIfNeeded(leg, resp)

	if leg.m.cfg.PreAck != nil {
		fromH := formatOutboundFromHeader(leg.params.FromDisplayName, leg.params.FromUser,
			leg.params.SIPHost, leg.params.SIPPort, leg.params.FromTag)
		if err := leg.m.cfg.PreAck(ctx, PreAckContext{
			Leg: EstablishedLeg{
				CallID:              leg.params.CallID,
				Scenario:            leg.req.Scenario,
				CorrelationID:       leg.req.CorrelationID,
				FromHeader:          fromH,
				ToHeader:            resp.GetHeader(stack.HeaderTo),
				RemoteSignalingAddr: udpAddrString(leg.dst),
				CSeqInvite:          fmt.Sprintf("%d INVITE", leg.params.CSeq),
				Answer:              answer,
			},
			Answer:         answer,
			ResponseSource: from,
		}); err != nil {
			logrus.WithFields(logrus.Fields{
				"call_id": leg.params.CallID,
				"error":   err,
			}).Warn("sip outbound pre-ack hook failed")
			leg.cleanupLeg()
			return
		}
	}

	ackURI := ackRequestURI(resp, leg.params.RequestURI)
	ack := buildACK(leg.params, resp, ackURI)
	if ack == nil {
		leg.cleanupLeg()
		return
	}
	if err := leg.sendOnPeer(ack, from); err != nil {
		logrus.WithFields(logrus.Fields{"call_id": leg.params.CallID, "error": err}).Warn("sip outbound ACK failed")
		leg.cleanupLeg()
		return
	}

	leg.sigMu.Lock()
	leg.byeToHeader = resp.GetHeader(stack.HeaderTo)
	leg.byeRequestURI = ackRequestURI(resp, leg.params.RequestURI)
	if from != nil {
		leg.byeRemote = cloneUDPAddr(from)
	} else {
		leg.byeRemote = cloneUDPAddr(leg.dst)
	}
	leg.byeCSeqNext = leg.params.CSeq + 1
	leg.sigMu.Unlock()

	leg.mu.Lock()
	leg.established = true
	leg.answer = answer
	leg.mu.Unlock()

	if peerSE, peerRefresher, _ := session_timer.ParseSessionExpires(resp.GetHeader(stack.HeaderSessionExpires)); peerSE > 0 {
		leg.startRefresherIfUAC(peerSE, peerRefresher)
	}

	if leg.m.cfg.OnEstablished != nil {
		fromH := formatOutboundFromHeader(leg.params.FromDisplayName, leg.params.FromUser,
			leg.params.SIPHost, leg.params.SIPPort, leg.params.FromTag)
		leg.m.cfg.OnEstablished(EstablishedLeg{
			CallID:              leg.params.CallID,
			Scenario:            leg.req.Scenario,
			CorrelationID:       leg.req.CorrelationID,
			CreatedAt:           time.Now(),
			FromHeader:          fromH,
			ToHeader:            resp.GetHeader(stack.HeaderTo),
			RemoteSignalingAddr: udpAddrString(leg.dst),
			CSeqInvite:          fmt.Sprintf("%d INVITE", leg.params.CSeq),
			Answer:              answer,
		})
	}
	leg.emitEvent(DialEventEstablished, 200, strings.TrimSpace(resp.StatusText), from)

	codec := ""
	if len(answer.Codecs) > 0 {
		codec = answer.Codecs[0].Name
	}
	logrus.WithFields(logrus.Fields{
		"call_id":        leg.params.CallID,
		"correlation_id": leg.req.CorrelationID,
		"scenario":       leg.req.Scenario,
		"codec":          codec,
		"remote_rtp":     fmt.Sprintf("%s:%d", answer.IP, answer.Port),
	}).Info("sip outbound established")
}

func (leg *outLeg) emitEvent(state string, code int, reason string, from *net.UDPAddr) {
	if leg == nil || leg.m == nil || leg.m.cfg.OnEvent == nil {
		return
	}
	leg.m.cfg.OnEvent(DialEvent{
		CallID:        leg.params.CallID,
		CorrelationID: leg.req.CorrelationID,
		Scenario:      leg.req.Scenario,
		State:         state,
		StatusCode:    code,
		Reason:        reason,
		StatusText:    reason,
		RemoteAddr:    udpAddrString(from),
		RequestURI:    leg.req.Target.RequestURI,
		At:            time.Now(),
	})
}

func (leg *outLeg) cleanupLeg() {
	if leg == nil || leg.m == nil {
		return
	}
	leg.stopRefresher()
	leg.stopCANCELRetransmit()
	callID := leg.params.CallID
	m := leg.m
	m.mu.Lock()
	delete(m.legs, callID)
	if leg.txKey != "" {
		delete(m.legsByTx, leg.txKey)
	}
	m.mu.Unlock()
	leg.peerMu.Lock()
	leg.peer = nil
	leg.peerMu.Unlock()
	if m.cfg.OnLegRemoved != nil {
		m.cfg.OnLegRemoved(callID)
	}
}
