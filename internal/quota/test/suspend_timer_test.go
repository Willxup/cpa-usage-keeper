package test

import (
	"testing"
	"time"
)

func TestSuspendAwareTimerZeroDelayFiresImmediately(t *testing.T) {
	ch, stop, err := newSuspendAwareTimer(0)
	if err != nil {
		t.Fatalf("unexpected error for 0 delay: %v", err)
	}
	defer stop()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected 0 delay timer to fire immediately")
	}
}

func TestSuspendAwareTimerFiresAfterDelay(t *testing.T) {
	start := time.Now()
	ch, stop, err := newSuspendAwareTimer(50 * time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stop()

	select {
	case <-ch:
		elapsed := time.Since(start)
		if elapsed < 40*time.Millisecond {
			t.Fatalf("timer fired too early: %v", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("expected timer to fire after delay")
	}
}

func TestSuspendAwareTimerStopCancelsDelivery(t *testing.T) {
	ch, stop, err := newSuspendAwareTimer(time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stop()

	select {
	case <-ch:
		t.Fatal("expected stopped timer not to deliver an event")
	case <-time.After(1250 * time.Millisecond):
	}
}
