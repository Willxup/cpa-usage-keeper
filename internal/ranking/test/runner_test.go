package test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"cpa-usage-keeper/internal/ranking"
)

type runnerSyncerStub struct {
	seedMu    sync.Mutex
	calls     int
	deadlines []time.Duration
	cancel    context.CancelFunc
	failFirst bool
	seed      string
}

type runnerIntervalStub struct {
	*runnerSyncerStub
	interval time.Duration
}

func (s *runnerIntervalStub) SyncInterval(context.Context) (time.Duration, error) {
	return s.interval, nil
}

func (s *runnerSyncerStub) RunOnce(ctx context.Context) error {
	s.calls++
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlines = append(s.deadlines, time.Until(deadline))
	}
	if s.calls >= 2 && s.cancel != nil {
		s.cancel()
	}
	if s.calls == 1 && s.failFirst {
		return errors.New("temporary failure")
	}
	return nil
}

func (s *runnerSyncerStub) ScheduleSeed(context.Context) (string, error) {
	s.seedMu.Lock()
	defer s.seedMu.Unlock()
	return s.seed, nil
}

func TestRunnerWaitsForStableSlotBeforeFirstAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = time.Second
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stub := &runnerSyncerStub{seed: "stable-instance-seed"}
		runner, err := ranking.NewRunnerWithTiming(stub, interval, 40*time.Millisecond)
		if err != nil {
			t.Fatalf("NewRunnerWithTiming: %v", err)
		}
		delay := ranking.NextScheduledRun(time.Now(), stub.seed, interval).Sub(time.Now())
		done := make(chan error, 1)
		go func() { done <- runner.Run(ctx) }()
		synctest.Wait()
		time.Sleep(delay - time.Nanosecond)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run: %v", err)
		}
		if stub.calls != 0 {
			t.Fatalf("runner synchronized before its stable slot: calls=%d", stub.calls)
		}
	})
}

func TestRunnerRepeatsAfterErrorAndUsesCenterInterval(t *testing.T) {
	for _, tc := range []struct {
		name           string
		centerInterval bool
		failFirst      bool
	}{
		{name: "local interval retries after error", failFirst: true},
		{name: "center interval overrides local default", centerInterval: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				stub := &runnerSyncerStub{cancel: cancel, failFirst: tc.failFirst, seed: "stable-instance-seed"}
				var syncer ranking.RunnerSyncer = stub
				interval := 5 * time.Millisecond
				if tc.centerInterval {
					syncer = &runnerIntervalStub{runnerSyncerStub: stub, interval: interval}
					interval = time.Hour
				}
				runner, err := ranking.NewRunnerWithTiming(syncer, interval, 40*time.Millisecond)
				if err != nil {
					t.Fatalf("NewRunnerWithTiming: %v", err)
				}
				done := make(chan error, 1)
				go func() { done <- runner.Run(ctx) }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("Run: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("runner did not continue at the selected interval")
				}
				if stub.calls != 2 || len(stub.deadlines) != 2 {
					t.Fatalf("expected two scheduled attempts with deadlines, got calls=%d deadlines=%v", stub.calls, stub.deadlines)
				}
				for _, deadline := range stub.deadlines {
					if deadline != 40*time.Millisecond {
						t.Fatalf("run deadline = %s, want 40ms", deadline)
					}
				}
			})
		})
	}
}

func TestRunnerReschedulesWithoutSyncingWhenIdentityChangesBeforeOldSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 500 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stub := &runnerSyncerStub{seed: "old-seed"}
		runner, err := ranking.NewRunnerWithTiming(stub, interval, 40*time.Millisecond)
		if err != nil {
			t.Fatalf("NewRunnerWithTiming: %v", err)
		}
		delay := ranking.NextScheduledRun(time.Now(), stub.seed, interval).Sub(time.Now())
		done := make(chan error, 1)
		go func() { done <- runner.Run(ctx) }()
		synctest.Wait()
		stub.seedMu.Lock()
		stub.seed = "new-seed"
		stub.seedMu.Unlock()
		time.Sleep(delay)
		synctest.Wait()
		if stub.calls != 0 {
			t.Fatalf("runner used the obsolete slot after the seed changed: calls=%d", stub.calls)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
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
