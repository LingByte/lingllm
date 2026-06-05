package tts

import (
	"context"
	"sync"
	"time"
)

// TTSService defines the interface for text-to-speech synthesis.
type TTSService interface {
	// Synthesize synthesizes text to audio and calls the callback for each audio chunk.
	// The callback receives PCM audio data (typically 16-bit mono at 16kHz).
	Synthesize(ctx context.Context, text string, callback func([]byte) error) error
}

// TTSPipelineComponent defines the interface for TTS pipeline components.
type TTSPipelineComponent interface {
	// Name returns the component name.
	Name() string

	// Process processes data through the component.
	// For text processors: input is string, output is string
	// For audio processors: input is []byte, output is []byte
	// Returns (processedData, shouldContinue, error)
	Process(ctx context.Context, data interface{}) (interface{}, bool, error)
}

// TextSegment represents a text segment for TTS synthesis.
type TextSegment struct {
	Text      string
	IsFinal   bool
	Timestamp time.Time
	PlayID    string
}

// AudioFrame represents a frame of audio data.
type AudioFrame struct {
	Data       []byte
	SampleRate int
	Channels   int
	PlayID     string
	Sequence   uint32
}

// TTSPipelineConfig contains configuration for the TTS pipeline.
type TTSPipelineConfig struct {
	// TTSService: the TTS service to use for synthesis
	TTSService TTSService
	// OutputCodec: output audio codec (e.g., "opus", "pcm")
	OutputCodec string
	// TargetSampleRate: target sample rate for output audio (default: 16000)
	TargetSampleRate int
	// FrameDuration: frame duration for audio processing (default: 60ms)
	FrameDuration time.Duration
	// TextProcessors: optional text processing components (e.g., text normalization)
	TextProcessors []TTSPipelineComponent
	// AudioProcessors: optional audio processing components (e.g., encoding)
	AudioProcessors []TTSPipelineComponent
	// SendCallback: callback for sending synthesized audio
	SendCallback func(data []byte) error
	// RecordCallback: optional callback for recording synthesized audio
	RecordCallback func(data []byte) error
	// Logger: optional logging callback
	Logger func(string)
}

// DefaultTTSPipelineConfig returns default TTS pipeline configuration.
func DefaultTTSPipelineConfig(ttsService TTSService) TTSPipelineConfig {
	return TTSPipelineConfig{
		TTSService:       ttsService,
		OutputCodec:      "pcm",
		TargetSampleRate: 16000,
		FrameDuration:    60 * time.Millisecond,
		TextProcessors:   []TTSPipelineComponent{},
		AudioProcessors:  []TTSPipelineComponent{},
	}
}

// TTSPipeline manages text-to-speech synthesis with pluggable components.
type TTSPipeline struct {
	mu                sync.RWMutex
	config            TTSPipelineConfig
	ttsService        TTSService
	textProcessors    []TTSPipelineComponent
	audioProcessors   []TTSPipelineComponent
	currentPlayID     string
	globalSeq         uint32
	seqMu             sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	logger            func(string)
	onCompleteFunc    func()
	completeMu        sync.Mutex
	completionCancel  context.CancelFunc
	completionCancelMu sync.Mutex
}

// NewTTSPipeline creates a new TTS pipeline with the given configuration.
func NewTTSPipeline(config TTSPipelineConfig) (*TTSPipeline, error) {
	if config.TTSService == nil {
		return nil, ErrTTSServiceRequired
	}

	if config.SendCallback == nil {
		return nil, ErrSendCallbackRequired
	}

	if config.TargetSampleRate == 0 {
		config.TargetSampleRate = 16000
	}

	if config.FrameDuration == 0 {
		config.FrameDuration = 60 * time.Millisecond
	}

	if config.OutputCodec == "" {
		config.OutputCodec = "pcm"
	}

	return &TTSPipeline{
		config:         config,
		ttsService:     config.TTSService,
		textProcessors: config.TextProcessors,
		audioProcessors: config.AudioProcessors,
		logger:         config.Logger,
	}, nil
}

// Start starts the TTS pipeline.
func (p *TTSPipeline) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ctx, p.cancel = context.WithCancel(ctx)

	if p.logger != nil {
		p.logger("[TTS Pipeline] Started")
	}

	return nil
}

// Stop stops the TTS pipeline.
func (p *TTSPipeline) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}

	p.completionCancelMu.Lock()
	if p.completionCancel != nil {
		p.completionCancel()
	}
	p.completionCancelMu.Unlock()

	if p.logger != nil {
		p.logger("[TTS Pipeline] Stopped")
	}

	return nil
}

