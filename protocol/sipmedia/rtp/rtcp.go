// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package rtp

// RFC 3550 §6 RTCP — Sender Reports, Receiver Reports, SDES.
//
// What this file ships:
//
//   - A separate UDP socket bound on RTP-port + 1 to receive RTCP
//     (matches the `a=rtcp:<port+1>` line we already advertise in
//     the SDP offer/answer; see pkg/sip/sdp/sdp.go). RTCP-MUX (RFC
//     5761) is NOT implemented yet — it's a follow-up.
//   - A periodic transmitter that fires a compound packet
//     [SR | RR] + SDES(CNAME) every ~5s (RFC 3550 §6.2 default,
//     with the §6.3 randomisation factor of [0.5, 1.5]).
//   - Per-incoming-stream receive bookkeeping for the canonical
//     stats: cumulative loss, fraction-lost, interarrival jitter
//     (RFC 3550 §A.8), and last-SR timestamp + arrival time for
//     RTT computation via the next outgoing RR.
//   - RTT and peer-reported jitter / loss are surfaced via
//     SessionStats so call records can show MOS-relevant metrics.
//
// Why not pion/interceptor: this stack runs on a custom Session +
// raw UDP socket; pion/interceptor expects pion/webrtc primitives.
// Hand-rolling the small slice of RFC 3550 we need keeps the dep
// surface flat and avoids a webrtc.PeerConnection-shaped abstraction
// layer just to send 4-5 packets per minute.

import (
	"github.com/LingByte/lingllm/protocol/sipmedia/internal/siplog"
	"errors"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
)

const (
	// rtcpInterval is the deterministic minimum between scheduled
	// compound RTCP packets. RFC 3550 §6.2 specifies 5s as the
	// minimum interval before randomisation; we use it as the base.
	rtcpInterval = 5 * time.Second

	// rtcpJitterClockRateDefault is the RTP clock rate we assume
	// when SetClockRate hasn't been called. PCMA / PCMU / G.722 (the
	// codecs we actually negotiate against PSTN gear) are all 8000 Hz.
	// Opus runs at 48000 Hz; callers should SetClockRate(48000) for
	// accurate jitter accounting on those legs.
	rtcpJitterClockRateDefault = uint32(8000)

	// ntpEpochOffsetSeconds = seconds between 1900-01-01 (NTP epoch)
	// and 1970-01-01 (Unix epoch). RFC 3550 §6.4.1.
	ntpEpochOffsetSeconds uint64 = 2208988800
)

// rtcpReceiverStats tracks the state needed to fill out one RR
// report block for the single remote SSRC we listen to.
//
// Concurrency: ReceiveRTP and the RTCP send loop both touch this.
// All fields go through `mu`; the hot path (per-RTP-packet update)
// holds the mutex only for a few microseconds of arithmetic.
type rtcpReceiverStats struct {
	mu sync.Mutex

	clockRate uint32 // codec-dependent; SetClockRate to override

	initialised bool
	ssrc        uint32

	// Sequence number tracking (RFC 3550 §A.1).
	baseSeq      uint32 // first ext-seq we saw
	maxSeq       uint16 // highest ext-seq number observed (low 16)
	cycles       uint32 // shifted up by 16 per wrap
	packetsRecv  uint32

	// Loss snapshot for fraction-lost (RFC 3550 §A.3).
	expectedPrior uint32
	receivedPrior uint32

	// Jitter (RFC 3550 §A.8) — kept as Q24.8 fixed point.
	transit int64
	jitter  uint32

	// Last SR for RTT (RFC 3550 §6.4.1).
	lastSRMidNTP    uint32
	lastSRArrivalNs int64
}

// rtcpSenderEcho captures the most recent RR we received from peer
// about *our* outgoing stream. RTT is computed in handleIncoming.
type rtcpSenderEcho struct {
	mu sync.Mutex

	seenRR        bool
	rttMs         uint32
	jitter        uint32 // peer-reported jitter on our stream
	lossFraction  uint8  // peer-reported fraction lost (Q0.8)
	cumulativeLost uint32
}

