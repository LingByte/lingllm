package asr

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// VADComponent is a voice activity detection component for the ASR pipeline.
// It detects speech based on energy (RMS) analysis and supports:
// - Adaptive noise threshold tracking
// - Consecutive frame counting for barge-in detection
// - Barge-in callback when speech is detected during TTS playback
type VADComponent struct {
	mu                      sync.RWMutex
	enabled                 bool
	threshold               float64
	adaptiveThreshold       float64
	consecutiveFramesNeeded int
	frameCounter            int
	lastLogTime             time.Time
	noiseLevel              float64
	noiseSamples            []float64
	maxNoiseSamples         int

	// Callbacks
	isTTSPlaying   func() bool // Check if TTS is playing
	bargeInCallback func()      // Called when barge-in is detected
	logger         func(string) // Logging callback
}

// VADConfig contains configuration for VAD component.
type VADConfig struct {
	// Enabled: whether VAD is enabled (default: true)
	Enabled bool
	// Threshold: RMS energy threshold for speech detection (default: 1500.0)
	Threshold float64
	// ConsecutiveFramesNeeded: how many consecutive frames above threshold trigger barge-in (default: 1)
	ConsecutiveFramesNeeded int
	// MaxNoiseSamples: maximum number of noise samples to track (default: 20)
	MaxNoiseSamples int
}

// DefaultVADConfig returns the default VAD configuration.
func DefaultVADConfig() VADConfig {
	return VADConfig{
		Enabled:                 true,
		Threshold:               1500.0,
		ConsecutiveFramesNeeded: 1,
		MaxNoiseSamples:         20,
	}
}

// NewVADComponent creates a new VAD component with the given configuration.
func NewVADComponent(config VADConfig) *VADComponent {
	if config.Threshold == 0 {
		config.Threshold = 1500.0
	}
	if config.ConsecutiveFramesNeeded == 0 {
		config.ConsecutiveFramesNeeded = 1
	}
	if config.MaxNoiseSamples == 0 {
		config.MaxNoiseSamples = 20
	}

	return &VADComponent{
		enabled:                 config.Enabled,
		threshold:               config.Threshold,
		adaptiveThreshold:       0,
		consecutiveFramesNeeded: config.ConsecutiveFramesNeeded,
		frameCounter:            0,
		lastLogTime:             time.Now(),
		noiseLevel:              0,
		noiseSamples:            make([]float64, 0),
		maxNoiseSamples:         config.MaxNoiseSamples,
	}
}

// Name returns the component name.
func (v *VADComponent) Name() string {
	return "vad"
}

// Process processes one audio frame for voice activity detection.
// Returns (pcmData, shouldContinue, error)
func (v *VADComponent) Process(ctx context.Context, data interface{}) (interface{}, bool, error) {
	pcmData, ok := data.([]byte)
	if !ok {
		return nil, false, fmt.Errorf("%w: expected []byte, got %T", ErrInvalidDataType, data)
	}

	if len(pcmData) < 2 {
		return pcmData, true, nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.enabled {
		return pcmData, true, nil
	}

	// Check if TTS is playing
	isTTSPlaying := false
	if v.isTTSPlaying != nil {
		isTTSPlaying = v.isTTSPlaying()
	}

	// Calculate RMS energy
	rms := v.calculateRMS(pcmData)

	// Update adaptive threshold based on noise level
	if rms < 350 {
		v.noiseSamples = append(v.noiseSamples, rms)
		if len(v.noiseSamples) > v.maxNoiseSamples {
			v.noiseSamples = v.noiseSamples[1:]
		}

		var sum float64
		for _, sample := range v.noiseSamples {
			sum += sample
		}

		if len(v.noiseSamples) > 0 {
			v.noiseLevel = sum / float64(len(v.noiseSamples))
			v.adaptiveThreshold = v.noiseLevel * 4.0
			if v.adaptiveThreshold < 180 {
				v.adaptiveThreshold = 180
			}
			if v.adaptiveThreshold > v.threshold {
				v.adaptiveThreshold = v.threshold
			}
		}
	}

	// Determine effective threshold
	effectiveThreshold := v.threshold
	if v.adaptiveThreshold > 0 {
		minAdaptiveFloor := v.threshold * 0.65
		if minAdaptiveFloor < 300 {
			minAdaptiveFloor = 300
		}
		effectiveThreshold = v.adaptiveThreshold
		if effectiveThreshold < minAdaptiveFloor {
			effectiveThreshold = minAdaptiveFloor
		}
	}

	// Check for speech
	if rms > effectiveThreshold {
		v.frameCounter++

		// Log if needed
		if v.logger != nil && time.Since(v.lastLogTime) >= time.Second {
			v.lastLogTime = time.Now()
			v.logger(fmt.Sprintf("[VAD] energy above threshold: rms=%.0f, threshold=%.0f, frames=%d/%d",
				rms, effectiveThreshold, v.frameCounter, v.consecutiveFramesNeeded))
		}

		// Check if we have enough consecutive frames
		if v.frameCounter >= v.consecutiveFramesNeeded {
			if v.logger != nil {
				v.logger(fmt.Sprintf("[VAD] barge-in detected: rms=%.0f, threshold=%.0f", rms, effectiveThreshold))
			}

			// Call barge-in callback if TTS is playing
			if isTTSPlaying && v.bargeInCallback != nil {
				v.bargeInCallback()
			}

			v.frameCounter = 0
		}
	} else {
		// Energy below threshold - reset frame counter
		if v.frameCounter > 0 && v.logger != nil && time.Since(v.lastLogTime) >= time.Second {
			v.lastLogTime = time.Now()
			v.logger(fmt.Sprintf("[VAD] energy below threshold: rms=%.0f, reset frames=%d", rms, v.frameCounter))
		}
		v.frameCounter = 0
	}

	return pcmData, true, nil
}

// SetEnabled enables or disables VAD detection.
func (v *VADComponent) SetEnabled(enabled bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.enabled = enabled
	if !enabled {
		v.frameCounter = 0
	}
}

// SetThreshold sets the RMS energy threshold for speech detection.
func (v *VADComponent) SetThreshold(threshold float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.threshold = threshold
}

// SetConsecutiveFrames sets how many consecutive frames above threshold trigger barge-in.
func (v *VADComponent) SetConsecutiveFrames(frames int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.consecutiveFramesNeeded = frames
}

// SetTTSPlayingCallback sets the callback to check if TTS is playing.
func (v *VADComponent) SetTTSPlayingCallback(callback func() bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.isTTSPlaying = callback
}

// SetBargeInCallback sets the callback for barge-in detection.
func (v *VADComponent) SetBargeInCallback(callback func()) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.bargeInCallback = callback
}

// SetLogger sets the logging callback.
func (v *VADComponent) SetLogger(callback func(string)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.logger = callback
}

// calculateRMS calculates the RMS (Root Mean Square) energy of PCM16 audio data.
func (v *VADComponent) calculateRMS(pcmData []byte) float64 {
	if len(pcmData) < 2 {
		return 0
	}

	var sumSquares float64
	sampleCount := len(pcmData) / 2
	if sampleCount == 0 {
		return 0
	}

	// Process 16-bit little-endian PCM samples
	for i := 0; i < len(pcmData)-1; i += 2 {
		// Combine two bytes into a 16-bit signed integer (little-endian)
		sample := int16(pcmData[i]) | int16(pcmData[i+1])<<8
		absSample := math.Abs(float64(sample))
		sumSquares += absSample * absSample
	}

	return math.Sqrt(sumSquares / float64(sampleCount))
}
