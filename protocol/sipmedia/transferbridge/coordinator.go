package transferbridge

import (
	"github.com/LingByte/lingllm/protocol/sip/outbound"
	siptransfer "github.com/LingByte/lingllm/protocol/sip/transfer"
	"github.com/LingByte/lingllm/protocol/sipmedia/session"
	"github.com/sirupsen/logrus"
)

// SessionLookup resolves media sessions by Call-ID.
type SessionLookup func(callID string) *session.CallSession

// MediaCoordinator composes signaling transfer (sip/transfer) with media bridging.
type MediaCoordinator struct {
	Signaling *siptransfer.Coordinator
	Bridges   *Manager
	Lookup    SessionLookup
}

// NewMediaCoordinator builds a coordinator with a fresh bridge manager when nil.
func NewMediaCoordinator(sig *siptransfer.Coordinator, lookup SessionLookup, bridges *Manager) *MediaCoordinator {
	if bridges == nil {
		bridges = NewManager()
	}
	return &MediaCoordinator{Signaling: sig, Bridges: bridges, Lookup: lookup}
}

// HandleDialEvent wires outbound dial events: REFER NOTIFY via signaling, bridge on established.
func (m *MediaCoordinator) HandleDialEvent(evt outbound.DialEvent) {
	if m == nil || m.Signaling == nil {
		return
	}
	m.Signaling.HandleDialEvent(evt)
	if evt.Scenario != outbound.ScenarioTransferAgent {
		return
	}
	inbound := evt.CorrelationID
	outboundID := evt.CallID
	switch evt.State {
	case outbound.DialEventEstablished:
		if m.Lookup == nil || m.Bridges == nil {
			return
		}
		inSess := m.Lookup(inbound)
		outSess := m.Lookup(outboundID)
		if inSess == nil || outSess == nil {
			logrus.WithFields(logrus.Fields{"inbound": inbound, "outbound": outboundID}).Warn("sipmedia transfer: bridge skipped (session missing)")
			return
		}
		if err := m.Bridges.StartBridge(inSess, outSess); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"inbound": inbound, "outbound": outboundID}).Warn("sipmedia transfer: bridge failed")
		}
	}
}

// HandleDialogCallIDAdopted rekeys the media bridge when outbound Call-ID changes.
func (m *MediaCoordinator) HandleDialogCallIDAdopted(oldID, newID, correlationID string) {
	if m == nil {
		return
	}
	if m.Signaling != nil {
		m.Signaling.HandleDialogCallIDAdopted(oldID, newID, correlationID)
	}
	if m.Bridges != nil {
		m.Bridges.MigrateOutboundCallID(correlationID, oldID, newID)
	}
}
