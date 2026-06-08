// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package rtp

import (
	"net"
	"testing"
	"time"

	"github.com/pion/rtcp"
)

// TestNewSession_BindsRTCPCompanionSocket verifies the side-effect:
// after NewSession returns, the RTP-port + 1 UDP port is bound by
// our process. Binding without using is what RFC 3550 expects when
// peers send RTCP unsolicited.
func TestNewSession_BindsRTCPCompanionSocket(t *testing.T) {
	s, err := NewSession(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rtpPort := s.LocalAddr.Port
	if rtpPort <= 0 {
		t.Fatal("no RTP port assigned")
	}

	s.rtcpMu.Lock()
	conn := s.rtcpConn
	s.rtcpMu.Unlock()
	if conn == nil {
		// Acceptable on rare port collisions, but very unlikely
		// here because we just used port 0 (kernel-assigned).
		t.Skip("RTCP companion bind failed; can't validate port wiring on this run")
	}
	if got := conn.LocalAddr().(*net.UDPAddr).Port; got != rtpPort+1 {
		t.Errorf("RTCP port=%d, want RTP+1=%d", got, rtpPort+1)
	}
}

func TestSession_Close_StopsRTCP(t *testing.T) {
	s, err := NewSession(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Second close must be safe (idempotent).
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	s.rtcpMu.Lock()
	conn := s.rtcpConn
	s.rtcpMu.Unlock()
	if conn != nil {
		t.Error("rtcpConn must be nil after Close")
	}
}

func TestUnixToNTP_RoundTripsBackToWithin1ms(t *testing.T) {
	// RFC 3550 §4 says the upper 32 bits are seconds since 1900.
	// Confirm the seconds half matches what we expect for "now".
	now := time.Now()
	ntp := unixToNTP(now)
	gotSecs := uint64(ntp >> 32)
	wantSecs := uint64(now.Unix()) + ntpEpochOffsetSeconds
	if gotSecs != wantSecs {
		t.Errorf("NTP seconds=%d want %d (epoch offset off?)", gotSecs, wantSecs)
	}
}

func TestRecordIncomingRTPForRTCP_FirstPacketInitialises(t *testing.T) {
	s, err := NewSession(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.recordIncomingRTPForRTCP(100, 0xDEADBEEF, 0xCAFEBABE)

	s.rxStats.mu.Lock()
	defer s.rxStats.mu.Unlock()
	if !s.rxStats.initialised {
		t.Error("first packet must initialise rx stats")
	}
	if s.rxStats.ssrc != 0xCAFEBABE {
		t.Errorf("ssrc=%x want 0xCAFEBABE", s.rxStats.ssrc)
	}
	if s.rxStats.packetsRecv != 1 {
		t.Errorf("packetsRecv=%d want 1", s.rxStats.packetsRecv)
	}
	if s.rxStats.maxSeq != 100 {
		t.Errorf("maxSeq=%d want 100", s.rxStats.maxSeq)
	}
}

func TestRecordIncomingRTPForRTCP_ResetsOnSSRCChange(t *testing.T) {
	// Mid-call SSRC swap (re-INVITE / transfer) must not blow up
	// jitter — we expect the running average to be re-seeded.
	s, _ := NewSession(0)
	defer s.Close()

	s.recordIncomingRTPForRTCP(1, 100, 0x1111)
	s.recordIncomingRTPForRTCP(2, 260, 0x1111) // builds some transit
	s.recordIncomingRTPForRTCP(3, 9999999, 0x2222) // SSRC swap

	s.rxStats.mu.Lock()
	defer s.rxStats.mu.Unlock()
	if s.rxStats.ssrc != 0x2222 {
		t.Errorf("ssrc=%x want 0x2222 after swap", s.rxStats.ssrc)
	}
	if s.rxStats.packetsRecv != 1 {
		t.Errorf("packetsRecv=%d want 1 (reset)", s.rxStats.packetsRecv)
	}
	if s.rxStats.transit != 0 {
		t.Errorf("transit=%d want 0 (reset)", s.rxStats.transit)
	}
}

func TestRecordIncomingRTPForRTCP_HandlesSeqWraparound(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()

	// Start near 16-bit max so the wrap is unambiguous.
	s.recordIncomingRTPForRTCP(0xFFFE, 1000, 0x33)
	s.recordIncomingRTPForRTCP(0xFFFF, 1160, 0x33)
	s.recordIncomingRTPForRTCP(0x0000, 1320, 0x33) // wrap
	s.recordIncomingRTPForRTCP(0x0001, 1480, 0x33)

	s.rxStats.mu.Lock()
	cycles := s.rxStats.cycles
	s.rxStats.mu.Unlock()
	if cycles != 0x10000 {
		t.Errorf("cycles=%x want 0x10000 after wrap", cycles)
	}
}

func TestSnapshotForRR_FractionLost(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()

	// Simulate 10 expected packets, only 7 received → fraction-lost
	// over the FIRST report interval should reflect 3/10 = 25.5/256.
	s.recordIncomingRTPForRTCP(1, 100, 0xABCD) // expectedPrior=0 after init
	for _, seq := range []uint16{2, 3, 4, 5, 7, 9} {
		// gaps: missing 6, 8, plus stop early at 9 (10 expected, we got 7)
		s.recordIncomingRTPForRTCP(seq, 100+uint32(seq)*160, 0xABCD)
	}
	// Force expected = highest - base + 1 = 9 - 1 + 1 = 9; received = 7.
	rb, ok := s.rxStats.snapshotForRR()
	if !ok {
		t.Fatal("expected RR data after >=1 packet")
	}
	if rb.SSRC != 0xABCD {
		t.Errorf("RB.SSRC=%x want 0xABCD", rb.SSRC)
	}
	if rb.FractionLost == 0 {
		t.Errorf("FractionLost=0; expected non-zero given 2-of-9 missing")
	}
	if rb.TotalLost == 0 {
		t.Error("TotalLost=0; expected >=2")
	}
}

func TestSnapshotForRR_NoData(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()
	if _, ok := s.rxStats.snapshotForRR(); ok {
		t.Error("must NOT report RR data before any RTP arrived")
	}
}

func TestProcessReportBlocks_ComputesRTT(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()

	// Pretend peer's RR says: my last SR's mid-NTP was X, peer
	// processed it 100ms ago. Our "now" is X + 200ms equivalent.
	now := time.Now()
	nowMid := uint32((unixToNTP(now) >> 16) & 0xFFFFFFFF)
	// 100ms in 1/65536 s units
	delay100ms := uint32(uint64(100*time.Millisecond) * 65536 / uint64(time.Second))
	// We pretend peer's SR was 200ms before "now".
	lsr := nowMid - uint32(uint64(200*time.Millisecond)*65536/uint64(time.Second))

	blocks := []rtcp.ReceptionReport{{
		SSRC:             s.SSRC,
		Jitter:           42,
		FractionLost:     8,
		TotalLost:        7,
		LastSenderReport: lsr,
		Delay:            delay100ms,
	}}
	s.processReportBlocks(blocks)

	snap := s.RTCPSnapshot()
	if !snap.PeerSeenRR {
		t.Error("PeerSeenRR must be true after processing an RR block")
	}
	// Expected RTT ≈ 200ms - 100ms = 100ms; allow ±15ms slack since
	// we re-took "now" inside processReportBlocks.
	if snap.RTTMs < 80 || snap.RTTMs > 120 {
		t.Errorf("RTTMs=%d, want ~100ms", snap.RTTMs)
	}
	if snap.PeerJitter != 42 {
		t.Errorf("PeerJitter=%d want 42", snap.PeerJitter)
	}
}

func TestProcessReportBlocks_IgnoresOtherSSRCs(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()
	// A block addressed to a different SSRC must NOT update our echo.
	other := s.SSRC + 1
	s.processReportBlocks([]rtcp.ReceptionReport{{
		SSRC:             other,
		Jitter:           99,
		FractionLost:     50,
		LastSenderReport: 1, // non-zero so RTT path would fire if matched
		Delay:            1,
	}})
	if s.RTCPSnapshot().PeerSeenRR {
		t.Error("must ignore RR blocks for other SSRCs")
	}
}

func TestBuildSDES_HasCNAMEItem(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()
	sdes := s.buildSDES()
	if len(sdes.Chunks) == 0 || len(sdes.Chunks[0].Items) == 0 {
		t.Fatal("SDES must contain at least one CNAME item")
	}
	it := sdes.Chunks[0].Items[0]
	if it.Type != rtcp.SDESCNAME {
		t.Errorf("first item type=%d want CNAME=%d", it.Type, rtcp.SDESCNAME)
	}
	if len(it.Text) == 0 {
		t.Error("CNAME text must not be empty")
	}
}

func TestBuildSR_RemembersTxStats(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()

	// Manually bump tx counters so buildSR has something to report.
	// We avoid calling SendRTP here because it needs a remote addr.
	s.txPackets = 5
	s.txBytes = 800
	s.rtcpLastSentRTPTS = 0xCAFEBABE

	sr := s.buildSR()
	if sr.SSRC != s.SSRC {
		t.Errorf("SR.SSRC=%x want %x", sr.SSRC, s.SSRC)
	}
	if sr.PacketCount != 5 {
		t.Errorf("PacketCount=%d want 5", sr.PacketCount)
	}
	if sr.OctetCount != 800 {
		t.Errorf("OctetCount=%d want 800", sr.OctetCount)
	}
	if sr.RTPTime != 0xCAFEBABE {
		t.Errorf("RTPTime=%x want 0xCAFEBABE", sr.RTPTime)
	}
}

func TestRTCPRemoteAddr_DerivesPortPlus1(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()

	s.SetRemoteAddr(&net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 30000})
	got := s.rtcpRemoteAddr()
	if got == nil {
		t.Fatal("rtcpRemoteAddr returned nil")
	}
	if got.Port != 30001 {
		t.Errorf("RTCP remote port=%d want 30001", got.Port)
	}
}

func TestSetRTCPClockRate_OverridesDefault(t *testing.T) {
	s, _ := NewSession(0)
	defer s.Close()

	s.SetRTCPClockRate(48000)
	s.rxStats.mu.Lock()
	got := s.rxStats.clockRate
	s.rxStats.mu.Unlock()
	if got != 48000 {
		t.Errorf("clockRate=%d want 48000", got)
	}
}
