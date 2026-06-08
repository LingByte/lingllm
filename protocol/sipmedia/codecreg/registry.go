// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Package codecreg holds the SIP audio codec negotiation registry.
//
// Pre-refactor: pkg/sip/session/negotiate.go 把 pcmu/pcma/g722/opus 的协商规则
// 用一个大 switch 写死，添加新 codec（例如 G.729 / iLBC / EVS）要同时改：
//
//  1. NegotiateOffer 的 case 分支
//  2. InternalPCMSampleRate 的 case 分支
//  3. preferred 排序 map
//  4. 容错文案
//
// 这个包把每个 codec 抽象成 Descriptor，注册到全局表里。新增 codec 写一个
// Descriptor + 在 init() 里 Register 即可，协商代码不再动。
//
// 编解码字节流（PCM ↔ 网络字节）的工厂在 pkg/media/encoder/registry.go，
// 那是单独一层；本包只关心"协商哪个 codec、桥接 PCM 用什么采样率"。
package codecreg

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
)

// Descriptor 描述一种 SIP 音频 codec 的协商规则。
//
// 设计：
//   - Name 全部 lowercase，匹配时统一 ToLower。
//   - Preference 数值越小越优先，便于在 offer 列表里挑出首选。
//   - BuildNegotiated 从 offer 的原始 sdp.Codec 派生：
//     (a) 落到媒体 codec config（送给 pkg/media encoder/decoder）；
//     (b) 落到 sdp 应答 codec（送回对端）；
//   - InternalPCMRate 决定 MediaSession 内部 PCM 桥的采样率，避免不必要的重采样。
//
// 注：BuildNegotiated 永远不应返回 error；如果 offer 字段非法，应在内部
// 兜底为合理默认（同既有逻辑）。这样负责协商的代码不必对每个 codec 做 if-err。
type Descriptor struct {
	Name            string
	Preference      int
	BuildNegotiated func(offer sdp.Codec) (media.CodecConfig, sdp.Codec)
	InternalPCMRate func(src media.CodecConfig) int
}

// registry 是模块私有状态。读多写少，启动期写一次（init），之后只读。
// sync.RWMutex 保护以支持运行时动态注册（未来插件场景）。
var (
	regMu  sync.RWMutex
	regMap = make(map[string]Descriptor)
)

// Register 注册或覆盖一个 codec 描述符。
// 重复 Register 同名 codec 会覆盖（用例：业务想替换默认 opus 的桥接策略）。
func Register(d Descriptor) {
	name := strings.ToLower(strings.TrimSpace(d.Name))
	if name == "" {
		return
	}
	if d.BuildNegotiated == nil || d.InternalPCMRate == nil {
		panic(fmt.Sprintf("codecreg: descriptor %q missing required builders", name))
	}
	d.Name = name
	regMu.Lock()
	regMap[name] = d
	regMu.Unlock()
}

// Lookup 按名字返回描述符；不存在返回 ok=false。
func Lookup(name string) (Descriptor, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	regMu.RLock()
	d, ok := regMap[n]
	regMu.RUnlock()
	return d, ok
}

// All 返回当前已注册描述符的一份快照（不暴露内部 map，避免外部并发改）。
// 仅用于诊断 / 调试 endpoint。
func All() []Descriptor {
	regMu.RLock()
	out := make([]Descriptor, 0, len(regMap))
	for _, d := range regMap {
		out = append(out, d)
	}
	regMu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Preference != out[j].Preference {
			return out[i].Preference < out[j].Preference
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// NegotiateOffer 从远端 SDP offer 中挑一个我们支持的 codec。
// 选择策略：先按本地 Descriptor.Preference 排序，第一个能匹配上的就用。
//
// 注意：偏好排序基于"我们注册了的"codec，对端没有 offer 的 codec 自然
// 不会被选出来。这避免了"远端把奇怪 codec 排前面我们却被迫选它"的坑。
func NegotiateOffer(offer []sdp.Codec) (media.CodecConfig, sdp.Codec, error) {
	if len(offer) == 0 {
		return media.CodecConfig{}, sdp.Codec{}, fmt.Errorf("codecreg: empty offer")
	}
	// 把对端 offer 复制一份，按本地偏好稳定排序。
	codecs := make([]sdp.Codec, len(offer))
	copy(codecs, offer)
	regMu.RLock()
	prefOf := func(name string) int {
		if d, ok := regMap[strings.ToLower(strings.TrimSpace(name))]; ok {
			return d.Preference
		}
		// 未注册的 codec 排到最后；后面也不会选中（Lookup 会失败）。
		return 1 << 30
	}
	sort.SliceStable(codecs, func(i, j int) bool {
		return prefOf(codecs[i].Name) < prefOf(codecs[j].Name)
	})
	regMu.RUnlock()

	for _, c := range codecs {
		if d, ok := Lookup(c.Name); ok {
			src, neg := d.BuildNegotiated(c)
			return src, neg, nil
		}
	}
	return media.CodecConfig{}, sdp.Codec{}, fmt.Errorf(
		"codecreg: no supported codec in offer (registered=%v)", registeredNames())
}

// InternalPCMSampleRate 把已协商 codec 桥接到 MediaSession PCM 时用的采样率。
// 未注册 codec 走最保守的回退：使用 src.SampleRate（若有）或 16 kHz。
func InternalPCMSampleRate(src media.CodecConfig) int {
	if d, ok := Lookup(src.Codec); ok {
		return d.InternalPCMRate(src)
	}
	if src.SampleRate > 0 {
		return src.SampleRate
	}
	return 16000
}

func registeredNames() []string {
	all := All()
	out := make([]string, 0, len(all))
	for _, d := range all {
		out = append(out, d.Name)
	}
	return out
}
