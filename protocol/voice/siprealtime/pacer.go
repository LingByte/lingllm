package siprealtime

import (
	"context"
	"sync"
	"time"
)

type outPacer struct {
	sampleRate int
	frameDur   time.Duration
	frameBytes int

	mu         sync.Mutex
	buf        []byte
	playCtx    context.Context
	playCancel context.CancelFunc
	playWG     sync.WaitGroup
	emit       func([]byte)
}

func newOutPacer(sampleRate int, emit func([]byte)) *outPacer {
	if sampleRate <= 0 {
		sampleRate = 8000
	}
	dur := 20 * time.Millisecond
	frameBytes := sampleRate * 2 * int(dur) / int(time.Second)
	if frameBytes < 2 {
		frameBytes = 320
	}
	return &outPacer{
		sampleRate: sampleRate,
		frameDur:   dur,
		frameBytes: frameBytes,
		emit:       emit,
	}
}

func (p *outPacer) push(pcm []byte) {
	if p == nil || len(pcm) == 0 || p.emit == nil {
		return
	}
	p.mu.Lock()
	p.buf = append(p.buf, pcm...)
	if p.playCancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		p.playCancel = cancel
		p.playCtx = ctx
		p.playWG.Add(1)
		go p.playLoop(ctx)
	}
	p.mu.Unlock()
}

func (p *outPacer) interrupt() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.buf = nil
	cancel := p.playCancel
	p.playCancel = nil
	p.playCtx = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.playWG.Wait()
}

func (p *outPacer) close() {
	p.interrupt()
}

func (p *outPacer) playLoop(ctx context.Context) {
	defer p.playWG.Done()
	defer func() {
		p.mu.Lock()
		p.playCancel = nil
		p.playCtx = nil
		p.mu.Unlock()
	}()

	next := time.Now()
	for {
		if ctx.Err() != nil {
			p.drain()
			return
		}
		p.mu.Lock()
		ready := len(p.buf) >= p.frameBytes
		p.mu.Unlock()
		if !ready {
			select {
			case <-ctx.Done():
				p.drain()
				return
			case <-time.After(2 * time.Millisecond):
				continue
			}
		}
		for {
			if ctx.Err() != nil {
				return
			}
			p.mu.Lock()
			if len(p.buf) < p.frameBytes {
				p.mu.Unlock()
				break
			}
			frame := make([]byte, p.frameBytes)
			copy(frame, p.buf[:p.frameBytes])
			p.buf = p.buf[p.frameBytes:]
			p.mu.Unlock()

			wait := time.Until(next)
			if wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			next = next.Add(p.frameDur)
			p.emit(frame)
		}
	}
}

func (p *outPacer) drain() {
	for {
		p.mu.Lock()
		if len(p.buf) == 0 {
			p.mu.Unlock()
			return
		}
		var frame []byte
		if len(p.buf) >= p.frameBytes {
			frame = make([]byte, p.frameBytes)
			copy(frame, p.buf[:p.frameBytes])
			p.buf = p.buf[p.frameBytes:]
		} else {
			frame = make([]byte, p.frameBytes)
			copy(frame, p.buf)
			p.buf = nil
		}
		p.mu.Unlock()
		p.emit(frame)
	}
}
