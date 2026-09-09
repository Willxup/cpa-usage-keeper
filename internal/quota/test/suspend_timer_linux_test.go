//go:build linux

package test

import (
	"testing"
	"time"
	_ "unsafe"

	"golang.org/x/sys/unix"
)

//go:linkname suspendAwareTimerParameters cpa-usage-keeper/internal/quota.suspendAwareTimerParameters
func suspendAwareTimerParameters(delay time.Duration) (int, int, int, unix.ItimerSpec)

func TestSuspendAwareTimerParametersUseBootTimeOneShot(t *testing.T) {
	delay := 90*time.Minute + 123*time.Millisecond
	clockID, createFlags, settimeFlags, spec := suspendAwareTimerParameters(delay)

	if clockID != unix.CLOCK_BOOTTIME {
		t.Fatalf("expected CLOCK_BOOTTIME, got %d", clockID)
	}
	wantCreateFlags := unix.TFD_CLOEXEC | unix.TFD_NONBLOCK
	if createFlags != wantCreateFlags {
		t.Fatalf("expected timerfd create flags %d, got %d", wantCreateFlags, createFlags)
	}
	if settimeFlags != 0 {
		t.Fatalf("expected relative timerfd settime flags, got %d", settimeFlags)
	}
	if got := unix.TimespecToNsec(spec.Value); got != delay.Nanoseconds() {
		t.Fatalf("expected timer delay %s, got %s", delay, time.Duration(got))
	}
	if got := unix.TimespecToNsec(spec.Interval); got != 0 {
		t.Fatalf("expected one-shot timer interval, got %s", time.Duration(got))
	}
}
