package test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/ranking"
)

type runnerSyncerStub struct {
	mu        sync.Mutex
	calls     int
	deadlines []time.Duration
	cancel    context.CancelFunc
	failFirst bool
	seed      string
	seedRead  chan string
}

type runnerIntervalStub struct {
	*runnerSyncerStub
	interval time.Duration
}

func (s *runnerIntervalStub) SyncInterval(context.Context) (time.Duration, error) {
	return s.interval, nil
}

func (s *runnerSyncerStub) RunOnce(ctx context.Context) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlines = append(s.deadlines, time.Until(deadline))
	}
	cancel := s.cancel
	s.mu.Unlock()
	if call >= 2 && cancel != nil {
		cancel()
	}
	if call == 1 && s.failFirst {
		return errors.New("temporary failure")
	}
	return nil
}

func (s *runnerSyncerStub) ScheduleSeed(context.Context) (string, error) {
	s.mu.Lock()
	seed := s.seed
	seedRead := s.seedRead
	s.mu.Unlock()
	if seed == "" {
		seed = "stable-instance-seed"
	}
	if seedRead != nil {
		select {
		case seedRead <- seed:
		default:
		}
	}
	return seed, nil
}

func (s *runnerSyncerStub) setSeed(seed string) {
	s.mu.Lock()
	s.seed = seed
	s.mu.Unlock()
}

func TestRunnerWaitsForStableSlotBeforeFirstAttempt(t *testing.T) {
	const interval = time.Second
	now := time.Now()
	seed := ""
	for candidate := 0; candidate < 10_000; candidate++ {
		value := fmt.Sprintf("delayed-instance-%d", candidate)
		if ranking.NextScheduledRun(now, value, interval).Sub(now) >= 500*time.Millisecond {
			seed = value
			break
		}
	}
	if seed == "" {
		t.Fatal("could not find a deterministic delayed schedule seed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	stub := &runnerSyncerStub{seed: seed}
	runner, err := ranking.NewRunnerWithTiming(stub, interval, 40*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRunnerWithTiming returned error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.calls != 0 {
		t.Fatalf("runner synchronized before its stable slot: calls=%d", stub.calls)
	}
}

func TestRunnerUsesHardTimeoutAndContinuesAfterError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := &runnerSyncerStub{cancel: cancel, failFirst: true}
	runner, err := ranking.NewRunnerWithTiming(stub, 5*time.Millisecond, 40*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRunnerWithTiming returned error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not continue to a second attempt")
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.calls < 2 {
		t.Fatalf("expected scheduled attempt and retry, got %d calls", stub.calls)
	}
	if len(stub.deadlines) == 0 || stub.deadlines[0] <= 0 || stub.deadlines[0] > 40*time.Millisecond {
		t.Fatalf("unexpected runner deadline: %+v", stub.deadlines)
	}
}

func TestRunnerUsesCenterSyncIntervalInsteadOfLocalDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := &runnerSyncerStub{cancel: cancel}
	syncer := &runnerIntervalStub{runnerSyncerStub: stub, interval: 5 * time.Millisecond}
	runner, err := ranking.NewRunnerWithTiming(syncer, time.Hour, 40*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRunnerWithTiming returned error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("runner ignored the center sync interval")
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.calls < 2 {
		t.Fatalf("expected repeated center-scheduled runs, got %d", stub.calls)
	}
}

func TestRunnerReschedulesWithoutSyncingWhenIdentityChangesBeforeOldSlot(t *testing.T) {
	const interval = 500 * time.Millisecond
	now := time.Now()
	findSeed := func(minDelay, maxDelay time.Duration) string {
		for candidate := 0; candidate < 100_000; candidate++ {
			seed := fmt.Sprintf("seed-%d", candidate)
			delay := ranking.NextScheduledRun(now, seed, interval).Sub(now)
			if delay >= minDelay && delay <= maxDelay {
				return seed
			}
		}
		return ""
	}
	oldSeed := findSeed(140*time.Millisecond, 200*time.Millisecond)
	newSeed := findSeed(350*time.Millisecond, 420*time.Millisecond)
	if oldSeed == "" || newSeed == "" {
		t.Fatal("could not find deterministic seeds for the runner schedule")
	}

	ctx, cancel := context.WithCancel(context.Background())
	stub := &runnerSyncerStub{seed: oldSeed, seedRead: make(chan string, 4)}
	runner, err := ranking.NewRunnerWithTiming(stub, interval, 40*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRunnerWithTiming returned error: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	select {
	case scheduledSeed := <-stub.seedRead:
		if scheduledSeed != oldSeed {
			t.Fatalf("initial schedule seed = %q, want %q", scheduledSeed, oldSeed)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not read the initial schedule seed")
	}
	stub.setSeed(newSeed)
	time.Sleep(ranking.NextScheduledRun(now, oldSeed, interval).Sub(now) + 60*time.Millisecond)

	stub.mu.Lock()
	calls := stub.calls
	stub.mu.Unlock()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("runner used the obsolete disabled slot after the seed changed: calls=%d", calls)
	}
}

func TestNextRankingRunUsesStableJitterWithinThirtyMinutes(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 3, 0, 0, time.UTC)
	first := ranking.NextScheduledRun(now, "instance-a", 30*time.Minute)
	second := ranking.NextScheduledRun(now, "instance-a", 30*time.Minute)
	other := ranking.NextScheduledRun(now, "instance-b", 30*time.Minute)

	if !first.Equal(second) {
		t.Fatalf("same seed produced different times: %s and %s", first, second)
	}
	if !first.After(now) || first.Sub(now) > 30*time.Minute {
		t.Fatalf("scheduled time is outside the next interval: now=%s next=%s", now, first)
	}
	if first.Equal(other) {
		t.Fatalf("different seeds unexpectedly produced the same jitter: %s", first)
	}
}
