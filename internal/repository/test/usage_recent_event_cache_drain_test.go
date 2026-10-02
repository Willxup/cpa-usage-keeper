package test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/repository"
)

func TestUsageRecentEventCacheDrainWaitsForWorkerAndKeepsState(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	cache, workerEntered, releaseWorker := newBlockedDrainTestCache(t, now)
	if !cache.TryAppend([]entities.UsageEvent{{EventKey: "first", APIGroupKey: "provider-a", AuthType: "oauth", AuthIndex: "auth-1", Timestamp: now.Add(-time.Minute), TotalTokens: 10}}) {
		t.Fatal("first append was not accepted")
	}
	// worker 已取走队列元素，但仍未写入事件和 5h 健康桶。
	waitForDrainTestSignal(t, workerEntered, "worker did not receive accepted append")

	drained := make(chan error, 1)
	go func() { drained <- cache.DrainAcceptedAppends(context.Background()) }()
	select {
	case err := <-drained:
		t.Fatalf("drain returned before worker completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	releaseWorker()
	if err := waitForDrainTestResult(t, drained); err != nil {
		t.Fatalf("drain accepted append: %v", err)
	}
	if events, ok := cache.Events(now.Add(-time.Hour), now, false, ""); !ok || len(events) != 1 || events[0].TotalTokens != 10 {
		t.Fatalf("drain removed or missed cached events: ok=%v events=%+v", ok, events)
	}
	if health, ok := cache.CredentialHealth("oauth", "auth-1", now); !ok || health.TotalSuccess != 1 {
		t.Fatalf("drain removed or missed credential health: ok=%v health=%+v", ok, health)
	}

	if !cache.TryAppend([]entities.UsageEvent{{EventKey: "second", APIGroupKey: "provider-a", AuthType: "oauth", AuthIndex: "auth-1", Timestamp: now.Add(-time.Minute), Failed: true, TotalTokens: 20}}) {
		t.Fatal("append after drain was not accepted")
	}
	if err := cache.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if events, ok := cache.Events(now.Add(-time.Hour), now, false, ""); !ok || len(events) != 2 {
		t.Fatalf("append after drain did not reach cache: ok=%v events=%+v", ok, events)
	}
	if health, ok := cache.CredentialHealth("oauth", "auth-1", now); !ok || health.TotalSuccess != 1 || health.TotalFailure != 1 {
		t.Fatalf("drain changed 5h health buckets: ok=%v health=%+v", ok, health)
	}
}

func TestUsageRecentEventCacheDrainCancellationLeavesWorkerUsable(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	cache, workerEntered, releaseWorker := newBlockedDrainTestCache(t, now)
	if !cache.TryAppend([]entities.UsageEvent{{EventKey: "first", Timestamp: now.Add(-time.Minute)}}) {
		t.Fatal("append was not accepted")
	}
	waitForDrainTestSignal(t, workerEntered, "worker did not receive accepted append")
	ctx, cancel := context.WithCancel(context.Background())
	drained := make(chan error, 1)
	go func() { drained <- cache.DrainAcceptedAppends(ctx) }()
	select {
	case err := <-drained:
		t.Fatalf("drain returned before cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if err := waitForDrainTestResult(t, drained); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled drain, got %v", err)
	}
	releaseWorker()
	drained = make(chan error, 1)
	go func() { drained <- cache.DrainAcceptedAppends(context.Background()) }()
	if err := waitForDrainTestResult(t, drained); err != nil {
		t.Fatalf("drain after cancellation: %v", err)
	}
	if events, ok := cache.Events(now.Add(-time.Hour), now, false, ""); !ok || len(events) != 1 {
		t.Fatalf("cancellation stopped the worker: ok=%v events=%+v", ok, events)
	}
}

func TestUsageRecentEventCacheDrainAndCloseRaceEnds(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	cache, workerEntered, releaseWorker := newBlockedDrainTestCache(t, now)
	if !cache.TryAppend([]entities.UsageEvent{{EventKey: "first", Timestamp: now.Add(-time.Minute)}}) {
		t.Fatal("first append was not accepted")
	}
	waitForDrainTestSignal(t, workerEntered, "worker did not receive accepted append")
	if !cache.TryAppend([]entities.UsageEvent{{EventKey: "second", Timestamp: now.Add(-time.Minute)}}) {
		t.Fatal("second append was not accepted")
	}
	drained := make(chan error, 1)
	go func() { drained <- cache.DrainAcceptedAppends(context.Background()) }()
	closed := make(chan struct{})
	go func() { cache.Close(); close(closed) }()
	waitForDrainTestSignal(t, *recentCacheField[chan struct{}](cache, "stopCh"), "Close did not signal worker")
	releaseWorker()
	if err := waitForDrainTestResult(t, drained); err == nil {
		if events, ok := cache.Events(now.Add(-time.Hour), now, false, ""); !ok || len(events) != 2 {
			t.Fatalf("successful drain missed a previously accepted append: ok=%v events=%+v", ok, events)
		}
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close race returned context error: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after worker release")
	}
}

func TestNilUsageRecentEventCacheDrainIsNoop(t *testing.T) {
	var cache *UsageRecentEventCache
	if err := cache.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatalf("nil cache drain: %v", err)
	}
}

func newBlockedDrainTestCache(t *testing.T, now time.Time) (*UsageRecentEventCache, <-chan struct{}, func()) {
	t.Helper()
	workerEntered := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Bool
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time {
		if first.CompareAndSwap(false, true) {
			close(workerEntered)
			<-release
		}
		return now
	}, QueueSize: 1})
	releaseWorker := sync.OnceFunc(func() { close(release) })
	t.Cleanup(cache.Close)
	t.Cleanup(releaseWorker)
	return cache, workerEntered, releaseWorker
}

func waitForDrainTestResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("drain did not finish")
		return nil
	}
}

func waitForDrainTestSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}
