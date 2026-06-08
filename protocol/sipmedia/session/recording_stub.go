package session

import (
	"context"

	"github.com/LingByte/lingllm/protocol/sip/hooks"
)

// RecordingInfo is a durable handle returned after FlushRecorder (URL/object key from hooks.RecordingSink).
type RecordingInfo struct {
	CallID    string
	URL       string
	ObjectKey string
}

// RecorderConfig configures optional stereo PCM taps via hooks.RecordingSink.
// SN3 RTP capture in CallSession is always available via TakeRecording regardless of this config.
type RecorderConfig struct {
	Meta       hooks.CallMeta
	SampleRate int
	Codec      string
	Sink       hooks.RecordingSink // nil → hooks.DefaultRegistry.Recording
}

func (cs *CallSession) activeRecordingSink() hooks.RecordingSink {
	if cs == nil {
		return hooks.NopRecording{}
	}
	cs.recMu.Lock()
	s := cs.recordingSink
	cs.recMu.Unlock()
	if s != nil {
		return s
	}
	return hooks.DefaultRegistry.Recording
}

// EnableRecorder arms stereo PCM taps when the sink accepts BeginRecording.
func (cs *CallSession) EnableRecorder(cfg RecorderConfig) bool {
	if cs == nil {
		return false
	}
	sink := cfg.Sink
	if sink == nil {
		sink = hooks.DefaultRegistry.Recording
	}
	meta := cfg.Meta
	if meta.CallID == "" {
		meta.CallID = cs.CallID
	}
	if meta.Direction == "" {
		meta.Direction = hooks.DirectionInbound
	}
	ok := sink.BeginRecording(meta, cfg.SampleRate, cfg.Codec)
	cs.recMu.Lock()
	cs.recorderEnabled = ok
	cs.recordingSink = sink
	cs.recMu.Unlock()
	return ok
}

// HasRecorder reports whether stereo PCM taps are armed.
func (cs *CallSession) HasRecorder() bool {
	if cs == nil {
		return false
	}
	cs.recMu.Lock()
	defer cs.recMu.Unlock()
	return cs.recorderEnabled
}

// WriteCallerPCM forwards decoded caller PCM to the recording sink when armed.
func (cs *CallSession) WriteCallerPCM(pcm []byte) {
	if cs == nil || len(pcm) == 0 || !cs.HasRecorder() {
		return
	}
	cs.activeRecordingSink().AppendPCM(cs.CallID, hooks.ChannelCaller, pcm)
}

// WriteAIPCM forwards decoded agent/AI PCM to the recording sink when armed.
func (cs *CallSession) WriteAIPCM(pcm []byte) {
	if cs == nil || len(pcm) == 0 || !cs.HasRecorder() {
		return
	}
	cs.activeRecordingSink().AppendPCM(cs.CallID, hooks.ChannelAgent, pcm)
}

// FlushRecorder delivers the SN3 blob to RecordingSink.FinishRecording.
func (cs *CallSession) FlushRecorder(ctx context.Context) (RecordingInfo, bool) {
	if cs == nil {
		return RecordingInfo{}, false
	}
	sn3 := cs.TakeRecording()
	artifact := hooks.RecordingArtifact{
		CallID:     cs.CallID,
		Format:     hooks.FormatSN3,
		Payload:    sn3,
		Codec:      cs.SourceCodec().Codec,
		SampleRate: cs.PCMSampleRate(),
		Channels:   1,
	}
	res, err := cs.activeRecordingSink().FinishRecording(ctx, artifact)
	if err != nil {
		return RecordingInfo{CallID: cs.CallID}, len(sn3) > 0
	}
	if res.URL == "" && res.ObjectKey == "" && len(sn3) == 0 {
		return RecordingInfo{CallID: cs.CallID}, false
	}
	return RecordingInfo{CallID: cs.CallID, URL: res.URL, ObjectKey: res.ObjectKey}, true
}
