package transferbridge

import (
	"fmt"
	"strings"
	"sync"

	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/media/encoder"
	"github.com/LingByte/lingllm/protocol/sipmedia/bridge"
	siprtp "github.com/LingByte/lingllm/protocol/sipmedia/rtp"
	"github.com/LingByte/lingllm/protocol/sipmedia/session"
	"github.com/sirupsen/logrus"
)

type activeBridge struct {
	inboundID  string
	outboundID string
	stop       func()
}

// Manager tracks active two-leg bridges keyed by inbound or outbound Call-ID.
type Manager struct {
	mu      sync.Mutex
	bridges map[string]*activeBridge
}

// NewManager creates an empty bridge registry.
func NewManager() *Manager {
	return &Manager{bridges: make(map[string]*activeBridge)}
}

// Active reports whether callID participates in a bridge.
func (m *Manager) Active(callID string) bool {
	callID = normCallID(callID)
	if m == nil || callID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.findLocked(callID) != nil
}

// StartBridge stops AI/media on both legs and bridges RTP (raw relay or PCM transcode).
func (m *Manager) StartBridge(inbound, outbound *session.CallSession) error {
	if m == nil {
		return fmt.Errorf("sipmedia/transferbridge: nil manager")
	}
	if inbound == nil || outbound == nil {
		return fmt.Errorf("sipmedia/transferbridge: nil session")
	}
	inID := normCallID(inbound.CallID)
	outID := normCallID(outbound.CallID)
	if inID == "" || outID == "" {
		return fmt.Errorf("sipmedia/transferbridge: empty call-id")
	}

	inbound.StopMediaPreserveRTP()
	outbound.StopMediaPreserveRTP()

	ccIn := inbound.SourceCodec()
	ccOut := outbound.SourceCodec()
	var (
		stop func()
		mode string
	)

	if bridge.CanRawDatagramRelay(ccIn, ccOut) {
		relay, err := bridge.NewTwoLegPayloadRelay(
			inbound.RTPSession(), outbound.RTPSession(),
			ccIn, ccOut,
			inbound.DTMFPayloadType(), outbound.DTMFPayloadType(),
		)
		if err == nil {
			dec := rawRelayDecoderFor(ccIn)
			relay.SetInboundRecording(
				func(seq uint16, ts uint32, p []byte) {
					inbound.AppendRecordingSample(session.RecordingDirUser, seq, ts, p)
					if dec != nil {
						if pcm, derr := dec(p); derr == nil {
							inbound.WriteCallerPCM(pcm)
						}
					}
				},
				func(seq uint16, ts uint32, p []byte) {
					inbound.AppendRecordingSample(session.RecordingDirAI, seq, ts, p)
					if dec != nil {
						if pcm, derr := dec(p); derr == nil {
							inbound.WriteAIPCM(pcm)
						}
					}
				},
			)
			relay.Start()
			stop = relay.Stop
			mode = "raw_rtp"
		}
	}

	if stop == nil {
		callerRx := siprtp.NewSIPRTPTransport(inbound.RTPSession(), ccIn, media.DirectionInput, inbound.DTMFPayloadType())
		callerTx := siprtp.NewSIPRTPTransport(inbound.RTPSession(), ccIn, media.DirectionOutput, 0)
		inbound.WireTransferBridgeRecording(callerRx, callerTx)
		agentRx := siprtp.NewSIPRTPTransport(outbound.RTPSession(), ccOut, media.DirectionInput, outbound.DTMFPayloadType())
		agentTx := siprtp.NewSIPRTPTransport(outbound.RTPSession(), ccOut, media.DirectionOutput, 0)
		pcm, err := bridge.NewTwoLegPCMBridge(callerRx, callerTx, agentRx, agentTx)
		if err != nil {
			return err
		}
		pcm.SetDirectionalPCMTap(func(dir bridge.BridgeDirection, pcm []byte) {
			switch dir {
			case bridge.DirectionCallerToAgent:
				inbound.WriteCallerPCM(pcm)
			case bridge.DirectionAgentToCaller:
				inbound.WriteAIPCM(pcm)
			}
		})
		pcm.Start()
		stop = pcm.Stop
		mode = "pcm"
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	ab := &activeBridge{inboundID: inID, outboundID: outID, stop: stop}
	m.bridges[inID] = ab
	m.bridges[outID] = ab
	logrus.WithFields(logrus.Fields{
		"inbound":  inID,
		"outbound": outID,
		"mode":     mode,
	}).Info("sipmedia transfer bridge started")
	return nil
}

// StopBridge tears down the bridge containing callID.
func (m *Manager) StopBridge(callID string) {
	callID = normCallID(callID)
	if m == nil || callID == "" {
		return
	}
	m.mu.Lock()
	ab := m.findLocked(callID)
	if ab == nil {
		m.mu.Unlock()
		return
	}
	delete(m.bridges, ab.inboundID)
	delete(m.bridges, ab.outboundID)
	m.mu.Unlock()
	if ab.stop != nil {
		ab.stop()
	}
	logrus.WithField("call_id", callID).Info("sipmedia transfer bridge stopped")
}

// MigrateOutboundCallID rekeys bridge when SBC rewrites outbound dialog Call-ID.
func (m *Manager) MigrateOutboundCallID(inbound, oldOut, newOut string) {
	inbound, oldOut, newOut = normCallID(inbound), normCallID(oldOut), normCallID(newOut)
	if m == nil || inbound == "" || oldOut == "" || newOut == "" || oldOut == newOut {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ab, ok := m.bridges[oldOut]
	if !ok || ab == nil || ab.inboundID != inbound {
		return
	}
	delete(m.bridges, oldOut)
	ab.outboundID = newOut
	m.bridges[newOut] = ab
	m.bridges[inbound] = ab
}

func (m *Manager) findLocked(callID string) *activeBridge {
	if ab := m.bridges[callID]; ab != nil {
		return ab
	}
	loc, ok := callLocalPart(callID)
	if !ok {
		return nil
	}
	for _, ab := range m.bridges {
		if ab == nil {
			continue
		}
		if lo, ok := callLocalPart(ab.outboundID); ok && lo == loc {
			return ab
		}
		if li, ok := callLocalPart(ab.inboundID); ok && li == loc {
			return ab
		}
	}
	return nil
}

func normCallID(s string) string { return strings.TrimSpace(s) }

func callLocalPart(cid string) (string, bool) {
	cid = normCallID(cid)
	i := strings.LastIndex(cid, "@")
	if i <= 0 || i >= len(cid)-1 {
		return "", false
	}
	return cid[:i], true
}

func rawRelayDecoderFor(c media.CodecConfig) func([]byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(c.Codec)) {
	case "pcmu":
		return encoder.DecodePCMU
	case "pcma":
		return encoder.DecodePCMA
	default:
		return nil
	}
}
