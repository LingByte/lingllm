package session

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/LingByte/lingllm/protocol/sipmedia/internal/siplog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LingByte/lingllm/media"
	"github.com/LingByte/lingllm/media/encoder"
	"github.com/LingByte/lingllm/protocol/sip/hooks"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sipmedia/rtp"
)

// SIP recording blob format v3 (sippersist): magic "SN3" then repeated
// [dir u8][seq u16LE][rtpTs u32LE][wallNs u64LE][len u16LE][payload].
// wallNs is nanoseconds since the first captured frame (time.Since anchor): restores real gaps between
// TTS phrases when RTP timestamps stay continuous across silence (unlike SN2 RTP-only placement).
// Legacy "SN2" blobs remain readable in pkg/utils/sip_recording_wav.go.
const recBlobMagic = "SN3"

const (
	recDirUser = 0
	recDirAI   = 1
)

// RecordingDirUser / RecordingDirAI match SN3 dir bytes for AppendRecordingSample (e.g. SIP transfer raw relay).
const (
	RecordingDirUser = recDirUser
	RecordingDirAI   = recDirAI
)

// CallSession binds an RTP session to a MediaSession for SIP calls.
//
// Uplink: RTP -> decode -> PCM for ASR processors.
// Downlink: only synthesized (TTS) PCM is encoded and sent as RTP; uplink is not echoed
// (see media.KeySIPSuppressUplinkEcho).
type CallSession struct {
	CallID          string
	rtpSess         *rtp.Session
	media           *media.MediaSession
	neg             sdp.Codec
	rxTransport     *rtp.SIPRTPTransport // RTP transports and codec (same as used for MediaSession) for handoff to in-process PCM bridge.
	txTransport     *rtp.SIPRTPTransport
	srcCodec        media.CodecConfig
	pcmSampleRate   int // internal PCM bridge rate (matches InternalPCMSampleRate(src))
	dtmfPT          uint8
	ctx             context.Context
	cancel          context.CancelFunc
	startOnce       sync.Once
	ackOnce         sync.Once // For SIP: media starts on ACK, not on INVITE.
	voiceMu         sync.Mutex
	voiceAttached   bool
	recMu           sync.Mutex
	recBuf          []byte
	recTimeOrigin   time.Time // first appendRecordingFrame sets anchor for wallNs (monotonic via time.Since)
	recorderEnabled bool
	recordingSink   hooks.RecordingSink // set by EnableRecorder; nil uses hooks.DefaultRegistry.Recording

	// remoteFromHeader 保存呼入 INVITE 的原始 From header（含 display-name +
	// SIP URI），由 sip/server 在创建 CallSession 后立刻 SetRemoteFromHeader
	// 注入。给转接路径用——转接时希望坐席话机上显示的是【真实主叫的手机号】，
	// 而不是平台 400 中继号；从这里取出 user 部分回写到外呼 DialRequest 的
	// CallerDisplayName 即可。出局 / 主动 Dial 的 CallSession 这里就是空。
	metaMu           sync.RWMutex
	remoteFromHeader string
	// remoteToHeader / inboundHistoryInfoRaw / inboundDiversionRaw are
	// captured at INVITE intake by sip/server. They feed the B2BUA
	// transfer path (RFC 7044 History-Info + RFC 5806 Diversion chain
	// extension) so retargeted legs surface the original To URI and
	// any upstream SBC retarget history downstream. We store them as
	// raw header strings (not parsed entries) on purpose:
	//   - keeps pkg/sip/session free of a dependency on pkg/sip/historyinfo
	//   - tolerates malformed headers (parsing failures don't break call setup)
	//   - lets the conversation layer re-parse on each retarget, which is
	//     cheap and avoids storing parser-version-bound structs in the
	//     long-lived session
	remoteToHeader        string
	inboundHistoryInfoRaw string
	inboundDiversionRaw   string
	tenantID              uint
	inboundUnboundTenant  bool

	// RFC 4028 Session Timer state. When we're the refreshee (the
	// usual case for inbound legs — see pkg/sip/session_timer), the
	// peer is contractually obligated to send a re-INVITE / UPDATE
	// within `sessionTimerInterval` seconds; if they don't, we MUST
	// BYE per RFC 4028 §10. Reset on every in-dialog re-INVITE /
	// UPDATE arrival via TouchSessionTimer.
	//
	// Stored as a *time.Timer rather than a goroutine so resetting
	// is allocation-free in the steady state and there's nothing to
	// gc when the dialog ends normally.
	sessionTimerMu       sync.Mutex
	sessionTimerInterval time.Duration
	sessionTimerWatchdog *time.Timer
}

