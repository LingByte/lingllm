package tts

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Speaker serializes TTS playback so adjacent utterances never overlap.
type Speaker struct {
	pipeline  *TTSPipeline
	queue     chan speakJob
	runCtx    context.Context
	runCancel context.CancelFunc
	wg        sync.WaitGroup
	closed    atomic.Bool

	onStarted func(utteranceID, text string, chained bool)
	onEnded   func(utteranceID string, ok bool, duration time.Duration, ttsFirstMs, e2eFirstMs int, moreQueued bool)
}

type speakJob struct {
	text              string
	utteranceID       string
	e2eAnchor         *time.Time
	chainFromPrevious bool
}

// SpeakerConfig configures a serial TTS speaker.
type SpeakerConfig struct {
	Pipeline  *TTSPipeline
	QueueSize int
	OnStarted func(utteranceID, text string, chained bool)
	OnEnded   func(utteranceID string, ok bool, duration time.Duration, ttsFirstMs, e2eFirstMs int, moreQueued bool)
}

// NewSpeaker creates a serial TTS speaker.
func NewSpeaker(cfg SpeakerConfig) (*Speaker, error) {
	if cfg.Pipeline == nil {
		return nil, errors.New("tts: nil pipeline")
	}
	size := cfg.QueueSize
	if size <= 0 {
		size = 64
	}
	return &Speaker{
		pipeline:  cfg.Pipeline,
		queue:     make(chan speakJob, size),
		onStarted: cfg.OnStarted,
		onEnded:   cfg.OnEnded,
	}, nil
}

// SetCallbacks updates lifecycle hooks (safe before Start).
func (s *Speaker) SetCallbacks(
	onStarted func(utteranceID, text string, chained bool),
	onEnded func(utteranceID string, ok bool, duration time.Duration, ttsFirstMs, e2eFirstMs int, moreQueued bool),
) {
	if s == nil {
		return
	}
	s.onStarted = onStarted
	s.onEnded = onEnded
}

// Start begins draining the speak queue.
func (s *Speaker) Start(ctx context.Context) {
	if s == nil || s.closed.Load() {
		return
	}
	s.runCtx, s.runCancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.worker()
}

// Stop drains and shuts down the speaker.
func (s *Speaker) Stop() {
	if s == nil || s.closed.Swap(true) {
		return
	}
	s.Interrupt()
	if s.runCancel != nil {
		s.runCancel()
	}
	close(s.queue)
	s.wg.Wait()
}

// Enqueue schedules text for synthesis. Non-blocking; drops when queue is full.
func (s *Speaker) Enqueue(text, utteranceID string, e2eAnchor *time.Time) bool {
	if s == nil || s.closed.Load() || text == "" {
		return false
	}
	chain := s.IsActive()
	job := speakJob{
		text:              text,
		utteranceID:       utteranceID,
		e2eAnchor:         e2eAnchor,
		chainFromPrevious: chain,
	}
	select {
	case s.queue <- job:
		return true
	default:
		return false
	}
}

// Interrupt stops the current utterance and drains pending jobs.
func (s *Speaker) Interrupt() {
	if s == nil {
		return
	}
	s.pipeline.Interrupt()
	s.drainQueue(true)
}

// DrainQueue drops pending jobs without interrupting the current utterance.
func (s *Speaker) DrainQueue() {
	if s != nil {
		s.drainQueue(true)
	}
}

func (s *Speaker) drainQueue(notify bool) {
	for {
		select {
		case job, ok := <-s.queue:
			if !ok {
				return
			}
			if notify && s.onEnded != nil {
				s.onEnded(job.utteranceID, false, 0, 0, 0, false)
			}
		default:
			return
		}
	}
}

// IsPlaying reports whether TTS audio is actively streaming.
func (s *Speaker) IsPlaying() bool {
	if s == nil {
		return false
	}
	return s.pipeline.IsPlaying()
}

// IsActive is true while streaming or while utterances remain queued.
func (s *Speaker) IsActive() bool {
	if s == nil {
		return false
	}
	return s.IsPlaying() || s.QueueDepth() > 0
}

// QueueDepth returns pending utterance count.
func (s *Speaker) QueueDepth() int {
	if s == nil {
		return 0
	}
	return len(s.queue)
}

func (s *Speaker) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.runCtx.Done():
			return
		case job, ok := <-s.queue:
			if !ok {
				return
			}
			if s.runCtx.Err() != nil {
				return
			}
			s.play(job)
		}
	}
}

func (s *Speaker) play(job speakJob) {
	start := time.Now()
	var firstByteAt atomic.Pointer[time.Time]

	s.pipeline.ArmFirstFrameHook(func() {
		ts := time.Now()
		firstByteAt.Store(&ts)
	})

	if s.onStarted != nil {
		s.onStarted(job.utteranceID, job.text, job.chainFromPrevious)
	}

	err := s.pipeline.Speak(job.text)
	s.pipeline.ArmFirstFrameHook(nil)

	dur := time.Since(start)
	ok := err == nil

	ttsFirstMs := 0
	e2eFirstMs := 0
	if fb := firstByteAt.Load(); fb != nil {
		ttsFirstMs = int(fb.Sub(start).Milliseconds())
		if job.e2eAnchor != nil {
			if d := fb.Sub(*job.e2eAnchor).Milliseconds(); d > 0 {
				e2eFirstMs = int(d)
			}
		}
	}

	moreQueued := len(s.queue) > 0
	if s.onEnded != nil {
		s.onEnded(job.utteranceID, ok, dur, ttsFirstMs, e2eFirstMs, moreQueued)
	}
}
