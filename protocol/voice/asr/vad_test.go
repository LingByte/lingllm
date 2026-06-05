package asr

import (
	"context"
	"encoding/binary"
	"testing"
)

func TestVADComponentCreation(t *testing.T) {
	config := DefaultVADConfig()
	vad := NewVADComponent(config)

	if vad == nil {
		t.Error("VADComponent should not be nil")
	}

	if vad.Name() != "vad" {
		t.Errorf("Name = %s, want 'vad'", vad.Name())
	}

	if !vad.enabled {
		t.Error("VAD should be enabled by default")
	}

	if vad.threshold != 1500.0 {
		t.Errorf("Threshold = %f, want 1500.0", vad.threshold)
	}

	if vad.consecutiveFramesNeeded != 1 {
		t.Errorf("ConsecutiveFramesNeeded = %d, want 1", vad.consecutiveFramesNeeded)
	}
}

func TestVADComponentCustomConfig(t *testing.T) {
	config := VADConfig{
		Enabled:                 false,
		Threshold:               2000.0,
		ConsecutiveFramesNeeded: 5,
		MaxNoiseSamples:         30,
	}
	vad := NewVADComponent(config)

	if vad.enabled {
		t.Error("VAD should be disabled with custom config")
	}

	if vad.threshold != 2000.0 {
		t.Errorf("Threshold = %f, want 2000.0", vad.threshold)
	}

	if vad.consecutiveFramesNeeded != 5 {
		t.Errorf("ConsecutiveFramesNeeded = %d, want 5", vad.consecutiveFramesNeeded)
	}

	if vad.maxNoiseSamples != 30 {
		t.Errorf("MaxNoiseSamples = %d, want 30", vad.maxNoiseSamples)
	}
}

func TestVADComponentProcessSilence(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	// Create silent PCM data (all zeros)
	silentData := make([]byte, 320)

	result, shouldContinue, err := vad.Process(context.Background(), silentData)

	if err != nil {
		t.Errorf("Process failed: %v", err)
	}

	if !shouldContinue {
		t.Error("shouldContinue should be true")
	}

	resultBytes, ok := result.([]byte)
	if !ok {
		t.Errorf("Result type = %T, want []byte", result)
	}

	if len(resultBytes) != len(silentData) {
		t.Errorf("Result length = %d, want %d", len(resultBytes), len(silentData))
	}
}

func TestVADComponentProcessSpeech(t *testing.T) {
	config := DefaultVADConfig()
	config.Threshold = 1000.0
	vad := NewVADComponent(config)

	// Create PCM data with high energy (simulating speech)
	speechData := make([]byte, 320)
	for i := 0; i < len(speechData)-1; i += 2 {
		// Write samples with high amplitude
		binary.LittleEndian.PutUint16(speechData[i:i+2], 5000)
	}

	result, shouldContinue, err := vad.Process(context.Background(), speechData)

	if err != nil {
		t.Errorf("Process failed: %v", err)
	}

	if !shouldContinue {
		t.Error("shouldContinue should be true")
	}

	resultBytes, ok := result.([]byte)
	if !ok {
		t.Errorf("Result type = %T, want []byte", result)
	}

	if len(resultBytes) != len(speechData) {
		t.Errorf("Result length = %d, want %d", len(resultBytes), len(speechData))
	}
}

func TestVADComponentSetEnabled(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	vad.SetEnabled(false)
	if vad.enabled {
		t.Error("VAD should be disabled after SetEnabled(false)")
	}

	vad.SetEnabled(true)
	if !vad.enabled {
		t.Error("VAD should be enabled after SetEnabled(true)")
	}
}

func TestVADComponentSetThreshold(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	vad.SetThreshold(2000.0)
	if vad.threshold != 2000.0 {
		t.Errorf("Threshold = %f, want 2000.0", vad.threshold)
	}
}

func TestVADComponentSetConsecutiveFrames(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	vad.SetConsecutiveFrames(5)
	if vad.consecutiveFramesNeeded != 5 {
		t.Errorf("ConsecutiveFramesNeeded = %d, want 5", vad.consecutiveFramesNeeded)
	}
}

func TestVADComponentSetTTSPlayingCallback(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	called := false
	vad.SetTTSPlayingCallback(func() bool {
		called = true
		return true
	})

	if vad.isTTSPlaying == nil {
		t.Error("TTS playing callback should be set")
	}

	// Call it to verify it works
	result := vad.isTTSPlaying()
	if !called {
		t.Error("TTS playing callback should have been called")
	}

	if !result {
		t.Error("TTS playing callback should return true")
	}
}

func TestVADComponentSetBargeInCallback(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	vad.SetBargeInCallback(func() {
		// Callback set
	})

	if vad.bargeInCallback == nil {
		t.Error("Barge-in callback should be set")
	}
}

