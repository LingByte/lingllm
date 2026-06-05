package asr

import (
	"context"
	"fmt"
)

// EchoFilterComponent filters out echo during TTS playback.
// When TTS is playing, it replaces audio with silence to prevent ASR
// from recognizing the AI's own voice.
type EchoFilterComponent struct {
	isTTSPlaying func() bool // Callback to check if TTS is playing
}

// NewEchoFilterComponent creates an echo filter component.
func NewEchoFilterComponent(isTTSPlaying func() bool) *EchoFilterComponent {
	return &EchoFilterComponent{
		isTTSPlaying: isTTSPlaying,
	}
}

// Name returns the component name.
func (e *EchoFilterComponent) Name() string {
	return "echo_filter"
}

// Process filters echo by replacing audio with silence during TTS playback.
func (e *EchoFilterComponent) Process(ctx context.Context, data interface{}) (interface{}, bool, error) {
	pcmData, ok := data.([]byte)
	if !ok {
		return nil, false, fmt.Errorf("%w: expected []byte, got %T", ErrInvalidDataType, data)
	}

	// If TTS is playing, replace with silence
	if e.isTTSPlaying != nil && e.isTTSPlaying() {
		silentFrame := make([]byte, len(pcmData))
		return silentFrame, true, nil
	}

	// Otherwise, pass through unchanged
	return pcmData, true, nil
}
