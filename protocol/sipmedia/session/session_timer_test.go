// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package session

import (
	"sync/atomic"
	"testing"
	"time"
)

// These tests exercise CallSession.ArmSessionTimerWatchdog /
// TouchSessionTimer / StopSessionTimer in isolation. We construct a
// bare CallSession (no RTP / media / encoder) since the watchdog is
// purely a time.Timer state machine and doesn't touch the rest of
// the struct.

func newBareCallSession() *CallSession {
	return &CallSession{CallID: "test"}
}

func TestSessionTimerWatchdog_Fires(t *testing.T) {
	cs := newBareCallSession()
	var fired int32
	cs.ArmSessionTimerWatchdog(40*time.Millisecond, func() {
		atomic.StoreInt32(&fired, 1)
	})
	time.Sleep(120 * time.Millisecond)
	if atomic.LoadInt32(&fired) != 1 {
		t.Fatal("watchdog did not fire after interval elapsed")
	}
}

func TestSessionTimerWatchdog_TouchPreventsFire(t *testing.T) {
	cs := newBareCallSession()
	var fired int32
	cs.ArmSessionTimerWatchdog(60*time.Millisecond, func() {
		atomic.StoreInt32(&fired, 1)
	})
	// Touch every 30ms for 5 cycles → total elapsed 150ms but the
	// timer never gets to count down to 0.
	for i := 0; i < 5; i++ {
		time.Sleep(30 * time.Millisecond)
		cs.TouchSessionTimer()
	}
	if atomic.LoadInt32(&fired) != 0 {
		t.Fatal("watchdog fired despite repeated Touch calls")
	}
	cs.StopSessionTimer()
}

func TestSessionTimerWatchdog_StopCancels(t *testing.T) {
	cs := newBareCallSession()
	var fired int32
	cs.ArmSessionTimerWatchdog(40*time.Millisecond, func() {
		atomic.StoreInt32(&fired, 1)
	})
	cs.StopSessionTimer()
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&fired) != 0 {
		t.Fatal("watchdog fired after Stop")
	}
}

func TestSessionTimerWatchdog_ReArmReplaces(t *testing.T) {
	cs := newBareCallSession()
	var firedFirst, firedSecond int32
	cs.ArmSessionTimerWatchdog(200*time.Millisecond, func() {
		atomic.StoreInt32(&firedFirst, 1)
	})
	// Replace with a faster, different callback before the first fires.
	cs.ArmSessionTimerWatchdog(40*time.Millisecond, func() {
		atomic.StoreInt32(&firedSecond, 1)
	})
	time.Sleep(120 * time.Millisecond)
	if atomic.LoadInt32(&firedFirst) != 0 {
		t.Errorf("first callback should have been cancelled by re-Arm")
	}
	if atomic.LoadInt32(&firedSecond) != 1 {
		t.Errorf("second callback (post re-Arm) should have fired")
	}
}

func TestSessionTimerWatchdog_StopOnNilSafe(t *testing.T) {
	// Must not panic.
	var cs *CallSession
	cs.StopSessionTimer()
	cs.TouchSessionTimer()
	cs.ArmSessionTimerWatchdog(time.Second, func() {})
}

func TestSessionTimerWatchdog_TouchWithoutArmIsNoop(t *testing.T) {
	cs := newBareCallSession()
	// Should silently do nothing (no panic, no goroutine leak).
	cs.TouchSessionTimer()
}

func TestSessionTimerWatchdog_StopFromInsideStop(t *testing.T) {
	// CallSession.Stop() calls StopSessionTimer() — verify that flow
	// doesn't deadlock or double-stop the timer.
	cs := newBareCallSession()
	cs.ArmSessionTimerWatchdog(time.Second, func() {})
	cs.Stop() // should call StopSessionTimer internally
	// Re-touch should be a no-op.
	cs.TouchSessionTimer()
}
