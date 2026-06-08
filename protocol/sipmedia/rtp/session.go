package rtp

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/LingByte/lingllm/protocol/sipmedia/internal/siplog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/srtp/v2"
)

// ErrRTPDiscard signals that a datagram was read but must be ignored (malformed RTP / policy).
// Callers that loop on ReceiveRTP should continue without treating it as a transport failure.
var ErrRTPDiscard = errors.New("rtp: discard packet")

// Session is a minimal RTP-over-UDP session.
//
// RTCP (and SRTCP) is not generated or parsed; SDP may still advertise a=rtcp for peer stacks.
// Optional SRTP (SDES) encrypts/decrypts RTP payloads only ([Session.EnableSDESSRTP]).
//
// It is intentionally protocol-agnostic:
// - Timestamp increments are provided by the caller via `samples` argument.
// - Payload framing / codec packetization happens above this layer.
type Session struct {
	LocalAddr  *net.UDPAddr
	RemoteAddr *net.UDPAddr
	Conn       *net.UDPConn

	// sdpRemote is a copy of the first SetRemoteAddr (from SDP c=/m=). Used only for logs vs symmetric RTP.
	sdpRemote *net.UDPAddr

	// SSRC/sequence/timestamp are advanced by this session.
	SSRC      uint32
	SeqNum    uint16
	Timestamp uint32

	// UDP read signal for "first packet received".
	firstPacketOnce sync.Once
	firstPacketCh   chan struct{}

	logFirstUDP  sync.Once
	logFirstTX   sync.Once
	logStatsOnce sync.Once
	closeOnce    sync.Once
	statsStopCh  chan struct{}

	txPackets uint64
	txBytes   uint64
	rxPackets uint64
	rxBytes   uint64

	firstTxUnixNano int64
	firstRxUnixNano int64
	natWarned       uint32

	closed uint32 // atomic: 1 after Close begins — Send/Receive must not use Conn

	// rxSSRCSeen/rxSSRC lock onto the first observed SSRC for this socket (symmetric RTP).
	rxSSRCSeen uint32 // atomic 0/1
	rxSSRC     uint32 // atomic, valid when rxSSRCSeen==1

	mirrorMu           sync.RWMutex
	mirrorRemotes      []mirrorRemote
	mirrorErrLastLogNs int64 // atomic unix nano — rate-limit mirror write error logs

	srtpMu sync.Mutex
	// SRTP SDES (RFC 3711 + RFC 4568): optional; when set, ReceiveRTP decrypts and SendRTP encrypts.
	srtpDecrypt *srtp.Context
	srtpEncrypt *srtp.Context

	// dtlsRoute is the active DTLS demux route on this socket.
	// Non-nil only during DTLS handshake (single-socket multiplex
	// per RFC 5764 §5.1). ReceiveRTP routes packets with first byte
	// 20-63 here when set; bytes 128-191 take the existing RTP path.
	// See dtls_session.go for the full lifecycle.
	dtlsMu    sync.Mutex
	dtlsRoute *dtlsConn

	// RFC 3550 RTCP companion socket on RTP port + 1. nil when bind
	// failed (best-effort) or before startRTCP / after stopRTCP.
	// rtcpConn / rtcpStopCh are guarded by rtcpMu to keep the close
	// path race-free vs. the read/send goroutines.
	rtcpMu            sync.Mutex
	rtcpConn          *net.UDPConn
	rtcpStopCh        chan struct{}
	rtcpTxWarnOnce    sync.Once
	rtcpLastSentRTPTS uint32 // atomic: last RTP timestamp we transmitted (for SR.RTPTime)
	rxStats           rtcpReceiverStats
	txEcho            rtcpSenderEcho
}

type mirrorRemote struct {
	addr      *net.UDPAddr
	expiresAt time.Time
}

type SessionStats struct {
	LocalSocket string
	RemoteSDP   string
	RemoteNow   string
	TXPackets   uint64
	TXBytes     uint64
	RXPackets   uint64
	RXBytes     uint64
	FirstTXAgo  int64
	FirstRXAgo  int64
}

