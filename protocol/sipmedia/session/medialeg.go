package session

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/media/encoder"
	siprtp "github.com/LingByte/lingllm/protocol/sipmedia/rtp"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
)

const defaultOutputQueue = 512

// MediaLegConfig optional tuning for MediaLeg.
type MediaLegConfig struct {
	OutputQueueSize   int  // default 512
	MaxSessionSeconds int  // 0 = no limit; passed to media.MediaSession.MaxSessionDuration (seconds)
	AllowUplinkEcho   bool // if false (default), set media.KeySIPSuppressUplinkEcho
}

// MediaLeg is one RTP leg wired to a MediaSession (decode uplink / encode downlink) using pkg/media/encoder.
type MediaLeg struct {
	callID   string
	rtpSess  *siprtp.Session
	media    *media.MediaSession
	neg      sdp.Codec
	srcCodec media.CodecConfig
	pcmSR    int // negotiated internal PCM sample rate (decode ↔ encode bridge)
	dtmfPT   uint8

	rx *siprtp.SIPRTPTransport
	tx *siprtp.SIPRTPTransport

	ctx    context.Context
	cancel context.CancelFunc

	startOnce sync.Once
}

// NewMediaLeg builds decode/encode chain from the remote SDP offer codecs and attaches RTP transports.
// rtpSess must already be listening; call ApplyRemoteSDP before Start if offer contains c=/m=.
func NewMediaLeg(parent context.Context, callID string, rtpSess *siprtp.Session, offer []sdp.Codec, cfg MediaLegConfig) (*MediaLeg, error) {
	if parent == nil {
		parent = context.Background()
	}
	if callID == "" {
		return nil, fmt.Errorf("sip1/session: empty callID")
	}
	if rtpSess == nil {
		return nil, fmt.Errorf("sip1/session: nil rtp session")
	}
	src, neg, err := NegotiateOffer(offer)
	if err != nil {
		return nil, err
	}
	pcmSR := InternalPCMSampleRate(src)
	pcm := media.CodecConfig{
		Codec:         "pcm",
		SampleRate:    pcmSR,
		Channels:      1,
		BitDepth:      16,
		FrameDuration: "",
	}
	dec, err := encoder.CreateDecode(src, pcm)
	if err != nil {
		return nil, fmt.Errorf("sip1/session: CreateDecode: %w", err)
	}
	dec = passthroughDTMFDecode(dec)
	enc, err := encoder.CreateEncode(src, pcm)
	if err != nil {
		return nil, fmt.Errorf("sip1/session: CreateEncode: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)
	dtmf := telephoneEventPT(offer, src.SampleRate)
	leg := &MediaLeg{
		callID:   callID,
		rtpSess:  rtpSess,
		neg:      neg,
		srcCodec: src,
		pcmSR:    pcmSR,
		dtmfPT:   dtmf,
		ctx:      ctx,
		cancel:   cancel,
	}
	leg.rx = siprtp.NewSIPRTPTransport(rtpSess, src, media.DirectionInput, dtmf)
	leg.rx.JitterPlaybackDelay = siprtp.DefaultJitterPlaybackDelay
	leg.tx = siprtp.NewSIPRTPTransport(rtpSess, src, media.DirectionOutput, 0)

	q := cfg.OutputQueueSize
	if q <= 0 {
		q = defaultOutputQueue
	}
	ms := media.NewDefaultSession().Context(ctx).SetSessionID("sip-" + callID)
	ms.QueueSize = q
	ms.Decode(dec).Encode(enc).Input(leg.rx).Output(leg.tx)
	if !cfg.AllowUplinkEcho {
		ms.Set(media.KeySIPSuppressUplinkEcho, true)
	}
	if cfg.MaxSessionSeconds > 0 {
		ms.MaxSessionDuration = cfg.MaxSessionSeconds
	}
	leg.media = ms
	return leg, nil
}

// ApplyRemoteSDP sets RTP remote address from sdp.Info (m= / c=).
func ApplyRemoteSDP(sess *siprtp.Session, info *sdp.Info) error {
	if sess == nil || info == nil {
		return fmt.Errorf("sip1/session: nil session or sdp info")
	}
	if info.IP == "" || info.Port <= 0 {
		return fmt.Errorf("sip1/session: sdp missing ip/port")
	}
	ip := net.ParseIP(info.IP)
	if ip == nil {
		return fmt.Errorf("sip1/session: bad sdp ip %q", info.IP)
	}
	sess.SetRemoteAddr(&net.UDPAddr{IP: ip, Port: info.Port})
	return nil
}

// MediaSession exposes the underlying media pipeline.
func (l *MediaLeg) MediaSession() *media.MediaSession {
	if l == nil {
		return nil
	}
	return l.media
}

// RTPSession exposes the RTP UDP session.
func (l *MediaLeg) RTPSession() *siprtp.Session {
	if l == nil {
		return nil
	}
	return l.rtpSess
}

// NegotiatedSDP returns the chosen sdp.Codec line.
func (l *MediaLeg) NegotiatedSDP() sdp.Codec {
	if l == nil {
		return sdp.Codec{}
	}
	return l.neg
}

// SourceCodec returns the negotiated RTP media.CodecConfig.
func (l *MediaLeg) SourceCodec() media.CodecConfig {
	if l == nil {
		return media.CodecConfig{}
	}
	return l.srcCodec
}

// PCMSampleRate returns the internal mono PCM sample rate between decode and encode.
func (l *MediaLeg) PCMSampleRate() int {
	if l == nil || l.pcmSR <= 0 {
		return 16000
	}
	return l.pcmSR
}

// DTMFPayloadType returns the negotiated RFC 2833 telephone-event PT, or 0.
func (l *MediaLeg) DTMFPayloadType() uint8 {
	if l == nil {
		return 0
	}
	return l.dtmfPT
}

// StopMediaPreserveRTP stops the MediaSession and RTP transport loops but keeps the UDP socket
// open so new SIPRTPTransport instances can attach for a two-leg bridge (same idea as pkg/sip/session.CallSession).
func (l *MediaLeg) StopMediaPreserveRTP() {
	if l == nil {
		return
	}
	if l.rx != nil {
		l.rx.PreserveSessionOnClose = true
	}
	if l.tx != nil {
		l.tx.PreserveSessionOnClose = true
	}
	if l.rtpSess != nil && l.rtpSess.Conn != nil {
		_ = l.rtpSess.Conn.SetReadDeadline(time.Now())
	}
	if l.cancel != nil {
		l.cancel()
	}
	if l.media != nil {
		_ = l.media.Close()
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = l.media.WaitServeShutdown(drainCtx)
		drainCancel()
		l.media = nil
	}
	if l.rtpSess != nil && l.rtpSess.Conn != nil {
		_ = l.rtpSess.Conn.SetReadDeadline(time.Time{})
	}
}

// Start runs MediaSession.Serve in a background goroutine (idempotent).
func (l *MediaLeg) Start() {
	if l == nil || l.media == nil {
		return
	}
	l.startOnce.Do(func() {
		l.media.NotifyServeStarting()
		go func() { _ = l.media.Serve() }()
	})
}

// CloseRTPOnly closes the RTP UDP socket without touching a nil MediaSession (e.g. after WebRTC bridge teardown).
func (l *MediaLeg) CloseRTPOnly() {
	if l == nil {
		return
	}
	if l.rtpSess != nil {
		_ = l.rtpSess.Close()
		l.rtpSess = nil
	}
}

// Stop closes media and RTP.
func (l *MediaLeg) Stop() {
	if l == nil {
		return
	}
	if l.cancel != nil {
		l.cancel()
	}
	if l.media != nil {
		_ = l.media.Close()
		l.media = nil
	}
	if l.rtpSess != nil {
		_ = l.rtpSess.Close()
		l.rtpSess = nil
	}
}