// startRTCP binds the companion RTCP socket (RTP port + 1) and spins
// up the receive + scheduled-send goroutines. Best-effort: if the
// port-bind fails (peer ephemeral / NAT collision) we log and run
// the call without RTCP rather than fail the dial — RTCP is purely
// observational on a well-behaving call.
func (s *Session) startRTCP(rtpLocalPort int) {
	if s == nil || s.LocalAddr == nil {
		return
	}
	port := rtpLocalPort
	if port <= 0 {
		port = s.LocalAddr.Port
	}
	if port <= 0 || port >= 65535 {
		return
	}
	rtcpAddr := &net.UDPAddr{IP: s.LocalAddr.IP, Port: port + 1}
	conn, err := net.ListenUDP("udp4", rtcpAddr)
	if err != nil {
		// Couldn't grab the +1 port — likely it's already in use by a
		// concurrent leg. Skip RTCP, keep the call. We deliberately
		// don't try a second random port: peers send RTCP to the port
		// from our SDP `a=rtcp:` line, which assumes port+1.
		siplog.L.Warn("rtp/rtcp companion socket bind failed; running without RTCP")
		return
	}
	s.rtcpMu.Lock()
	s.rtcpConn = conn
	s.rtcpStopCh = make(chan struct{})
	if s.rxStats.clockRate == 0 {
		s.rxStats.clockRate = rtcpJitterClockRateDefault
	}
	s.rtcpMu.Unlock()
	go s.rtcpReceiveLoop(conn)
	go s.rtcpSendLoop(conn)
}

// stopRTCP closes the companion socket and stops both goroutines.
// Idempotent. Called from Session.Close.
func (s *Session) stopRTCP() {
	if s == nil {
		return
	}
	s.rtcpMu.Lock()
	conn := s.rtcpConn
	stopCh := s.rtcpStopCh
	s.rtcpConn = nil
	s.rtcpStopCh = nil
	s.rtcpMu.Unlock()
	if stopCh != nil {
		select {
		case <-stopCh:
			// already closed
		default:
			close(stopCh)
		}
	}
	if conn != nil {
		_ = conn.Close()
	}
}

// SetRTCPClockRate sets the codec clock rate used for jitter
// computation. Default is 8000; call this with 48000 for Opus, or
// the actual negotiated rate otherwise.
func (s *Session) SetRTCPClockRate(hz uint32) {
	if s == nil || hz == 0 {
		return
	}
	s.rxStats.mu.Lock()
	s.rxStats.clockRate = hz
	s.rxStats.mu.Unlock()
}

// recordIncomingRTPForRTCP is called from ReceiveRTP for every
// successfully-decoded packet. It feeds the receiver-stats machine
// (loss, jitter, sequence cycles) per RFC 3550 §A.1 + §A.8.
//
// arrivalRTPClock = arrival time expressed in RTP clock units.
func (s *Session) recordIncomingRTPForRTCP(seq uint16, ts uint32, ssrc uint32) {
	if s == nil {
		return
	}
	rs := &s.rxStats
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.initialised {
		rs.initialised = true
		rs.ssrc = ssrc
		rs.baseSeq = uint32(seq)
		rs.maxSeq = seq
		rs.cycles = 0
		rs.packetsRecv = 1
		rs.transit = 0
		rs.jitter = 0
		return
	}
	if ssrc != rs.ssrc {
		// A different SSRC mid-stream usually means a media reset
		// (re-INVITE / transfer). Re-initialise so jitter doesn't
		// blow up on the inevitable timestamp discontinuity.
		rs.ssrc = ssrc
		rs.baseSeq = uint32(seq)
		rs.maxSeq = seq
		rs.cycles = 0
		rs.packetsRecv = 1
		rs.transit = 0
		rs.jitter = 0
		return
	}

	// Sequence wrap detection (RFC 3550 §A.1, simplified — full impl
	// has out-of-order tolerance; we approximate by checking deltas).
	if seq < rs.maxSeq && uint32(rs.maxSeq)-uint32(seq) > 0x8000 {
		rs.cycles += 1 << 16
	}
	if seq > rs.maxSeq || (seq < rs.maxSeq && uint32(rs.maxSeq)-uint32(seq) > 0x8000) {
		rs.maxSeq = seq
	}
	rs.packetsRecv++

	// Jitter (RFC 3550 §A.8). All math in RTP clock units → Q24.8
	// fixed-point average of |D|.
	if rs.clockRate == 0 {
		rs.clockRate = rtcpJitterClockRateDefault
	}
	arrivalRTP := uint32(time.Now().UnixNano()/int64(time.Second/time.Duration(rs.clockRate))) // arrival in RTP clock
	// transit = arrival_in_rtp_units - rtp_timestamp (signed)
	transit := int64(arrivalRTP) - int64(ts)
	if rs.transit != 0 {
		d := transit - rs.transit
		if d < 0 {
			d = -d
		}
		// jitter += (|D| - jitter) / 16  (running average)
		// Use int64 for headroom; jitter is uint32 in the wire format.
		j := int64(rs.jitter)
		j += (d - j) >> 4
		if j < 0 {
			j = 0
		}
		rs.jitter = uint32(j)
	}
	rs.transit = transit
}