// NewSession creates a RTP UDP session.
//
// If localPort is 0 or negative, the OS will choose an available ephemeral port.
func NewSession(localPort int) (*Session, error) {
	addr := &net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: localPort,
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("rtp: listen udp: %w", err)
	}

	var rnd [10]byte
	ssrc, seq, ts := uint32(0xDEADBEEF), uint16(0x1357), uint32(0x2468ACE0)
	if _, err := rand.Read(rnd[:]); err == nil {
		ssrc = binary.BigEndian.Uint32(rnd[0:4])
		seq = binary.BigEndian.Uint16(rnd[4:6])
		ts = binary.BigEndian.Uint32(rnd[6:10])
	} else {
		now := time.Now().UnixNano()
		ssrc = uint32(now ^ (now >> 32))
		seq = uint16(now)
		ts = uint32(now >> 8)
	}
	if ssrc == 0 {
		ssrc = binary.BigEndian.Uint32(rnd[0:4]) ^ 0xA5A5A5A5
	}
	if ssrc == 0 {
		ssrc = 0xBADC0FFE
	}

	s := &Session{
		LocalAddr:     conn.LocalAddr().(*net.UDPAddr),
		Conn:          conn,
		SSRC:          ssrc,
		SeqNum:        seq,
		Timestamp:     ts,
		firstPacketCh: make(chan struct{}),
		statsStopCh:   make(chan struct{}),
	}
	// RFC 3550 §6: best-effort RTCP companion socket on RTP-port + 1.
	// Failure is logged but never fatal — the call still works,
	// peers just won't get our SR/RR.
	s.startRTCP(s.LocalAddr.Port)
	return s, nil
}

// FirstPacket returns a channel that is closed once the first RTP packet is received.
func (s *Session) FirstPacket() <-chan struct{} {
	return s.firstPacketCh
}

func (s *Session) StatsSnapshot() SessionStats {
	if s == nil {
		return SessionStats{}
	}
	return SessionStats{
		LocalSocket: addrStringOrEmpty(s.LocalAddr),
		RemoteSDP:   addrStringOrEmpty(s.sdpRemote),
		RemoteNow:   addrStringOrEmpty(s.RemoteAddr),
		TXPackets:   atomic.LoadUint64(&s.txPackets),
		TXBytes:     atomic.LoadUint64(&s.txBytes),
		RXPackets:   atomic.LoadUint64(&s.rxPackets),
		RXBytes:     atomic.LoadUint64(&s.rxBytes),
		FirstTXAgo:  sinceMillis(atomic.LoadInt64(&s.firstTxUnixNano)),
		FirstRXAgo:  sinceMillis(atomic.LoadInt64(&s.firstRxUnixNano)),
	}
}

// SetRemoteAddr sets the remote RTP address for outgoing packets.
func (s *Session) SetRemoteAddr(addr *net.UDPAddr) {
	s.RemoteAddr = addr
	if s.sdpRemote == nil && addr != nil {
		s.sdpRemote = cloneUDPAddr(addr)
	}
}

// AddMirrorRemote adds a temporary extra RTP destination for outbound packets.
// Useful for NAT/ALG scenarios where real media port differs from SDP offer.
func (s *Session) AddMirrorRemote(addr *net.UDPAddr, ttl time.Duration) {
	if s == nil || addr == nil || addr.IP == nil || addr.Port <= 0 || ttl <= 0 {
		return
	}
	exp := time.Now().Add(ttl)
	cp := cloneUDPAddr(addr)
	s.mirrorMu.Lock()
	defer s.mirrorMu.Unlock()
	// refresh existing mirror target if present
	for i := range s.mirrorRemotes {
		m := s.mirrorRemotes[i]
		if m.addr != nil && m.addr.IP.Equal(cp.IP) && m.addr.Port == cp.Port {
			s.mirrorRemotes[i].expiresAt = exp
			return
		}
	}
	s.mirrorRemotes = append(s.mirrorRemotes, mirrorRemote{
		addr:      cp,
		expiresAt: exp,
	})
}

func cloneUDPAddr(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	b := *a
	return &b
}

func (s *Session) buildPacket(payload []byte, payloadType uint8) *RTPPacket {
	return &RTPPacket{
		Header: RTPHeader{
			Version:        2,
			Padding:        false,
			Extension:      false,
			CSRCCount:      0,
			Marker:         false,
			PayloadType:    payloadType,
			SequenceNumber: s.SeqNum,
			Timestamp:      s.Timestamp,
			SSRC:           s.SSRC,
		},
		Payload: payload,
	}
}

func (s *Session) updateAfterSend(samples uint32) {
	s.SeqNum++
	// RTP timestamp is measured in units of the codec's sampling clock.
	s.Timestamp += samples
}

