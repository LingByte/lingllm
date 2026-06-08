package outbound

import (
	"time"

	"github.com/LingByte/lingllm/protocol/sip/historyinfo"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
)

// Scenario classifies why an outbound leg exists.
type Scenario string

const (
	ScenarioCampaign      Scenario = "campaign"
	ScenarioTransferAgent Scenario = "transfer_agent"
	ScenarioCallback      Scenario = "callback"
	ScenarioManual        Scenario = "manual"
)

// DialTarget describes the next SIP hop for an outbound INVITE.
type DialTarget struct {
	RequestURI        string // e.g. sip:1001@192.168.1.10;user=phone
	SignalingAddr     string // host:port of proxy or UAS
	CallerUser        string
	CallerDisplayName string
	Transport         Transport
}

// DialRequest is one outbound signaling attempt (no media binding).
type DialRequest struct {
	Scenario      Scenario
	Target        DialTarget
	CorrelationID string
	ScriptID      string

	CallerUser        string
	CallerDisplayName string

	AssertedIdentityURI         string
	AssertedIdentityDisplayName string
	PrivacyTokens               []string
	HistoryInfo                 []historyinfo.Entry
	Diversion                   []historyinfo.Diversion

	// RTPPort is advertised in the SDP offer only (signaling demo default 10000).
	RTPPort int
	// Codecs overrides the default outbound offer list when non-empty.
	Codecs []sdp.Codec
	// OfferSRTP adds SDES a=crypto to the SDP offer (RTP/SAVPF).
	OfferSRTP bool
}

// EstablishedLeg is delivered after 200 OK + ACK (signaling complete).
type EstablishedLeg struct {
	CallID        string
	Scenario      Scenario
	CorrelationID string
	CreatedAt     time.Time

	FromHeader          string
	ToHeader            string
	RemoteSignalingAddr string
	CSeqInvite          string

	Answer *sdp.Info
}

const (
	DialEventInvited     = "invited"
	DialEventProvisional = "provisional"
	DialEventEstablished = "established"
	DialEventFailed      = "failed"
)

// DialEvent streams dial lifecycle transitions.
type DialEvent struct {
	CallID        string
	CorrelationID string
	Scenario      Scenario
	State         string
	StatusCode    int
	Reason        string
	At            time.Time
	RequestURI    string
	StatusText    string
	RemoteAddr    string
}
