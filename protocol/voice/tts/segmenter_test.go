package tts

import (
	"context"
	"testing"
	"time"
)

func TestTextSegmenterComponentCreation(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {})

	if segmenter == nil {
		t.Error("TextSegmenterComponent should not be nil")
	}

	if segmenter.Name() != "text_segmenter" {
		t.Errorf("Name = %s, want 'text_segmenter'", segmenter.Name())
	}
}

func TestTextSegmenterComponentDefaultConfig(t *testing.T) {
	config := DefaultTextSegmenterConfig()

	if config.DelayTimeout != 50*time.Millisecond {
		t.Errorf("DelayTimeout = %v, want 50ms", config.DelayTimeout)
	}

	if config.MinChars != 15 {
		t.Errorf("MinChars = %d, want 15", config.MinChars)
	}

	if config.MaxChars != 35 {
		t.Errorf("MaxChars = %d, want 35", config.MaxChars)
	}
}

func TestTextSegmenterComponentEmptyText(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {})

	result, shouldContinue, err := segmenter.Process(context.Background(), "")

	if err != nil {
		t.Errorf("Process failed: %v", err)
	}

	if !shouldContinue {
		t.Error("shouldContinue should be true")
	}

	if result != "" {
		t.Errorf("Result = %q, want empty string", result)
	}
}

func TestTextSegmenterComponentSentenceEnding(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	segmentCount := 0
	lastSegment := ""

	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		segmentCount++
		lastSegment = segment.Text
	})

	// Process text with sentence ending
	segmenter.Process(context.Background(), "这是一个句子。")

	if segmentCount != 1 {
		t.Errorf("Segment count = %d, want 1", segmentCount)
	}

	if lastSegment != "这是一个句子。" {
		t.Errorf("Last segment = %q, want '这是一个句子。'", lastSegment)
	}
}

func TestTextSegmenterComponentMaxChars(t *testing.T) {
	config := TextSegmenterConfig{
		DelayTimeout: 50 * time.Millisecond,
		MinChars:     5,
		MaxChars:     10,
	}

	segmentCount := 0
	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		segmentCount++
	})

	// Process text that exceeds max chars
	segmenter.Process(context.Background(), "这是一个很长的句子需要分段")

	if segmentCount < 1 {
		t.Error("Should have created at least one segment")
	}
}

func TestTextSegmenterComponentOnComplete(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	segmentCount := 0

	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		segmentCount++
	})

	// Process text without ending punctuation
	segmenter.Process(context.Background(), "这是一个句子")

	// No segment should be created yet (waiting for more text or timeout)
	initialCount := segmentCount

	// Call OnComplete to flush remaining text
	segmenter.OnComplete()

	if segmentCount <= initialCount {
		t.Error("OnComplete should flush remaining text")
	}
}

func TestTextSegmenterComponentSetPlayID(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	var capturedPlayID string

	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		capturedPlayID = segment.PlayID
	})

	segmenter.SetPlayID("test-play-id")
	segmenter.Process(context.Background(), "测试。")

	if capturedPlayID != "test-play-id" {
		t.Errorf("PlayID = %s, want 'test-play-id'", capturedPlayID)
	}
}

func TestTextSegmenterComponentSetLogger(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	logCalled := false

	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {})

	segmenter.SetLogger(func(msg string) {
		logCalled = true
	})

	// Trigger a flush to generate a log message
	segmenter.Process(context.Background(), "测试。")

	if !logCalled {
		t.Error("Logger should have been called")
	}
}

func TestTextSegmenterComponentReset(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {})

	// Add some text
	segmenter.Process(context.Background(), "测试文本")

	// Reset
	segmenter.Reset()

	// Buffer should be empty
	if segmenter.buffer != "" {
		t.Errorf("Buffer should be empty after reset, got %q", segmenter.buffer)
	}
}

func TestTextSegmenterComponentCommaSegmentation(t *testing.T) {
	config := TextSegmenterConfig{
		DelayTimeout: 50 * time.Millisecond,
		MinChars:     5,
		MaxChars:     50,
	}

	segmentCount := 0
	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		segmentCount++
	})

	// Process text with comma
	segmenter.Process(context.Background(), "这是第一部分，")

	if segmentCount < 1 {
		t.Error("Should have created a segment at comma")
	}
}

func TestTextSegmenterComponentEnglishPunctuation(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	segmentCount := 0
	lastSegment := ""

	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		segmentCount++
		lastSegment = segment.Text
	})

	// Process English text with period
	segmenter.Process(context.Background(), "This is a sentence.")

	if segmentCount != 1 {
		t.Errorf("Segment count = %d, want 1", segmentCount)
	}

	if lastSegment != "This is a sentence." {
		t.Errorf("Last segment = %q, want 'This is a sentence.'", lastSegment)
	}
}

func TestTextSegmenterComponentMultipleSegments(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	segments := []string{}

	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		segments = append(segments, segment.Text)
	})

	// Process multiple sentences
	segmenter.Process(context.Background(), "第一句。")
	segmenter.Process(context.Background(), "第二句。")
	segmenter.Process(context.Background(), "第三句。")

	if len(segments) != 3 {
		t.Errorf("Segment count = %d, want 3", len(segments))
	}
}

func TestTextSegmenterComponentFinalSegment(t *testing.T) {
	config := DefaultTextSegmenterConfig()
	var finalSegment TextSegment

	segmenter := NewTextSegmenterComponent(config, func(segment TextSegment) {
		finalSegment = segment
	})

	// Process text and complete
	segmenter.Process(context.Background(), "这是最后一句")
	segmenter.OnComplete()

	if !finalSegment.IsFinal {
		t.Error("Final segment should have IsFinal=true")
	}

	if finalSegment.Text != "这是最后一句" {
		t.Errorf("Final segment text = %q, want '这是最后一句'", finalSegment.Text)
	}
}
