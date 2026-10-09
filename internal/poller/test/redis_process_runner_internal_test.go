package poller_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
)

func TestRedisProcessRunnerSleepsUnlessFullBatchCanContinue(t *testing.T) {
	for _, tc := range []struct {
		name      string
		first     redisProcessSyncerResult
		wantCalls int
	}{
		{"non-full", redisProcessSyncerResult{result: &servicedto.RedisBatchSyncResult{Status: "completed", ProcessedRows: 999}}, 1},
		{"full", redisProcessSyncerResult{result: &servicedto.RedisBatchSyncResult{Status: "completed", ProcessedRows: 1000, BatchFull: true}}, 2},
		{"warning full", redisProcessSyncerResult{result: &servicedto.RedisBatchSyncResult{Status: "completed_with_warnings", ProcessedRows: 1000, BatchFull: true}, err: errors.New("decode warning")}, 2},
		// 待重试行必须等待，避免一次 drain 耗尽全部重试机会。
		{"warning retry pending", redisProcessSyncerResult{result: &servicedto.RedisBatchSyncResult{Status: "completed_with_warnings", ProcessedRows: 1000, BatchFull: true, RetryPending: true}, err: errors.New("identity lookup warning")}, 1},
		{"failed full", redisProcessSyncerResult{result: &servicedto.RedisBatchSyncResult{Status: "failed", ProcessedRows: 1000, BatchFull: true}, err: errors.New("sqlite locked")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{tc.first}}
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
			if syncer.calls != tc.wantCalls {
				t.Fatalf("process calls before sleep = %d, want %d", syncer.calls, tc.wantCalls)
			}
			requireDurations(t, delays, []time.Duration{time.Second})
		})
	}
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
	results []redisProcessSyncerResult
	calls   int
}

func (s *sequenceRedisProcessSyncer) ProcessRedisUsageInbox(context.Context) (*servicedto.RedisBatchSyncResult, error) {
	if s.calls >= len(s.results) {
		s.calls++
		return &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}, nil
	}
	result := s.results[s.calls]
	s.calls++
	return result.result, result.err
}
