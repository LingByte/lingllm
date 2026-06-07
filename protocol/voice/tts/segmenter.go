package tts

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TextSegmenterConfig contains configuration for text segmentation.
type TextSegmenterConfig struct {
	// DelayTimeout: delay before sending a segment (default: 50ms)
	DelayTimeout time.Duration
	// MinChars: minimum characters before sending a segment (default: 10)
	MinChars int
	// MaxChars: maximum characters in a segment (default: 20)
	MaxChars int
}

// DefaultTextSegmenterConfig returns default text segmenter configuration.
// Optimized for low-latency streaming with responsive segment sizes:
// - MinChars: 10 (send quickly to reduce latency)
// - MaxChars: 20 (small segments for responsive playback)
// - DelayTimeout: 50ms (minimal delay before sending)
func DefaultTextSegmenterConfig() TextSegmenterConfig {
	return TextSegmenterConfig{
		DelayTimeout: 50 * time.Millisecond,
		MinChars:     10,
		MaxChars:     20,
	}
}

// TextSegmenterComponent segments text for streaming TTS synthesis.
// It intelligently breaks text at sentence boundaries and accumulates text
// based on character count and punctuation.
type TextSegmenterComponent struct {
	mu            sync.Mutex
	config        TextSegmenterConfig
	buffer        string
	lastUpdate    time.Time
	delayTimer    *time.Timer
	outputFunc    func(TextSegment) // Callback for output segments
	currentPlayID string
	logger        func(string)
}

// NewTextSegmenterComponent creates a new text segmenter component.
func NewTextSegmenterComponent(config TextSegmenterConfig, outputFunc func(TextSegment)) *TextSegmenterComponent {
	if config.DelayTimeout == 0 {
		config.DelayTimeout = 50 * time.Millisecond
	}
	if config.MinChars == 0 {
		config.MinChars = 10
	}
	if config.MaxChars == 0 {
		config.MaxChars = 20
	}

	return &TextSegmenterComponent{
		config:     config,
		outputFunc: outputFunc,
	}
}

// Name returns the component name.
func (s *TextSegmenterComponent) Name() string {
	return "text_segmenter"
}

// Process processes text for segmentation.
// Returns (remainingText, shouldContinue, error)
func (s *TextSegmenterComponent) Process(ctx context.Context, data interface{}) (interface{}, bool, error) {
	text, ok := data.(string)
	if !ok {
		return data, false, fmt.Errorf("expected string, got %T", data)
	}

	if text == "" {
		return text, true, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.buffer += text
	s.lastUpdate = time.Now()

	bufferLen := len([]rune(s.buffer))

	// 1. Check for sentence-ending punctuation: send immediately
	if strings.HasSuffix(s.buffer, "。") ||
		strings.HasSuffix(s.buffer, "！") ||
		strings.HasSuffix(s.buffer, "？") ||
		strings.HasSuffix(s.buffer, ".") ||
		strings.HasSuffix(s.buffer, "!") ||
		strings.HasSuffix(s.buffer, "?") {
		s.flush(true)
		return "", true, nil
	}

	// 2. Check for comma/pause punctuation: send if we have enough characters
	if strings.HasSuffix(s.buffer, "，") ||
		strings.HasSuffix(s.buffer, "、") ||
		strings.HasSuffix(s.buffer, ",") ||
		strings.HasSuffix(s.buffer, ";") {
		if bufferLen >= s.config.MinChars {
			s.flush(false)
			return "", true, nil
		}
	}

	// 3. Check if buffer exceeds max characters: send immediately
	if bufferLen >= s.config.MaxChars {
		s.flush(false)
		return "", true, nil
	}

	// 4. Set delay timer if buffer has minimum characters
	if bufferLen >= s.config.MinChars {
		if s.delayTimer != nil {
			s.delayTimer.Stop()
		}
		s.delayTimer = time.AfterFunc(s.config.DelayTimeout, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if len([]rune(s.buffer)) > 0 {
				s.flush(false)
			}
		})
	}

	return "", true, nil
}

// OnComplete marks the end of text input.
func (s *TextSegmenterComponent) OnComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.delayTimer != nil {
		s.delayTimer.Stop()
		s.delayTimer = nil
	}

	// Send remaining buffer
	if len(s.buffer) > 0 {
		s.flush(true)
	}
}

// flush sends the current buffer as a text segment.
func (s *TextSegmenterComponent) flush(isFinal bool) {
	if len(s.buffer) == 0 {
		return
	}

	segment := TextSegment{
		Text:      s.buffer,
		IsFinal:   isFinal,
		Timestamp: time.Now(),
		PlayID:    s.currentPlayID,
	}

	s.buffer = ""

	if s.delayTimer != nil {
		s.delayTimer.Stop()
		s.delayTimer = nil
	}

	if s.outputFunc != nil {
		s.outputFunc(segment)
	}

	if s.logger != nil {
		s.logger(fmt.Sprintf("[TextSegmenter] Flushed segment: %q (final=%v)", segment.Text, isFinal))
	}
}

// SetPlayID sets the current play ID for segments.
func (s *TextSegmenterComponent) SetPlayID(playID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentPlayID = playID
}

// SetLogger sets the logging callback.
func (s *TextSegmenterComponent) SetLogger(callback func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger = callback
}

// Reset resets the segmenter state.
func (s *TextSegmenterComponent) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.buffer = ""
	if s.delayTimer != nil {
		s.delayTimer.Stop()
		s.delayTimer = nil
	}
}