// SendRTP sends one RTP packet.
//
// `samples` is the number of audio samples represented by `payload` at the RTP clock rate.
// For PCM-based codecs, this should match the negotiated codec frame duration.
func (s *Session) SendRTP(payload []byte, payloadType uint8, samples uint32) error {
	if s == nil {
		return fmt.Errorf("rtp: nil session")
	}
	if atomic.LoadUint32(&s.closed) != 0 {
		return fmt.Errorf("rtp: session closed")
	}
	if s.Conn == nil {
		return fmt.Errorf("rtp: nil udp conn")
	}
	if s.RemoteAddr == nil {
		return fmt.Errorf("rtp: remote address not set")
	}

	pkt := s.buildPacket(payload, payloadType)
	data, err := pkt.Marshal()
	if err != nil {
		return fmt.Errorf("rtp: marshal: %w", err)
	}

	out := data
	if s.srtpEncrypt != nil {
		s.srtpMu.Lock()
		enc, encErr := s.srtpEncrypt.EncryptRTP(nil, data, nil)
		s.srtpMu.Unlock()
		if encErr != nil {
			return fmt.Errorf("rtp: srtp encrypt: %w", encErr)
		}
		out = enc
	}

	if _, err := s.Conn.WriteToUDP(out, s.RemoteAddr); err != nil {
		return fmt.Errorf("rtp: send: %w", err)
	}
	s.sendMirrorRTP(out, s.RemoteAddr)
	atomic.AddUint64(&s.txPackets, 1)
	atomic.AddUint64(&s.txBytes, uint64(len(payload)))
	// Latch the RTP timestamp we just sent for the next SR.RTPTime.
	atomic.StoreUint32(&s.rtcpLastSentRTPTS, s.Timestamp)
	nowUnix := time.Now().UnixNano()
	_ = atomic.CompareAndSwapInt64(&s.firstTxUnixNano, 0, nowUnix)
	s.startStatsLoop()

	s.logFirstTX.Do(func() {
		siplog.L.Info("rtp first outbound packet (diagnostics)")
	})

	s.updateAfterSend(samples)
	return nil
}

// ReceiveRTP reads a UDP datagram and parses it into an RTPPacket.
//
// It also opportunistically "learns" remote address (symmetric RTP behavior).
func (s *Session) ReceiveRTP(buffer []byte) (n int, from *net.UDPAddr, packet *RTPPacket, err error) {
	if s == nil {
		return 0, nil, nil, fmt.Errorf("rtp: nil session")
	}
	if atomic.LoadUint32(&s.closed) != 0 {
		return 0, nil, nil, fmt.Errorf("rtp: session closed")
	}
	if s.Conn == nil {
		return 0, nil, nil, fmt.Errorf("rtp: nil udp conn")
	}

	n, addr, err := s.Conn.ReadFromUDP(buffer)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("rtp: read udp: %w", err)
	}
	atomic.AddUint64(&s.rxPackets, 1)
	atomic.AddUint64(&s.rxBytes, uint64(n))
	_ = atomic.CompareAndSwapInt64(&s.firstRxUnixNano, 0, time.Now().UnixNano())
	s.startStatsLoop()

	s.logFirstUDP.Do(func() {
		siplog.L.Info("rtp first udp datagram on media socket (diagnostics)")
	})

	s.firstPacketOnce.Do(func() {
		close(s.firstPacketCh)
	})

	before := s.RemoteAddr
	if s.RemoteAddr == nil || !s.RemoteAddr.IP.Equal(addr.IP) || s.RemoteAddr.Port != addr.Port {
		s.RemoteAddr = addr
		if before != nil && (before.IP.String() != addr.IP.String() || before.Port != addr.Port) {
			siplog.L.WithFields(map[string]interface{}{
				"learned_remote": addr.String(),
				"sdp_remote": func() string {
					if s.sdpRemote != nil {
						return s.sdpRemote.String()
					}
					return ""
				}(),
			}).Info("rtp symmetric path: send target updated (NAT)")
		}
	}

	// RFC 5764 §5.1.2 demux: if there's an active DTLS handshake on
	// this socket, route packets typed as DTLS (first byte 20-63) to
	// it. RTP packets (128-191) continue down the existing path.
	if n >= 1 && IsDTLSPacket(buffer[0]) {
		if s.routeDTLSPacket(buffer[:n]) {
			return n, addr, nil, ErrRTPDiscard
		}
		// Stray DTLS packet (handshake already finished or never
		// started) — drop quietly.
		return n, addr, nil, ErrRTPDiscard
	}

	work := buffer[:n]
	if s.srtpDecrypt != nil {
		s.srtpMu.Lock()
		plain, derr := s.srtpDecrypt.DecryptRTP(nil, work, nil)
		s.srtpMu.Unlock()
		if derr != nil {
			return n, addr, nil, ErrRTPDiscard
		}
		work = plain
	}

	pkt := &RTPPacket{}
	if err := pkt.Unmarshal(work); err != nil {
		return n, addr, nil, fmt.Errorf("rtp: unmarshal: %w", err)
	}

	if pkt.Header.Version != 2 {
		return n, addr, nil, ErrRTPDiscard
	}

	ssrc := pkt.Header.SSRC
	for {
		if atomic.LoadUint32(&s.rxSSRCSeen) == 0 {
			if atomic.CompareAndSwapUint32(&s.rxSSRCSeen, 0, 1) {
				atomic.StoreUint32(&s.rxSSRC, ssrc)
				break
			}
			continue
		}
		if atomic.LoadUint32(&s.rxSSRC) != ssrc {
			return n, addr, nil, ErrRTPDiscard
		}
		break
	}

	// Feed the RTCP receiver-stats machine. RFC 3550 §A.1 + §A.8
	// require running this on every accepted RTP packet so the
	// jitter EWMA and seq-cycle tracking stay current.
	s.recordIncomingRTPForRTCP(pkt.Header.SequenceNumber, pkt.Header.Timestamp, ssrc)

	return n, addr, pkt, nil
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.closeOnce.Do(func() {
		atomic.StoreUint32(&s.closed, 1)
		if s.statsStopCh != nil {
			close(s.statsStopCh)
		}
		// Stop RTCP BEFORE closing the RTP socket so the RTCP loop
		// doesn't try to read from a freed FD.
		s.stopRTCP()
		if s.Conn != nil {
			err = s.Conn.Close()
		}
	})
	return err
}