// Synthesize synthesizes text to audio.
func (p *TTSPipeline) Synthesize(ctx context.Context, text string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.ctx == nil {
		return ErrPipelineNotStarted
	}

	// Process text through text processors
	processedText := text
	for _, processor := range p.textProcessors {
		result, shouldContinue, err := processor.Process(ctx, processedText)
		if err != nil {
			if p.logger != nil {
				p.logger("[TTS Pipeline] Text processing error: " + err.Error())
			}
			return err
		}

		if !shouldContinue {
			return nil
		}

		if resultStr, ok := result.(string); ok {
			processedText = resultStr
		}
	}

	if p.logger != nil {
		p.logger("[TTS Pipeline] Synthesizing: " + processedText)
	}

	// Synthesize the processed text
	const frameSizeBytes = 1920 // 60ms @ 16kHz, 16bit, mono
	estimatedSize := len(processedText) * 100
	if estimatedSize < frameSizeBytes*2 {
		estimatedSize = frameSizeBytes * 2
	}
	buffer := make([]byte, 0, estimatedSize)

	err := p.ttsService.Synthesize(p.ctx, processedText, func(pcmData []byte) error {
		buffer = append(buffer, pcmData...)

		for len(buffer) >= frameSizeBytes {
			frameData := make([]byte, frameSizeBytes)
			copy(frameData, buffer[:frameSizeBytes])
			buffer = buffer[frameSizeBytes:]

			// Process audio through audio processors
			audioData := interface{}(frameData)
			for _, processor := range p.audioProcessors {
				result, shouldContinue, err := processor.Process(ctx, audioData)
				if err != nil {
					if p.logger != nil {
						p.logger("[TTS Pipeline] Audio processing error: " + err.Error())
					}
					return err
				}

				if !shouldContinue {
					return nil
				}

				audioData = result
			}

			// Convert back to bytes for sending
			var audioBytes []byte
			if resultBytes, ok := audioData.([]byte); ok {
				audioBytes = resultBytes
			} else {
				audioBytes = frameData
			}

			// Record audio if callback is set
			if p.config.RecordCallback != nil {
				if err := p.config.RecordCallback(audioBytes); err != nil {
					if p.logger != nil {
						p.logger("[TTS Pipeline] Recording error: " + err.Error())
					}
				}
			}

			// Send audio
			if err := p.config.SendCallback(audioBytes); err != nil {
				if p.logger != nil {
					p.logger("[TTS Pipeline] Send error: " + err.Error())
				}
				return err
			}
		}

		return nil
	})

	if err != nil {
		if p.logger != nil {
			p.logger("[TTS Pipeline] Synthesis error: " + err.Error())
		}
		return err
	}

	// Handle remaining data
	if len(buffer) > 0 {
		// Process audio through audio processors
		audioData := interface{}(buffer)
		for _, processor := range p.audioProcessors {
			result, shouldContinue, err := processor.Process(ctx, audioData)
			if err != nil {
				if p.logger != nil {
					p.logger("[TTS Pipeline] Audio processing error: " + err.Error())
				}
				return err
			}

			if !shouldContinue {
				return nil
			}

			audioData = result
		}

		// Convert back to bytes for sending
		var audioBytes []byte
		if resultBytes, ok := audioData.([]byte); ok {
			audioBytes = resultBytes
		} else {
			audioBytes = buffer
		}

		// Record audio if callback is set
		if p.config.RecordCallback != nil {
			if err := p.config.RecordCallback(audioBytes); err != nil {
				if p.logger != nil {
					p.logger("[TTS Pipeline] Recording error: " + err.Error())
				}
			}
		}

		// Send audio
		if err := p.config.SendCallback(audioBytes); err != nil {
			if p.logger != nil {
				p.logger("[TTS Pipeline] Send error: " + err.Error())
			}
			return err
		}
	}

	return nil
}

// SetOnCompleteFunc sets the completion callback.
func (p *TTSPipeline) SetOnCompleteFunc(callback func()) {
	p.completeMu.Lock()
	defer p.completeMu.Unlock()
	p.onCompleteFunc = callback
}

// SetLogger sets the logging callback.
func (p *TTSPipeline) SetLogger(callback func(string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logger = callback
}

// GetConfig returns the current pipeline configuration.
func (p *TTSPipeline) GetConfig() TTSPipelineConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// Close closes the TTS pipeline.
func (p *TTSPipeline) Close() error {
	return p.Stop()
}
