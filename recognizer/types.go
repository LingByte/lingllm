package recognizer

// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// ComputeSampleByteCount computes the number of bytes for audio samples
// based on sample rate, bit depth, and number of channels.
// Formula: (sampleRate * bitDepth * channels) / 8
func ComputeSampleByteCount(sampleRate, bitDepth, channels int) int {
	return (sampleRate * bitDepth * channels) / 8
}