func TestVADComponentSetLogger(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	logCount := 0
	vad.SetLogger(func(msg string) {
		logCount++
	})

	if vad.logger == nil {
		t.Error("Logger callback should be set")
	}
}

func TestVADComponentInvalidData(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	_, _, err := vad.Process(context.Background(), "invalid")
	if err == nil {
		t.Error("Process with invalid data should return error")
	}
}

func TestVADComponentEmptyData(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	emptyData := []byte{}
	result, shouldContinue, err := vad.Process(context.Background(), emptyData)

	if err != nil {
		t.Errorf("Process with empty data should not error: %v", err)
	}

	if !shouldContinue {
		t.Error("shouldContinue should be true")
	}

	if result == nil {
		t.Error("Result should not be nil")
	}
}

func TestVADComponentDisabled(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())
	vad.SetEnabled(false)

	speechData := make([]byte, 320)
	for i := 0; i < len(speechData)-1; i += 2 {
		binary.LittleEndian.PutUint16(speechData[i:i+2], 5000)
	}

	_, shouldContinue, err := vad.Process(context.Background(), speechData)

	if err != nil {
		t.Errorf("Process failed: %v", err)
	}

	if !shouldContinue {
		t.Error("shouldContinue should be true")
	}

	// Frame counter should be reset when disabled
	if vad.frameCounter != 0 {
		t.Errorf("Frame counter = %d, want 0 (disabled)", vad.frameCounter)
	}
}

func TestVADComponentBargeInDetection(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())
	vad.SetThreshold(1000.0)
	vad.SetConsecutiveFrames(1)

	bargeInDetected := false
	vad.SetBargeInCallback(func() {
		bargeInDetected = true
	})

	isTTSPlaying := true
	vad.SetTTSPlayingCallback(func() bool {
		return isTTSPlaying
	})

	// Create speech data
	speechData := make([]byte, 320)
	for i := 0; i < len(speechData)-1; i += 2 {
		binary.LittleEndian.PutUint16(speechData[i:i+2], 5000)
	}

	// Process the speech data
	_, _, err := vad.Process(context.Background(), speechData)
	if err != nil {
		t.Errorf("Process failed: %v", err)
	}

	if !bargeInDetected {
		t.Error("Barge-in should have been detected")
	}
}

func TestVADComponentBargeInNotDetectedWhenTTSNotPlaying(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())
	vad.SetThreshold(1000.0)
	vad.SetConsecutiveFrames(1)

	bargeInDetected := false
	vad.SetBargeInCallback(func() {
		bargeInDetected = true
	})

	isTTSPlaying := false
	vad.SetTTSPlayingCallback(func() bool {
		return isTTSPlaying
	})

	// Create speech data
	speechData := make([]byte, 320)
	for i := 0; i < len(speechData)-1; i += 2 {
		binary.LittleEndian.PutUint16(speechData[i:i+2], 5000)
	}

	// Process the speech data
	_, _, err := vad.Process(context.Background(), speechData)
	if err != nil {
		t.Errorf("Process failed: %v", err)
	}

	if bargeInDetected {
		t.Error("Barge-in should not be detected when TTS is not playing")
	}
}

func TestVADComponentCalculateRMS(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())

	// Test with silence
	silentData := make([]byte, 320)
	rms := vad.calculateRMS(silentData)
	if rms != 0 {
		t.Errorf("RMS of silence = %f, want 0", rms)
	}

	// Test with speech
	speechData := make([]byte, 320)
	for i := 0; i < len(speechData)-1; i += 2 {
		binary.LittleEndian.PutUint16(speechData[i:i+2], 5000)
	}
	rms = vad.calculateRMS(speechData)
	if rms == 0 {
		t.Error("RMS of speech should not be 0")
	}

	if rms < 4000 || rms > 6000 {
		t.Errorf("RMS of speech = %f, expected around 5000", rms)
	}
}

func TestVADComponentAdaptiveThreshold(t *testing.T) {
	vad := NewVADComponent(DefaultVADConfig())
	vad.SetThreshold(1500.0)

	// Process several frames of low-energy noise to build noise profile
	noiseData := make([]byte, 320)
	for i := 0; i < len(noiseData)-1; i += 2 {
		binary.LittleEndian.PutUint16(noiseData[i:i+2], 100) // Low energy
	}

	for j := 0; j < 10; j++ {
		vad.Process(context.Background(), noiseData)
	}

	// Check that adaptive threshold has been set
	if vad.adaptiveThreshold == 0 {
		t.Error("Adaptive threshold should be set after processing noise")
	}

	if vad.noiseLevel == 0 {
		t.Error("Noise level should be set after processing noise")
	}
}
