// Package siprealtime wires pkg/realtime agents into SIP RTP call legs.
package siprealtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/protocol/sipmedia/session"
	"github.com/LingByte/lingllm/realtime"
	"github.com/sirupsen/logrus"
)

// Bridge connects caller RTP PCM ↔ realtime.Agent for one SIP leg.
type Bridge struct {
	callID string
	agent  realtime.Agent
	ms     *media.MediaSession
	inSR   int
	outSR  int
	pcmSR  int

	mu        sync.Mutex
	closed    bool
	replyBusy atomic.Bool
	gen       atomic.Uint32
	pacer     *outPacer
}

// Attach starts a realtime session on cs and registers media processors.
// Call before CallSession.StartOnACK / Start.
func Attach(cs *session.CallSession, cfg Config) (*Bridge, error) {
	if cs == nil {
		return nil, fmt.Errorf("nil call session")
	}
	if len(cfg.Credential) == 0 {
		return nil, fmt.Errorf("empty realtime credential")
	}
	ms := cs.MediaSession()
	if ms == nil {
		return nil, fmt.Errorf("call session has no media session")
	}

	b := &Bridge{
		callID: cs.CallID,
		ms:     ms,
		inSR:   cfg.InputSampleRate,
		outSR:  cfg.OutputSampleRate,
		pcmSR:  cs.PCMSampleRate(),
	}
	if b.inSR <= 0 {
		b.inSR = 16000
	}
	if b.outSR <= 0 {
		b.outSR = 24000
	}
	if b.pcmSR <= 0 {
		b.pcmSR = 8000
	}

	b.pacer = newOutPacer(b.pcmSR, b.emitPCM)

	agent, err := realtime.NewAgentFromCredential(cfg.Credential, realtime.Options{
		SystemPrompt:     cfg.SystemPrompt,
		Voice:            cfg.Voice,
		InputSampleRate:  b.inSR,
		OutputSampleRate: b.outSR,
		OnEvent:          b.onEvent,
	})
	if err != nil {
		return nil, err
	}
	b.agent = agent
	ctx := ms.GetContext()
	if err := agent.Start(ctx); err != nil {
		_ = agent.Close()
		return nil, err
	}

	if err := cs.AttachVoiceConversation(func() error {
		proc := media.NewPacketProcessor("sip-realtime-uplink", media.PriorityNormal,
			func(ctx context.Context, session *media.MediaSession, packet media.MediaPacket) error {
				ap, ok := packet.(*media.AudioPacket)
				if !ok || ap == nil || ap.IsSynthesized || len(ap.Payload) == 0 {
					return nil
				}
				b.feedUplink(ap.Payload)
				return nil
			})
		ms.RegisterProcessor(proc)
		return nil
	}); err != nil {
		_ = b.Close()
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"call_id": b.callID,
		"pcm_sr":  b.pcmSR,
		"in_sr":   b.inSR,
		"out_sr":  b.outSR,
	}).Info("sip realtime bridge attached")
	return b, nil
}

func (b *Bridge) feedUplink(pcm []byte) {
	if b == nil || len(pcm) == 0 {
		return
	}
	b.mu.Lock()
	if b.closed || b.agent == nil {
		b.mu.Unlock()
		return
	}
	agent := b.agent
	b.mu.Unlock()

	if b.replyBusy.Load() {
		return
	}
	if b.inSR != b.pcmSR {
		resampled, err := media.ResamplePCM(pcm, b.pcmSR, b.inSR)
		if err != nil {
			return
		}
		pcm = resampled
	}
	_ = agent.PushAudio(pcm)
}

func (b *Bridge) emitPCM(pcm []byte) {
	if b == nil || b.ms == nil || len(pcm) == 0 {
		return
	}
	b.ms.SendToOutput("sip-realtime", &media.AudioPacket{
		Payload:       pcm,
		IsSynthesized: true,
	})
}

func (b *Bridge) onEvent(ev realtime.Event) {
	if b == nil {
		return
	}
	switch ev.Type {
	case realtime.EventUserTranscript:
		if ev.Final && strings.TrimSpace(ev.Text) != "" {
			logrus.WithFields(logrus.Fields{"call_id": b.callID, "text": ev.Text}).Info("sip realtime user")
		}
	case realtime.EventUserSpeechStarted:
		b.replyBusy.Store(false)
		b.bargeIn()
	case realtime.EventAssistantText:
		if ev.Text != "" && !b.replyBusy.Load() {
			b.replyBusy.Store(true)
		}
		if ev.Final {
			b.replyBusy.Store(false)
		}
	case realtime.EventAssistantAudio:
		if len(ev.AudioPC) == 0 {
			return
		}
		b.replyBusy.Store(false)
		pcm := ev.AudioPC
		if b.outSR != b.pcmSR {
			resampled, err := media.ResamplePCM(pcm, b.outSR, b.pcmSR)
			if err != nil {
				return
			}
			pcm = resampled
		}
		b.pacer.push(pcm)
	case realtime.EventAssistantTurnEnd:
		b.replyBusy.Store(false)
	case realtime.EventError:
		logrus.WithFields(logrus.Fields{
			"call_id": b.callID,
			"err":     ev.Err,
			"fatal":   ev.Fatal,
		}).Warn("sip realtime error")
	case realtime.EventSessionClose:
		logrus.WithField("call_id", b.callID).Info("sip realtime session closed")
	}
}

func (b *Bridge) bargeIn() {
	b.gen.Add(1)
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if agent != nil {
		_ = agent.Cancel()
	}
	if b.pacer != nil {
		b.pacer.interrupt()
	}
}

// Close tears down the realtime agent and output pacer.
func (b *Bridge) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	agent := b.agent
	b.agent = nil
	b.mu.Unlock()

	if b.pacer != nil {
		b.pacer.close()
	}
	if agent != nil {
		return agent.Close()
	}
	return nil
}
