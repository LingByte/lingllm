// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Package vad provides unified voice activity detection (VAD) interface supporting
// multiple providers (HTTP, WebSocket) with session management and health checks.
//
// Supported Providers:
//   - HTTP: Traditional HTTP-based VAD service
//   - WebSocket: Real-time WebSocket-based VAD service
//
// Usage:
//
//	// Create factory
//	factory := vad.NewDefaultFactory(logger)
//
//	// Create detector and session manager
//	config := &vad.Config{
//		Provider: vad.ProviderHTTP,
//		BaseURL:  "http://localhost:8080",
//		Timeout:  10 * time.Second,
//		SessionTTL: 5 * time.Minute,
//	}
//
//	detector, manager, err := factory.CreateDetectorAndManager(config)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer detector.Close()
//	defer manager.Close()
//
//	// Process audio
//	result, err := manager.ProcessAudio(ctx, sessionID, audioData, "pcm")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	if result.HaveVoice {
//		// Handle voice activity
//	}
package vad
