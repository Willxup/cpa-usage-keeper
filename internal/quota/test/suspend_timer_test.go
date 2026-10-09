package test

import (
	"testing"
	"time"
)

func TestSuspendAwareTimerDelivers(t *testing.T) {
	for _, delay := range []time.Duration{0, 50 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) {
			start := time.Now()
			ch, stop, err := newSuspendAwareTimer(delay)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			select {
			case <-ch:
				if elapsed := time.Since(start); elapsed < delay-10*time.Millisecond {
					t.Fatalf("timer fired too early: %v", elapsed)
				}
			case <-time.After(time.Second):
				t.Fatal("timer did not fire")
			}
		})
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