// extendedHighestSeq returns the cycles<<16 | maxSeq value for RR.
func (rs *rtcpReceiverStats) extendedHighestSeq() uint32 {
	return rs.cycles | uint32(rs.maxSeq)
}

// snapshotForRR pulls the current RR fields atomically and updates
// the prior-counters so the NEXT RR's fraction-lost is delta-based
// (RFC 3550 §A.3).
func (rs *rtcpReceiverStats) snapshotForRR() (block rtcp.ReceptionReport, hasData bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.initialised || rs.packetsRecv == 0 {
		return rtcp.ReceptionReport{}, false
	}
	extHighest := rs.extendedHighestSeq()
	expected := extHighest - rs.baseSeq + 1
	lost := uint32(0)
	if expected > rs.packetsRecv {
		lost = expected - rs.packetsRecv
	}
	expectedInterval := expected - rs.expectedPrior
	receivedInterval := rs.packetsRecv - rs.receivedPrior
	rs.expectedPrior = expected
	rs.receivedPrior = rs.packetsRecv
	var fraction uint8
	if expectedInterval != 0 && expectedInterval >= receivedInterval {
		lostInterval := expectedInterval - receivedInterval
		// fraction = (lost_interval << 8) / expected_interval — clamps
		// to 0..255. RFC 3550 §A.3.
		fraction = uint8((lostInterval << 8) / expectedInterval)
	}

	// DLSR = (now - lastSRArrival) in 1/65536 sec units (RFC 3550 §6.4.1).
	var dlsr uint32
	if rs.lastSRArrivalNs != 0 {
		elapsedNs := time.Now().UnixNano() - rs.lastSRArrivalNs
		if elapsedNs > 0 {
			// 65536 / 1e9 = ~6.5536e-5; multiply then divide.
			dlsr = uint32(uint64(elapsedNs) * 65536 / uint64(time.Second))
		}
	}

	return rtcp.ReceptionReport{
		SSRC:               rs.ssrc,
		FractionLost:       fraction,
		TotalLost:          lost & 0x00FFFFFF, // 24-bit
		LastSequenceNumber: extHighest,
		Jitter:             rs.jitter,
		LastSenderReport:   rs.lastSRMidNTP,
		Delay:              dlsr,
	}, true
}

// rtcpSendLoop emits a compound RTCP packet at randomised intervals
// per RFC 3550 §6.3.1 — the [0.5, 1.5] × interval factor reduces
// "packet trains" when multiple endpoints come up at once.
func (s *Session) rtcpSendLoop(conn *net.UDPConn) {
	// Initial randomised wait so we don't synchronise with peer's
	// schedule; RFC 3550 §6.2 recommends starting at half the
	// computed interval, then full interval thereafter.
	rng := rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(s.SSRC)))
	wait := rtcpInterval/2 + time.Duration(rng.Int63n(int64(rtcpInterval/2)))
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		s.rtcpMu.Lock()
		stopCh := s.rtcpStopCh
		s.rtcpMu.Unlock()
		if stopCh == nil {
			return
		}
		select {
		case <-stopCh:
			return
		case <-timer.C:
		}
		s.sendOneRTCP(conn)
		// Reset with the §6.3.1 randomisation factor.
		factor := 0.5 + rng.Float64() // [0.5, 1.5)
		next := time.Duration(float64(rtcpInterval) * factor)
		timer.Reset(next)
	}
}

