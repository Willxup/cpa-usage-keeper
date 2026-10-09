package test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"cpa-usage-keeper/internal/ranking"
)

type localRankingAggregatorStub struct {
	calls atomic.Int64
}

func (s *localRankingAggregatorStub) AggregateOnce(context.Context) error {
	s.calls.Add(1)
	return nil
}

func TestLocalRankingRunnerWaitsFiveMinutesBeforeStartingAndKeepsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		aggregator := &localRankingAggregatorStub{}
		runner, err := ranking.NewLocalRankingRunner(aggregator)
		if err != nil {
			t.Fatalf("create local ranking runner: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- runner.Run(ctx) }()
		synctest.Wait()
		for _, step := range []struct {
			elapsed   time.Duration
			wantCalls int64
		}{
			{5*time.Minute - time.Nanosecond, 0},
			{time.Nanosecond, 1},
			{5 * time.Minute, 2},
		} {
			time.Sleep(step.elapsed)
			synctest.Wait()
			if aggregator.calls.Load() != step.wantCalls {
				t.Fatalf("aggregation calls = %d, want %d", aggregator.calls.Load(), step.wantCalls)
			}
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("local ranking runner stopped with error: %v", err)
		}
	})
}
