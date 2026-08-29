package test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/ranking"
)

type localRankingAggregatorStub struct {
	calls atomic.Int64
	seen  chan struct{}
}

func (s *localRankingAggregatorStub) AggregateOnce(context.Context) error {
	s.calls.Add(1)
	select {
	case s.seen <- struct{}{}:
	default:
	}
	return nil
}

func TestLocalRankingRunnerWaitsOneIntervalBeforeStartingAndKeepsRunning(t *testing.T) {
	aggregator := &localRankingAggregatorStub{seen: make(chan struct{}, 2)}
	runner, err := ranking.NewLocalRankingRunnerWithInterval(aggregator, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("create local ranking runner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	select {
	case <-aggregator.seen:
		t.Fatal("local ranking runner executed before the startup interval")
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-aggregator.seen:
	case <-time.After(time.Second):
		t.Fatal("local ranking runner did not execute after the startup interval")
	}
	select {
	case <-aggregator.seen:
	case <-time.After(time.Second):
		t.Fatal("local ranking runner did not continue on the next interval")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("local ranking runner stopped with error: %v", err)
	}
	if aggregator.calls.Load() < 2 {
		t.Fatalf("expected at least two aggregation calls, got %d", aggregator.calls.Load())
	}
}

func TestLocalRankingRunnerUsesFiveMinuteProductionInterval(t *testing.T) {
	if ranking.LocalRankingAggregationInterval != 5*time.Minute {
		t.Fatalf("local ranking interval = %s, want 5m", ranking.LocalRankingAggregationInterval)
	}
}
