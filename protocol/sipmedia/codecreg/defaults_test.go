package codecreg

import (
	"testing"

	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
)

func TestOpusInternalRate(t *testing.T) {
	cases := []struct {
		rate int
		want int
	}{
		{8000, 8000},
		{16000, 16000},
		{9000, 8000},
		{22000, 24000},
		{40000, 48000},
		{0, 48000},
	}
	for _, tc := range cases {
		got := opusInternalRate(media.CodecConfig{SampleRate: tc.rate})
		if got != tc.want {
			t.Fatalf("rate %d: got %d want %d", tc.rate, got, tc.want)
		}
	}
}

func TestG711Descriptor_BuildNegotiated(t *testing.T) {
	d := g711Descriptor("pcma", 0)
	src, neg := d.BuildNegotiated(sdp.Codec{PayloadType: 8, Name: "PCMA", ClockRate: 8000, Channels: 0})
	if src.Codec != "pcma" || neg.Name != "pcma" || neg.Channels != 1 {
		t.Fatalf("pcma: src=%+v neg=%+v", src, neg)
	}
}
