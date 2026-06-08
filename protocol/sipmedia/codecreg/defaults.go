// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package codecreg

// defaults.go ——内置 codec 描述符。
//
// 这里把原来 pkg/sip/session/negotiate.go 里 switch 的每个 case 改写成一份
// Descriptor。新加 codec 只需要在这个文件里追加一份描述符，无需修改协商代码。
//
// 偏好顺序保持与历史一致：pcma > pcmu > g722 > opus。

import (
	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
)

func init() {
	Register(g711Descriptor("pcma", 0))
	Register(g711Descriptor("pcmu", 1))
	Register(g722Descriptor())
	Register(opusDescriptor())
}

// g711Descriptor 生成 pcma / pcmu 描述符。这两个 codec 协商规则完全一致，
// 只是 codec name 与偏好不同，所以共用一个工厂。
func g711Descriptor(name string, pref int) Descriptor {
	return Descriptor{
		Name:       name,
		Preference: pref,
		BuildNegotiated: func(offer sdp.Codec) (media.CodecConfig, sdp.Codec) {
			ch := offer.Channels
			if ch < 1 {
				ch = 1
			}
			neg := sdp.Codec{
				PayloadType: offer.PayloadType,
				Name:        name,
				ClockRate:   offer.ClockRate,
				Channels:    ch,
			}
			src := media.CodecConfig{
				Codec:         name,
				SampleRate:    offer.ClockRate,
				Channels:      1,
				BitDepth:      8,
				PayloadType:   offer.PayloadType,
				FrameDuration: "20ms",
			}
			return src, neg
		},
		InternalPCMRate: func(src media.CodecConfig) int {
			if src.SampleRate > 0 {
				return src.SampleRate
			}
			return 8000
		},
	}
}

// g722Descriptor —— G.722 是 16 kHz 编码，但 RTP clock 报作 8000 是历史包袱。
// 内部 PCM 桥按 16 kHz 跑以避免重采样开销。
func g722Descriptor() Descriptor {
	return Descriptor{
		Name:       "g722",
		Preference: 2,
		BuildNegotiated: func(offer sdp.Codec) (media.CodecConfig, sdp.Codec) {
			neg := sdp.Codec{
				PayloadType: offer.PayloadType,
				Name:        "g722",
				ClockRate:   8000,
				Channels:    1,
			}
			src := media.CodecConfig{
				Codec:         "g722",
				SampleRate:    16000,
				Channels:      1,
				BitDepth:      16,
				PayloadType:   offer.PayloadType,
				FrameDuration: "20ms",
			}
			return src, neg
		},
		InternalPCMRate: func(src media.CodecConfig) int { return 16000 },
	}
}

// opusDescriptor —— Opus 支持 8/12/16/24/48 kHz；通道数取 offer 给的，clamp 到 1-2；
// 内部 PCM 桥取与 RTP 一致的采样率，避免重采样链路。
func opusDescriptor() Descriptor {
	return Descriptor{
		Name:       "opus",
		Preference: 3,
		BuildNegotiated: func(offer sdp.Codec) (media.CodecConfig, sdp.Codec) {
			decodeCh := offer.Channels
			if decodeCh < 1 {
				decodeCh = 1
			}
			if decodeCh > 2 {
				decodeCh = 2
			}
			neg := sdp.Codec{
				PayloadType: offer.PayloadType,
				Name:        "opus",
				ClockRate:   offer.ClockRate,
				Channels:    decodeCh,
			}
			src := media.CodecConfig{
				Codec:              "opus",
				SampleRate:         offer.ClockRate,
				Channels:           1,
				OpusDecodeChannels: decodeCh,
				BitDepth:           16,
				PayloadType:        offer.PayloadType,
				FrameDuration:      "20ms",
			}
			return src, neg
		},
		InternalPCMRate: opusInternalRate,
	}
}

// opusInternalRate —— Opus 合法采样率枚举之外的输入，按最近上界回落，
// 但要避免选 48k 把 8k 通话也推到高采样率。逻辑保持与历史一致。
func opusInternalRate(src media.CodecConfig) int {
	switch src.SampleRate {
	case 8000, 12000, 16000, 24000, 48000:
		return src.SampleRate
	}
	switch {
	case src.SampleRate > 36000:
		return 48000
	case src.SampleRate > 20000:
		return 24000
	case src.SampleRate > 14000:
		return 16000
	case src.SampleRate > 10000:
		return 12000
	case src.SampleRate > 0:
		return 8000
	default:
		return 48000
	}
}
