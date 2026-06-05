//go:build opus
// +build opus

package encoder

func init() {
	// Register OPUS codec when build tag is enabled
	RegisterCodec(CodecOPUS, createOPUSEncode, createOPUSDecode)
}
