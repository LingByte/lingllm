package hooks

import "context"

// RecordingFormat identifies in-memory capture encodings produced by the SIP stack.
type RecordingFormat string

const (
	// FormatSN3 is the session-layer RTP blob (magic "SN3", dir/seq/ts/wallNs/payload frames).
	FormatSN3 RecordingFormat = "sn3"
	// FormatStereoPCM is optional live stereo PCM from WriteCallerPCM / WriteAIPCM taps.
	FormatStereoPCM RecordingFormat = "stereo_pcm"
)

// RecordingChannel separates caller vs agent/AI halves in stereo PCM taps.
type RecordingChannel int

const (
	ChannelCaller RecordingChannel = iota
	ChannelAgent
)

// RecordingArtifact is what the SIP stack can hand off at hangup without knowing storage backend.
type RecordingArtifact struct {
	CallID      string
	Format      RecordingFormat
	Payload     []byte // SN3 blob or app-defined PCM container
	Codec       string
	SampleRate  int
	Channels    int // 1 mono taps, 2 when stereo PCM sink merged
}

// RecordingSink receives live PCM taps and/or the final SN3 blob at call end.
// Implementations may write to object storage, enqueue transcode, or persist CDR URLs.
//
// The SIP stack calls these hooks; it does not encode WAV, upload files, or touch a database.
type RecordingSink interface {
	// BeginRecording returns false to disable taps for this call (SN3 capture may still run in session).
	BeginRecording(meta CallMeta, sampleRate int, codec string) bool

	// AppendPCM delivers mono PCM16 LE frames during the call (transfer bridge / voice pipeline).
	// Implementations must copy pcm if retained beyond the call.
	AppendPCM(callID string, ch RecordingChannel, pcm []byte)

	// FinishRecording is invoked once at hangup with the SN3 artifact (may be nil/empty).
	// Return a durable reference (URL, object key) for LifecycleSink.OnBye if needed.
	FinishRecording(ctx context.Context, artifact RecordingArtifact) (RecordingResult, error)
}

// RecordingResult is an opaque durable handle returned by RecordingSink.
type RecordingResult struct {
	URL      string
	ObjectKey string
	Bytes    int64
}

// NopRecording ignores all recording hooks.
type NopRecording struct{}

func (NopRecording) BeginRecording(CallMeta, int, string) bool { return false }
func (NopRecording) AppendPCM(string, RecordingChannel, []byte) {}
func (NopRecording) FinishRecording(context.Context, RecordingArtifact) (RecordingResult, error) {
	return RecordingResult{}, nil
}

// Registry holds optional sinks wired once at process startup.
type Registry struct {
	Lifecycle LifecycleSink
	Recording RecordingSink
}

// DefaultRegistry uses nop sinks until the application replaces them.
var DefaultRegistry = Registry{
	Lifecycle: NopLifecycle{},
	Recording: NopRecording{},
}
