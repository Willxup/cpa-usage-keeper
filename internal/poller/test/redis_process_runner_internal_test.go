package poller_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
)

func TestRedisProcessRunnerSleepsAfterNonFullBatch(t *testing.T) {
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{{
		result: &servicedto.RedisBatchSyncResult{Status: "completed", ProcessedRows: 999},
	}}}
	runner := poller.NewRedisProcessRunner(syncer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisProcessRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		cancel()
		return false
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := syncer.callCount(); calls != 1 {
		t.Fatalf("expected one process call before sleep, got %d", calls)
	}
	requireDurations(t, delays, []time.Duration{time.Second})
}

func TestRedisProcessRunnerSkipsSleepAfterFullBatch(t *testing.T) {
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{
		{result: &servicedto.RedisBatchSyncResult{Status: "completed", ProcessedRows: 1000, BatchFull: true}},
		{result: &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}},
	}}
	runner := poller.NewRedisProcessRunner(syncer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisProcessRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		cancel()
		return false
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := syncer.callCount(); calls != 2 {
		t.Fatalf("expected full batch to process again before sleeping, got %d calls", calls)
	}
	requireDurations(t, delays, []time.Duration{time.Second})
}

func TestRedisProcessRunnerSkipsSleepAfterWarningFullBatch(t *testing.T) {
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{
		{result: &servicedto.RedisBatchSyncResult{Status: "completed_with_warnings", ProcessedRows: 1000, BatchFull: true}, err: errors.New("decode warning")},
		{result: &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}},
	}}
	runner := poller.NewRedisProcessRunner(syncer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisProcessRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		cancel()
		return false
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := syncer.callCount(); calls != 2 {
		t.Fatalf("expected warning full batch to process again before sleeping, got %d calls", calls)
	}
	requireDurations(t, delays, []time.Duration{time.Second})
}

func TestRedisProcessRunnerSleepsAfterWarningFullBatchWithRetryPending(t *testing.T) {
	// 满批中的部分成功不能掩盖待重试行；否则同一临时故障会在连续 drain 中瞬间耗尽五次机会。
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{{
		result: &servicedto.RedisBatchSyncResult{Status: "completed_with_warnings", ProcessedRows: 1000, BatchFull: true, RetryPending: true},
		err:    errors.New("identity lookup warning"),
	}}}
	runner := poller.NewRedisProcessRunner(syncer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisProcessRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		cancel()
		return false
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := syncer.callCount(); calls != 1 {
		t.Fatalf("expected retryable warning to sleep before another process call, got %d calls", calls)
	}
	requireDurations(t, delays, []time.Duration{time.Second})
}

func TestRedisProcessRunnerSleepsAfterFailedFullBatch(t *testing.T) {
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{{
		result: &servicedto.RedisBatchSyncResult{Status: "failed", ProcessedRows: 1000, BatchFull: true},
		err:    errors.New("sqlite locked"),
	}}}
	runner := poller.NewRedisProcessRunner(syncer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisProcessRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		cancel()
		return false
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := syncer.callCount(); calls != 1 {
		t.Fatalf("expected one failed full batch process call before sleep, got %d", calls)
	}
	requireDurations(t, delays, []time.Duration{time.Second})
}

func TestRedisProcessRunnerDoesNotRepeatManagedRowFailureWarnings(t *testing.T) {
	// 可重试行和已丢弃行都由 service 的逐行状态机负责，runner 不能再输出重复批次错误。
	tests := []struct {
		name   string
		result *servicedto.RedisBatchSyncResult
	}{
		{name: "retry pending", result: &servicedto.RedisBatchSyncResult{Status: "failed", ProcessedRows: 1, RetryPending: true}},
		{name: "confirmed discard", result: &servicedto.RedisBatchSyncResult{Status: "failed", ProcessedRows: 1, DiscardedRows: 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logs := capturePollerLogs(t, logrus.ErrorLevel)
			syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{{result: test.result, err: errors.New("identity lookup failed")}}}
			runner := poller.NewRedisProcessRunner(syncer)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			setRedisProcessRunnerSleep(t, runner, func(context.Context, time.Duration) bool {
				cancel()
				return false
			})

			if err := runner.Run(ctx); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if strings.Contains(logs.String(), "redis process batch failed") {
				t.Fatalf("managed row failure must not emit duplicate runner error: %s", logs.String())
			}
		})
	}
}

type redisProcessSyncerResult struct {
	result *servicedto.RedisBatchSyncResult
	err    error
}

type sequenceRedisProcessSyncer struct {
	mu      sync.Mutex
	results []redisProcessSyncerResult
	calls   int
}

func (s *sequenceRedisProcessSyncer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *sequenceRedisProcessSyncer) ProcessRedisUsageInbox(context.Context) (*servicedto.RedisBatchSyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.results) {
		s.calls++
		return &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}, nil
	}
	result := s.results[s.calls]
	s.calls++
	return result.result, result.err
}
