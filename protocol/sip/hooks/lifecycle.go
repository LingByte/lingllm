package hooks

import "time"

// Direction classifies a SIP media leg.
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// HangupInitiator identifies who ended the dialog.
type HangupInitiator string

const (
	HangupLocal   HangupInitiator = "local"
	HangupRemote  HangupInitiator = "remote"
	HangupUnknown HangupInitiator = "unknown"
)

// CallMeta is the minimal call context passed to lifecycle sinks.
type CallMeta struct {
	CallID        string
	CorrelationID string // inbound Call-ID for outbound transfer legs
	Direction     Direction
	Scenario      string // e.g. outbound.ScenarioTransferAgent — opaque string avoids import cycles
	TenantID      uint
	RemoteAddr    string
	LocalIP       string
	Codec         string
	StartedAt     time.Time
}

// HangupMeta accompanies OnBye / OnFailed.
type HangupMeta struct {
	Initiator HangupInitiator
	SIPCode   int    // final SIP status when known (0 if N/A)
	Reason    string // SIP phrase or internal reason
	Duration  time.Duration
	EndedAt   time.Time
}

// LifecycleSink receives call state transitions. All methods must be non-blocking
// or fast; heavy work should be queued by the implementation.
type LifecycleSink interface {
	// OnRinging is an optional early hook (180/183). No-op default: use NopLifecycle.
	OnRinging(meta CallMeta)

	// OnEstablished fires after ACK (inbound UAS) or outbound 200+ACK.
	OnEstablished(meta CallMeta)

	// OnTransferStarted fires when a B2BUA transfer dial begins (REFER or AI transfer).
	OnTransferStarted(inbound CallMeta, targetURI string)

	// OnBridgeStarted fires when two-leg media bridge is active.
	OnBridgeStarted(inboundCallID, outboundCallID string)

	// OnBye fires when a dialog ends cleanly (BYE completed or local teardown).
	OnBye(meta CallMeta, hangup HangupMeta, recording RecordingArtifact)

	// OnFailed fires when setup or transfer fails before a stable bridge.
	OnFailed(meta CallMeta, hangup HangupMeta)
}

// NopLifecycle is a sink that ignores all events.
type NopLifecycle struct{}

func (NopLifecycle) OnRinging(CallMeta)                            {}
func (NopLifecycle) OnEstablished(CallMeta)                        {}
func (NopLifecycle) OnTransferStarted(CallMeta, string)            {}
func (NopLifecycle) OnBridgeStarted(string, string)                {}
func (NopLifecycle) OnBye(CallMeta, HangupMeta, RecordingArtifact) {}
func (NopLifecycle) OnFailed(CallMeta, HangupMeta)                 {}