// SetRemoteFromHeader 由 sip/server 在 INVITE 入站时调用，记录 PSTN 主叫
// 的原始 From header 供转接路径回显。nil-safe / 空值跳过。
func (cs *CallSession) SetRemoteFromHeader(v string) {
	if cs == nil {
		return
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	cs.metaMu.Lock()
	cs.remoteFromHeader = v
	cs.metaMu.Unlock()
}

// RemoteFromHeader 返回入站 INVITE 的 From header；nil 或未注入时返回 ""。
func (cs *CallSession) RemoteFromHeader() string {
	if cs == nil {
		return ""
	}
	cs.metaMu.RLock()
	defer cs.metaMu.RUnlock()
	return cs.remoteFromHeader
}

// SetInboundRetargetHeaders captures the raw To header plus any
// pre-existing History-Info / Diversion headers from the inbound
// INVITE. Called by sip/server at INVITE intake. Arguments are
// trimmed; empty ones are ignored so re-calling with partial info
// doesn't clobber previously-stored values.
func (cs *CallSession) SetInboundRetargetHeaders(toHdr, historyInfo, diversion string) {
	if cs == nil {
		return
	}
	to := strings.TrimSpace(toHdr)
	hi := strings.TrimSpace(historyInfo)
	dv := strings.TrimSpace(diversion)
	if to == "" && hi == "" && dv == "" {
		return
	}
	cs.metaMu.Lock()
	defer cs.metaMu.Unlock()
	if to != "" {
		cs.remoteToHeader = to
	}
	if hi != "" {
		cs.inboundHistoryInfoRaw = hi
	}
	if dv != "" {
		cs.inboundDiversionRaw = dv
	}
}

// InboundRetargetHeaders returns (rawTo, rawHistoryInfo, rawDiversion)
// captured at INVITE intake. Empty strings when not set (e.g. outbound-
// originated CallSession). Consumers should treat the strings as
// opaque SIP header values and pass them through historyinfo.ParseChain
// / ParseDiversionChain.
func (cs *CallSession) InboundRetargetHeaders() (string, string, string) {
	if cs == nil {
		return "", "", ""
	}
	cs.metaMu.RLock()
	defer cs.metaMu.RUnlock()
	return cs.remoteToHeader, cs.inboundHistoryInfoRaw, cs.inboundDiversionRaw
}

// ArmSessionTimerWatchdog starts an RFC 4028 watchdog. If no
// TouchSessionTimer call arrives within `interval`, onExpiry runs
// (the SIP server wires this to "send BYE with Reason: SIP;cause=408").
//
// Calling Arm a second time replaces any prior watchdog (e.g. timer
// was renegotiated by a re-INVITE). interval <= 0 stops the watchdog.
//
// Safe to call on nil receiver (no-op).
func (cs *CallSession) ArmSessionTimerWatchdog(interval time.Duration, onExpiry func()) {
	if cs == nil {
		return
	}
	cs.sessionTimerMu.Lock()
	defer cs.sessionTimerMu.Unlock()

	if cs.sessionTimerWatchdog != nil {
		cs.sessionTimerWatchdog.Stop()
		cs.sessionTimerWatchdog = nil
	}
	cs.sessionTimerInterval = 0

	if interval <= 0 || onExpiry == nil {
		return
	}
	cs.sessionTimerInterval = interval
	cs.sessionTimerWatchdog = time.AfterFunc(interval, onExpiry)
}

// TouchSessionTimer resets the watchdog back to the full interval.
// Call this every time we accept a refresh re-INVITE or UPDATE in
// the dialog. No-op when no watchdog is armed.
func (cs *CallSession) TouchSessionTimer() {
	if cs == nil {
		return
	}
	cs.sessionTimerMu.Lock()
	defer cs.sessionTimerMu.Unlock()
	if cs.sessionTimerWatchdog == nil || cs.sessionTimerInterval <= 0 {
		return
	}
	cs.sessionTimerWatchdog.Reset(cs.sessionTimerInterval)
}

// StopSessionTimer cancels any pending watchdog. Called automatically
// from Stop(); also called by the server when BYE arrives before
// expiry.
func (cs *CallSession) StopSessionTimer() {
	if cs == nil {
		return
	}
	cs.sessionTimerMu.Lock()
	defer cs.sessionTimerMu.Unlock()
	if cs.sessionTimerWatchdog != nil {
		cs.sessionTimerWatchdog.Stop()
		cs.sessionTimerWatchdog = nil
	}
	cs.sessionTimerInterval = 0
}

// SetTenantID records the tenant scope for media/AI (inbound DID or outbound campaign).
func (cs *CallSession) SetTenantID(id uint) {
	if cs == nil {
		return
	}
	cs.metaMu.Lock()
	cs.tenantID = id
	cs.metaMu.Unlock()
}

// TenantID returns the tenant id bound to this call (0 = none / legacy).
func (cs *CallSession) TenantID() uint {
	if cs == nil {
		return 0
	}
	cs.metaMu.RLock()
	defer cs.metaMu.RUnlock()
	return cs.tenantID
}

// SetInboundUnboundTenant marks inbound calls where the called DID is not assigned to any tenant.
func (cs *CallSession) SetInboundUnboundTenant(v bool) {
	if cs == nil {
		return
	}
	cs.metaMu.Lock()
	cs.inboundUnboundTenant = v
	cs.metaMu.Unlock()
}

// InboundUnboundTenant is true when the inbound number is not bound to a tenant (play not_bind.wav).
func (cs *CallSession) InboundUnboundTenant() bool {
	if cs == nil {
		return false
	}
	cs.metaMu.RLock()
	defer cs.metaMu.RUnlock()
	return cs.inboundUnboundTenant
}

// NewCallSession creates a call session with codec negotiation from SDP.
func NewCallSession(callID string, rtpSess *rtp.Session, sdpCodecs []sdp.Codec) (*CallSession, error) {
	if callID == "" {
		return nil, fmt.Errorf("sip: empty callID")
	}
	if rtpSess == nil {
		return nil, fmt.Errorf("sip: nil rtp session")
	}
	if len(sdpCodecs) == 0 {
		return nil, fmt.Errorf("sip: empty sdp codecs")
	}
	preferredCodecs := map[string]int{
		// Prefer narrowband G.711 A-law first for best PSTN/carrier interoperability.
		// Order matters when multiple codecs are offered; we pick the first supported.
		"pcma": 0,
		"pcmu": 1,
		"g722": 2,
		"opus": 3,
	}
	codecs := make([]sdp.Codec, len(sdpCodecs))
	copy(codecs, sdpCodecs)
	sort.SliceStable(codecs, func(i, j int) bool {
		ci := strings.ToLower(strings.TrimSpace(codecs[i].Name))
		cj := strings.ToLower(strings.TrimSpace(codecs[j].Name))
		ri, okI := preferredCodecs[ci]
		rj, okJ := preferredCodecs[cj]
		if !okI {
			ri = 100
		}
		if !okJ {
			rj = 100
		}
		return ri < rj
	})

	// Choose the first supported codec by preference.
	var src media.CodecConfig
	negotiatedPayloadType := uint8(0)
	var negotiatedSDP sdp.Codec
	found := false
	for _, c := range codecs {
		switch c.Name {
		case "pcmu", "pcma":
			found = true
			negotiatedPayloadType = c.PayloadType
			negotiatedSDP = c
			negotiatedSDP.Channels = 1
			src = media.CodecConfig{
				Codec:         c.Name, // "pcmu" or "pcma"
				SampleRate:    c.ClockRate,
				Channels:      1,
				BitDepth:      8, // PCMU/PCMA payload is 8-bit
				PayloadType:   negotiatedPayloadType,
				FrameDuration: "20ms",
			}
			break
		case "g722":
			found = true
			negotiatedPayloadType = c.PayloadType
			negotiatedSDP = c
			negotiatedSDP.Channels = 1
			src = media.CodecConfig{
				Codec:         "g722",
				SampleRate:    16000,
				Channels:      1,
				BitDepth:      16,
				PayloadType:   negotiatedPayloadType,
				FrameDuration: "20ms",
			}
			break
		case "opus":
			found = true
			negotiatedPayloadType = c.PayloadType
			decodeCh := c.Channels
			if decodeCh < 1 {
				decodeCh = 1
			}
			if decodeCh > 2 {
				decodeCh = 2
			}
			negotiatedSDP = c
			// 200 OK SDP must match offered channel count (e.g. OPUS/48000/2). Answering /1 while
			// the peer sends stereo RTP breaks several stacks; we still encode TTS mono (Channels:1).
			negotiatedSDP.Channels = decodeCh
			src = media.CodecConfig{
				Codec:              "opus",
				SampleRate:         c.ClockRate, // typically 48000
				Channels:           1,
				OpusDecodeChannels: decodeCh,
				BitDepth:           16,
				PayloadType:        negotiatedPayloadType,
				FrameDuration:      "20ms",
			}
			break
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("sip: unsupported codec (need one of: opus/g722/pcmu/pcma)")
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
		return nil, fmt.Errorf("sip: CreateDecode failed: %w", err)
	}
	dec = passthroughDTMFDecode(dec)
	enc, err := encoder.CreateEncode(src, pcm)
	if err != nil {
		return nil, fmt.Errorf("sip: CreateEncode failed: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	dtmfPT := telephoneEventPayloadType(sdpCodecs)
	cs := &CallSession{
		CallID:        callID,
		rtpSess:       rtpSess,
		neg:           negotiatedSDP,
		srcCodec:      src,
		pcmSampleRate: pcmSR,
		dtmfPT:        dtmfPT,
		ctx:           ctx,
		cancel:        cancel,
	}
	rxTransport := rtp.NewSIPRTPTransport(rtpSess, src, media.DirectionInput, dtmfPT)
	rxTransport.JitterPlaybackDelay = rtp.DefaultJitterPlaybackDelay
	rxTransport.OnInputRTP = func(seq uint16, ts uint32, p []byte) { cs.appendRecordingFrame(recDirUser, seq, ts, p) }
	txTransport := rtp.NewSIPRTPTransport(rtpSess, src, media.DirectionOutput, 0)
	txTransport.OnOutputRTP = func(seq uint16, ts uint32, p []byte) { cs.appendRecordingFrame(recDirAI, seq, ts, p) }
	cs.rxTransport = rxTransport
	cs.txTransport = txTransport

	ms := media.NewDefaultSession().Context(ctx).SetSessionID("sip-call-" + callID)
	ms.QueueSize = 512
	ms.Decode(dec).
		Encode(enc).
		Input(rxTransport).
		Output(txTransport)
	ms.Set(media.KeySIPSuppressUplinkEcho, true)
	cs.media = ms

	return cs, nil
}

// MediaSession exposes the underlying media pipeline for voice processors (ASR/TTS hooks).
func (cs *CallSession) MediaSession() *media.MediaSession {
	if cs == nil {
		return nil
	}
	return cs.media
}

// AttachVoiceConversation runs fn once before media Serve() (typically from ACK) to register
// processors or other hooks. If fn fails, a later call may retry.
func (cs *CallSession) AttachVoiceConversation(fn func() error) error {
	if cs == nil || fn == nil {
		return nil
	}
	cs.voiceMu.Lock()
	defer cs.voiceMu.Unlock()
	if cs.voiceAttached {
		siplog.L.Debug("sip session: voice attach skipped (already attached; often duplicate ACK)")
		return nil
	}
	if err := fn(); err != nil {
		return err
	}
	cs.voiceAttached = true
	return nil
}

func passthroughDTMFDecode(dec media.EncoderFunc) media.EncoderFunc {
	return func(p media.MediaPacket) ([]media.MediaPacket, error) {
		if _, ok := p.(*media.DTMFPacket); ok {
			return []media.MediaPacket{p}, nil
		}
		return dec(p)
	}
}

func telephoneEventPayloadType(codecs []sdp.Codec) uint8 {
	for _, c := range codecs {
		if strings.EqualFold(strings.TrimSpace(c.Name), "telephone-event") {
			return c.PayloadType
		}
	}
	return 0
}

func (cs *CallSession) NegotiatedCodec() sdp.Codec {
	if cs == nil {
		return sdp.Codec{}
	}
	return cs.neg
}

// RTCPStats returns a one-shot RTCP counter snapshot (safe at BYE; O(1)).
func (cs *CallSession) RTCPStats() rtp.RTCPStats {
	if cs == nil || cs.rtpSess == nil {
		return rtp.RTCPStats{}
	}
	return cs.rtpSess.RTCPSnapshot()
}

// RTPSession returns the underlying RTP/UDP session (for building a transfer bridge).
func (cs *CallSession) RTPSession() *rtp.Session {
	if cs == nil {
		return nil
	}
	return cs.rtpSess
}

// SourceCodec is the negotiated RTP codec (PCMU/PCMA/G722/OPUS) for this leg.
func (cs *CallSession) SourceCodec() media.CodecConfig {
	if cs == nil {
		return media.CodecConfig{}
	}
	return cs.srcCodec
}

// PCMSampleRate is the internal mono PCM rate produced by RTP decode (and fed to ASR processors).
func (cs *CallSession) PCMSampleRate() int {
	if cs == nil || cs.pcmSampleRate <= 0 {
		return 16000
	}
	return cs.pcmSampleRate
}

// DTMFPayloadType is the negotiated telephone-event PT, or 0 if none.
func (cs *CallSession) DTMFPayloadType() uint8 {
	if cs == nil {
		return 0
	}
	return cs.dtmfPT
}

// StopMediaPreserveRTP stops the MediaSession (AI pipeline, RTP read/write loops) but keeps the UDP
// socket open so new SIPRTPTransport instances can attach for bridging.
func (cs *CallSession) StopMediaPreserveRTP() {
	if cs == nil {
		return
	}
	if cs.rxTransport != nil {
		cs.rxTransport.PreserveSessionOnClose = true
	}
	if cs.txTransport != nil {
		cs.txTransport.PreserveSessionOnClose = true
	}
	// With PreserveSessionOnClose, Transport.Close() does not close the UDP socket, so a goroutine
	// blocked in ReceiveRTP would otherwise keep running. The transfer bridge then reads the same
	// socket and two readers split packets → noise. Wake the blocked read before tearing down media.
	if cs.rtpSess != nil && cs.rtpSess.Conn != nil {
		_ = cs.rtpSess.Conn.SetReadDeadline(time.Now())
	}
	if cs.cancel != nil {
		cs.cancel()
	}
	if cs.media != nil {
		_ = cs.media.Close()
		// Do not hand the RTP socket to the transfer bridge until MediaSession transport goroutines
		// have stopped calling ReadFromUDP — two readers on one UDP socket steal packets.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = cs.media.WaitServeShutdown(drainCtx)
		drainCancel()
	}
	// The wakeup above leaves a past deadline on the conn; the next Read (transfer bridge) would
	// otherwise return i/o timeout immediately and silence audio. Clear the deadline for new readers.
	if cs.rtpSess != nil && cs.rtpSess.Conn != nil {
		_ = cs.rtpSess.Conn.SetReadDeadline(time.Time{})
	}
}

// CloseRTPOnly closes the RTP UDP socket after a bridge or full teardown path.
func (cs *CallSession) CloseRTPOnly() {
	if cs == nil || cs.rtpSess == nil {
		return
	}
	_ = cs.rtpSess.Close()
	cs.rtpSess = nil
}

// Start starts MediaSession serving in background.
func (cs *CallSession) Start() {
	if cs == nil || cs.media == nil {
		return
	}
	cs.startOnce.Do(func() {
		cs.media.NotifyServeStarting()
		go func() {
			_ = cs.media.Serve()
		}()
	})
}

// StartOnACK starts media pipeline once (idempotent) when ACK is received.
func (cs *CallSession) StartOnACK() {
	if cs == nil {
		return
	}
	cs.ackOnce.Do(func() {
		cs.Start()
	})
}

// Stop stops the session and closes underlying RTP resources.
func (cs *CallSession) Stop() {
	if cs == nil {
		return
	}
	// Cancel session timer before everything else so a watchdog fire
	// racing with teardown can't trigger a duplicate BYE on an already
	// dead dialog.
	cs.StopSessionTimer()
	if cs.cancel != nil {
		cs.cancel()
	}
	if cs.media != nil {
		_ = cs.media.Close()
	}
	if cs.rtpSess != nil {
		_ = cs.rtpSess.Close()
		cs.rtpSess = nil
	}
}

// AppendRecordingSample appends one RTP payload to the SN3 blob (used during transfer bridge when
// media bypasses the original rx/tx transports).
func (cs *CallSession) AppendRecordingSample(dir byte, seq uint16, ts uint32, payload []byte) {
	cs.appendRecordingFrame(dir, seq, ts, payload)
}

// WireTransferBridgeRecording attaches SN3 callbacks to PCM-bridge transports sharing inbound RTP session.
func (cs *CallSession) WireTransferBridgeRecording(callerRx, callerTx *rtp.SIPRTPTransport) {
	if cs == nil {
		return
	}
	if callerRx != nil {
		callerRx.OnInputRTP = func(seq uint16, ts uint32, p []byte) {
			cs.appendRecordingFrame(recDirUser, seq, ts, p)
		}
	}
	if callerTx != nil {
		callerTx.OnOutputRTP = func(seq uint16, ts uint32, p []byte) {
			cs.appendRecordingFrame(recDirAI, seq, ts, p)
		}
	}
}

func (cs *CallSession) appendRecordingFrame(dir byte, seq uint16, ts uint32, p []byte) {
	if cs == nil || len(p) == 0 {
		return
	}
	if dir != recDirUser && dir != recDirAI {
		return
	}
	maxB := 50 * 1024 * 1024
	cs.recMu.Lock()
	defer cs.recMu.Unlock()
	if len(cs.recBuf) >= maxB {
		return
	}
	rem := maxB - len(cs.recBuf)
	if rem <= 0 {
		return
	}
	frameOverhead := 1 + 2 + 4 + 8 + 2 // dir + seq + rtpTs + wallNs + uint16 len
	if len(cs.recBuf) == 0 {
		if len(recBlobMagic) > rem {
			return
		}
		cs.recTimeOrigin = time.Now()
		cs.recBuf = append(cs.recBuf, recBlobMagic...)
		rem = maxB - len(cs.recBuf)
	}
	if frameOverhead+len(p) > rem {
		return
	}
	wallNs := uint64(time.Since(cs.recTimeOrigin))
	cs.recBuf = append(cs.recBuf, dir)
	var hdr [16]byte
	binary.LittleEndian.PutUint16(hdr[0:2], seq)
	binary.LittleEndian.PutUint32(hdr[2:6], ts)
	binary.LittleEndian.PutUint64(hdr[6:14], wallNs)
	binary.LittleEndian.PutUint16(hdr[14:16], uint16(len(p)))
	cs.recBuf = append(cs.recBuf, hdr[:]...)
	cs.recBuf = append(cs.recBuf, p...)
}

// TakeRecording returns buffered RTP recording (SN3 …) and clears the buffer.
func (cs *CallSession) TakeRecording() []byte {
	if cs == nil {
		return nil
	}
	cs.recMu.Lock()
	defer cs.recMu.Unlock()
	if len(cs.recBuf) == 0 {
		return nil
	}
	out := make([]byte, len(cs.recBuf))
	copy(out, cs.recBuf)
	cs.recBuf = cs.recBuf[:0]
	cs.recTimeOrigin = time.Time{}
	return out
}
