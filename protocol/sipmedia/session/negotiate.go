package session

// negotiate.go —— SIP 音频 codec 协商。
//
// 历史上这里把 pcmu/pcma/g722/opus 写死在 switch 里。新加 codec（G.729 / iLBC /
// EVS / AMR-WB 等）需要同时改三处分支，是项目"扩展性 4/10"评分的主要扣分点。
//
// 现在这层退化成两个一次性 thin wrapper：真正的协商规则、偏好排序、PCM 桥接
// 都搬到 pkg/sip/codecreg 注册表里。要加 codec？写一份 codecreg.Descriptor 并
// Register 即可，本文件不需要再改。
//
// 兼容性：函数签名与错误返回保持不变；老调用方零修改即可继续工作。

import (
	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sipmedia/codecreg"
)

// NegotiateOffer picks the first supported audio codec from a remote SDP offer (ordered by preference).
func NegotiateOffer(offer []sdp.Codec) (src media.CodecConfig, neg sdp.Codec, err error) {
	return codecreg.NegotiateOffer(offer)
}

// InternalPCMSampleRate chooses the MediaSession PCM bridge rate for a negotiated RTP codec so decode/encode
// avoids unnecessary resampling (e.g. keep G.711 at 8 kHz, Opus at negotiated clock, G.722 at 16 kHz PCM).
func InternalPCMSampleRate(src media.CodecConfig) int {
	return codecreg.InternalPCMSampleRate(src)
}

func telephoneEventPT(offer []sdp.Codec, matchClock int) uint8 {
	c, ok := sdp.PickTelephoneEventFromOffer(offer, matchClock)
	if !ok {
		return 0
	}
	return c.PayloadType
}