func (s *Session) sendMirrorRTP(data []byte, primary *net.UDPAddr) {
	if s == nil || s.Conn == nil || len(data) == 0 {
		return
	}
	now := time.Now()
	s.mirrorMu.Lock()
	if len(s.mirrorRemotes) == 0 {
		s.mirrorMu.Unlock()
		return
	}
	live := s.mirrorRemotes[:0]
	for _, m := range s.mirrorRemotes {
		if m.addr == nil || m.addr.IP == nil || m.addr.Port <= 0 || !m.expiresAt.After(now) {
			continue
		}
		live = append(live, m)
	}
	s.mirrorRemotes = live
	remotes := make([]*net.UDPAddr, 0, len(live))
	for _, m := range live {
		remotes = append(remotes, cloneUDPAddr(m.addr))
	}
	s.mirrorMu.Unlock()

	for _, r := range remotes {
		if r == nil {
			continue
		}
		if primary != nil && primary.IP != nil && primary.IP.Equal(r.IP) && primary.Port == r.Port {
			continue
		}
		if _, werr := s.Conn.WriteToUDP(data, r); werr != nil {
			prev := atomic.LoadInt64(&s.mirrorErrLastLogNs)
			now := time.Now().UnixNano()
			if now-prev > int64(5*time.Second) {
				if atomic.CompareAndSwapInt64(&s.mirrorErrLastLogNs, prev, now) {
					siplog.L.WithError(werr).Warn("rtp mirror write failed")
				}
			}
		}
	}
}

func (s *Session) startStatsLoop() {
	if s == nil {
		return
	}
	s.logStatsOnce.Do(func() {
		go s.statsLoop()
	})
}

func (s *Session) statsLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.statsStopCh:
			return
		case <-ticker.C:
			txP := atomic.LoadUint64(&s.txPackets)
			rxP := atomic.LoadUint64(&s.rxPackets)
			if txP == 0 && rxP == 0 {
				continue
			}
			firstTx := atomic.LoadInt64(&s.firstTxUnixNano)
			if txP > 0 && rxP == 0 && firstTx > 0 && time.Since(time.Unix(0, firstTx)) >= 10*time.Second {
				if atomic.CompareAndSwapUint32(&s.natWarned, 0, 1) {
					siplog.L.Warn("rtp nat suspected: outbound active but inbound silent")
				}
			}
		}
	}
}

func sinceMillis(unixNano int64) int64 {
	if unixNano <= 0 {
		return -1
	}
	return time.Since(time.Unix(0, unixNano)).Milliseconds()
}

func addrStringOrEmpty(a *net.UDPAddr) string {
	if a == nil {
		return ""
	}
	return a.String()
}