// sendOneRTCP builds a compound packet:
//
//	[SR or RR] + SDES(CNAME)
//
// The choice between SR and RR depends on whether we've sent any
// RTP since the call started: an RTP-receiver-only endpoint MUST
// emit RR per RFC 3550 §6.4.2.
func (s *Session) sendOneRTCP(conn *net.UDPConn) {
	if s == nil || conn == nil {
		return
	}
	remote := s.rtcpRemoteAddr()
	if remote == nil {
		return
	}

	var pkts []rtcp.Packet

	txPackets := atomic.LoadUint64(&s.txPackets)
	if txPackets > 0 {
		pkts = append(pkts, s.buildSR())
	} else {
		pkts = append(pkts, s.buildRR())
	}
	pkts = append(pkts, s.buildSDES())

	raw, err := rtcp.Marshal(pkts)
	if err != nil {
		siplog.L.WithError(err).Warn("rtcp marshal failed")
		return
	}
	if _, err := conn.WriteToUDP(raw, remote); err != nil {
		if !errors.Is(err, net.ErrClosed) {
			s.rtcpTxWarnOnce.Do(func() {
				siplog.L.WithError(err).Warn("rtcp send failed")
			})
		}
	}
}

// rtcpRemoteAddr derives the peer's RTCP address. We prefer the
// RemoteAddr we've learned (post symmetric-RTP NAT learning) and
// shift to port+1, matching the SDP convention.
func (s *Session) rtcpRemoteAddr() *net.UDPAddr {
	if s == nil {
		return nil
	}
	src := s.RemoteAddr
	if src == nil {
		src = s.sdpRemote
	}
	if src == nil || src.IP == nil || src.Port <= 0 {
		return nil
	}
	return &net.UDPAddr{IP: src.IP, Port: src.Port + 1}
}

// buildSR constructs the Sender Report half of a compound packet.
// We populate ReceiverReports too when we have stats on the inbound
// stream (RFC 3550 §6.4.1 allows up to 31 blocks).
func (s *Session) buildSR() *rtcp.SenderReport {
	now := time.Now()
	ntp := unixToNTP(now)
	rtpTS := atomic.LoadUint32(&s.rtcpLastSentRTPTS)
	if rtpTS == 0 {
		// Approximate from current send timestamp; harmless if zero.
		rtpTS = s.Timestamp
	}
	rep := &rtcp.SenderReport{
		SSRC:        s.SSRC,
		NTPTime:     ntp,
		RTPTime:     rtpTS,
		PacketCount: uint32(atomic.LoadUint64(&s.txPackets)),
		OctetCount:  uint32(atomic.LoadUint64(&s.txBytes)),
	}
	if rb, ok := s.rxStats.snapshotForRR(); ok {
		rep.Reports = []rtcp.ReceptionReport{rb}
	}
	return rep
}

// buildRR is the receiver-only counterpart for legs that haven't
// transmitted RTP yet (e.g. one-way IVR prompt that's still buffering).
func (s *Session) buildRR() *rtcp.ReceiverReport {
	rep := &rtcp.ReceiverReport{SSRC: s.SSRC}
	if rb, ok := s.rxStats.snapshotForRR(); ok {
		rep.Reports = []rtcp.ReceptionReport{rb}
	}
	return rep
}

// buildSDES emits the mandatory CNAME chunk per RFC 3550 §6.5.1.
// CNAME ought to be a stable, unique identifier; we use SSRC@host
// which satisfies "persistent across the session" without leaking
// PII.
func (s *Session) buildSDES() *rtcp.SourceDescription {
	host := "lingbyte"
	if s.LocalAddr != nil && s.LocalAddr.IP != nil {
		host = s.LocalAddr.IP.String()
	}
	cname := []byte("ssrc-")
	cname = append(cname, []byte(uint32Hex(s.SSRC))...)
	cname = append(cname, '@')
	cname = append(cname, []byte(host)...)
	return &rtcp.SourceDescription{
		Chunks: []rtcp.SourceDescriptionChunk{{
			Source: s.SSRC,
			Items: []rtcp.SourceDescriptionItem{{
				Type: rtcp.SDESCNAME,
				Text: string(cname),
			}},
		}},
	}
}

func uint32Hex(v uint32) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = hex[v&0xF]
		v >>= 4
	}
	return string(out)
}

