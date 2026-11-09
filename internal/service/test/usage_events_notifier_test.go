package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestUsageEventsNotifierCoalescesSlowSubscriberWithoutBlocking(t *testing.T) {
	notifier := service.NewUsageEventsNotifier()
	wake, unsubscribe := notifier.Subscribe()
	defer unsubscribe()
	done := make(chan struct{})
	go func() {
		for id := int64(1); id <= 10000; id++ {
			notifier.NotifyUsageEventsCommitted([]entities.UsageEvent{{ID: id}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unread subscriber blocked committed event notifications")
	}
	select {
	case <-wake:
	default:
		t.Fatal("subscriber did not receive a wakeup")
	}
	select {
	case <-wake:
		t.Fatal("redundant wakeups were not coalesced")
	default:
	}
}
