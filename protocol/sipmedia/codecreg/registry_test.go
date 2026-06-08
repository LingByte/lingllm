// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package codecreg_test

import (
	"testing"

	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sipmedia/codecreg"
)

func TestNegotiateOffer_PrefersPCMA(t *testing.T) {
	offer := []sdp.Codec{
		{PayloadType: 111, Name: "opus", ClockRate: 48000, Channels: 2},
		{PayloadType: 0, Name: "PCMU", ClockRate: 8000, Channels: 1},
		{PayloadType: 8, Name: "PCMA", ClockRate: 8000, Channels: 1},
	}
	src, neg, err := codecreg.NegotiateOffer(offer)
	if err != nil {
		t.Fatalf("negotiate failed: %v", err)
	}
	if neg.Name != "pcma" {
		t.Fatalf("expected pcma to win, got %s", neg.Name)
	}
	if src.SampleRate != 8000 {
		t.Fatalf("expected 8k samplerate, got %d", src.SampleRate)
	}
}

func TestNegotiateOffer_FallsBackToOpus(t *testing.T) {
	offer := []sdp.Codec{{PayloadType: 111, Name: "opus", ClockRate: 48000, Channels: 2}}
	_, neg, err := codecreg.NegotiateOffer(offer)
	if err != nil || neg.Name != "opus" {
		t.Fatalf("opus-only offer must negotiate opus: neg=%+v err=%v", neg, err)
	}
}

func TestNegotiateOffer_RejectsUnknown(t *testing.T) {
	offer := []sdp.Codec{{PayloadType: 96, Name: "amr-wb", ClockRate: 16000, Channels: 1}}
	_, _, err := codecreg.NegotiateOffer(offer)
	if err == nil {
		t.Fatalf("unknown codec should error")
	}
}

func TestInternalPCMSampleRate_ByCodec(t *testing.T) {
	cases := []struct {
		src  media.CodecConfig
		want int
	}{
		{media.CodecConfig{Codec: "pcma", SampleRate: 8000}, 8000},
		{media.CodecConfig{Codec: "g722", SampleRate: 8000}, 16000},
		{media.CodecConfig{Codec: "opus", SampleRate: 48000}, 48000},
		{media.CodecConfig{Codec: "opus", SampleRate: 22000}, 24000},
		{media.CodecConfig{Codec: "unknown", SampleRate: 24000}, 24000},
		{media.CodecConfig{Codec: "unknown"}, 16000},
	}
	for _, tc := range cases {
		if got := codecreg.InternalPCMSampleRate(tc.src); got != tc.want {
			t.Errorf("%+v: want %d, got %d", tc.src, tc.want, got)
		}
	}
}

func TestRegister_OverridesAndExternalCodecPath(t *testing.T) {
	// 模拟"外部"注册：业务在 init 之外加一个 codec，本测试验证流程。
	codecreg.Register(codecreg.Descriptor{
		Name:       "ilbc",
		Preference: 50, // 排在内置 codec 之后
		BuildNegotiated: func(o sdp.Codec) (media.CodecConfig, sdp.Codec) {
			return media.CodecConfig{
					Codec: "ilbc", SampleRate: 8000, Channels: 1,
					BitDepth: 16, PayloadType: o.PayloadType, FrameDuration: "30ms",
				}, sdp.Codec{
					PayloadType: o.PayloadType, Name: "ilbc", ClockRate: 8000, Channels: 1,
				}
		},
		InternalPCMRate: func(media.CodecConfig) int { return 8000 },
	})
	if !Has("ilbc") {
		t.Fatalf("registered codec should be visible")
	}
	// 单独 offer ilbc 时它应能选中。
	_, neg, err := codecreg.NegotiateOffer([]sdp.Codec{
		{PayloadType: 97, Name: "iLBC", ClockRate: 8000, Channels: 1},
	})
	if err != nil || neg.Name != "ilbc" {
		t.Fatalf("ilbc-only offer must select ilbc, got %+v err=%v", neg, err)
	}
}

// Has 是 Lookup 的简化形态。
func Has(name string) bool {
	_, ok := codecreg.Lookup(name)
	return ok
}