// rtcpReceiveLoop reads RTCP packets and feeds RR-driven stats
// (RTT, peer-perceived jitter / loss) into rtcp_sender_echo.
func (s *Session) rtcpReceiveLoop(conn *net.UDPConn) {
	buf := make([]byte, 1500)
	for {
		s.rtcpMu.Lock()
		stopCh := s.rtcpStopCh
		s.rtcpMu.Unlock()
		if stopCh == nil {
			return
		}
		select {
		case <-stopCh:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Read deadline expirations are normal; ignore.
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if n == 0 {
			continue
		}
		pkts, perr := rtcp.Unmarshal(buf[:n])
		if perr != nil {
			continue
		}
		s.handleIncomingRTCP(pkts)
	}
}

// handleIncomingRTCP processes the unmarshalled compound packet.
func (s *Session) handleIncomingRTCP(pkts []rtcp.Packet) {
	for _, p := range pkts {
		switch v := p.(type) {
		case *rtcp.SenderReport:
			// Cache the middle 32 bits of NTP for our next RR's LSR
			// field, plus arrival time for DLSR (RFC 3550 §6.4.1).
			midNTP := uint32((uint64(v.NTPTime) >> 16) & 0xFFFFFFFF)
			s.rxStats.mu.Lock()
			s.rxStats.lastSRMidNTP = midNTP
			s.rxStats.lastSRArrivalNs = time.Now().UnixNano()
			s.rxStats.mu.Unlock()
			// SR also contains RR blocks about *our* sending stream.
			s.processReportBlocks(v.Reports)
		case *rtcp.ReceiverReport:
			s.processReportBlocks(v.Reports)
		}
	}
}

// processReportBlocks looks for blocks describing our outgoing SSRC
// and computes RTT from LSR/DLSR (RFC 3550 §6.4.1 last paragraph).
func (s *Session) processReportBlocks(blocks []rtcp.ReceptionReport) {
	for _, b := range blocks {
		if b.SSRC != s.SSRC {
			continue
		}
		s.txEcho.mu.Lock()
		s.txEcho.seenRR = true
		s.txEcho.jitter = b.Jitter
		s.txEcho.lossFraction = b.FractionLost
		s.txEcho.cumulativeLost = b.TotalLost
		// RTT = now - LSR - DLSR, all in 1/65536 sec units.
		if b.LastSenderReport != 0 {
			nowMid := uint32((uint64(unixToNTP(time.Now())) >> 16) & 0xFFFFFFFF)
			rtt32 := nowMid - b.LastSenderReport - b.Delay
			rttMs := uint32(uint64(rtt32) * 1000 / 65536)
			s.txEcho.rttMs = rttMs
		}
		s.txEcho.mu.Unlock()
	}
}

// RTCPStats is the read-only snapshot exposed via SessionStats.
type RTCPStats struct {
	// PeerSeenRR tells callers whether our peer has emitted at least
	// one RR about us; useful to distinguish "no RTCP support" from
	// "no observed loss yet".
	PeerSeenRR bool
	// RTTMs is the round-trip time computed from the latest peer RR.
	RTTMs uint32
	// PeerJitter (in RTP clock units) as reported by peer's RR.
	PeerJitter uint32
	// PeerLossFraction is fraction-lost from peer's last RR (Q0.8).
	PeerLossFraction uint8
	// PeerCumulativeLost — peer's running total of dropped pkts of ours.
	PeerCumulativeLost uint32
	// LocalJitter is what we measured on the inbound stream.
	LocalJitter uint32
	// LocalPacketsRecv — RTP packets we accepted.
	LocalPacketsRecv uint32
}

// RTCPSnapshot returns the RTCP-derived metrics. Nil-safe.
func (s *Session) RTCPSnapshot() RTCPStats {
	if s == nil {
		return RTCPStats{}
	}
	out := RTCPStats{}
	s.txEcho.mu.Lock()
	out.PeerSeenRR = s.txEcho.seenRR
	out.RTTMs = s.txEcho.rttMs
	out.PeerJitter = s.txEcho.jitter
	out.PeerLossFraction = s.txEcho.lossFraction
	out.PeerCumulativeLost = s.txEcho.cumulativeLost
	s.txEcho.mu.Unlock()
	s.rxStats.mu.Lock()
	out.LocalJitter = s.rxStats.jitter
	out.LocalPacketsRecv = s.rxStats.packetsRecv
	s.rxStats.mu.Unlock()
	return out
}

// unixToNTP converts a wall-clock time to the 64-bit NTP timestamp
// format used by RTCP SR (RFC 3550 §4 / §6.4.1).
func unixToNTP(t time.Time) uint64 {
	secs := uint64(t.Unix()) + ntpEpochOffsetSeconds
	frac := uint64(t.Nanosecond()) << 32 / uint64(time.Second)
	return (secs << 32) | (frac & 0xFFFFFFFF)
}
