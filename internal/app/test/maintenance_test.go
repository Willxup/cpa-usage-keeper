package test

import (
	"context"
	. "cpa-usage-keeper/internal/app"
	"strings"
	"testing"
	"time"
)

type maintenanceSyncStub struct {
	cleanupCalls int
}

type canceledMaintenanceSyncStub struct {
	cancel context.CancelFunc
}

func (s *canceledMaintenanceSyncStub) CleanupStorage(ctx context.Context) error {
	s.cancel()
	return ctx.Err()
}

func (s *maintenanceSyncStub) CleanupStorage(context.Context) error {
	s.cleanupCalls++
	return nil
}

func TestNextDailyCleanupAtUsesLocal0430(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	before := nextDailyCleanupAt(time.Date(2026, 4, 26, 18, 30, 0, 0, time.UTC))
	if !before.Equal(time.Date(2026, 4, 26, 20, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected same local day 04:30 cleanup, got %s", before)
	}
	after := nextDailyCleanupAt(time.Date(2026, 4, 26, 20, 30, 0, 0, time.UTC))
	if !after.Equal(time.Date(2026, 4, 27, 20, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected next local day 04:30 cleanup, got %s", after)
	}
}

func TestStorageCleanupRunnerLogsTaskStart(t *testing.T) {
	logs := captureAppInfoLogs(t)
	syncer := &maintenanceSyncStub{}
	runner := NewStorageCleanupRunner(syncer)
	(*appTestField[func() time.Time](runner, "now")) = func() time.Time { return time.Date(2026, 4, 26, 18, 30, 0, 0, time.UTC) }
	(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(context.Context, time.Duration) bool { return false }

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("cleanup runner returned error: %v", err)
	}

	content := logs.String()
	if !strings.Contains(content, "level=info") || !strings.Contains(content, "msg=\"storage cleanup task started\"") {
		t.Fatalf("expected storage cleanup start info log, got %q", content)
	}
}

func TestStorageCleanupRunnerRunsAtScheduledTime(t *testing.T) {
	syncer := &maintenanceSyncStub{}
	runner := NewStorageCleanupRunner(syncer)
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	(*appTestField[func() time.Time](runner, "now")) = func() time.Time { return time.Date(2026, 4, 26, 18, 30, 0, 0, time.UTC) }
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(_ context.Context, d time.Duration) bool {
		calls++
		if calls == 1 {
			if d != 2*time.Hour {
				t.Fatalf("expected cleanup sleep until local 04:30, got %s", d)
			}
			return true
		}
		cancel()
		return false
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("cleanup runner returned error: %v", err)
	}

	if syncer.cleanupCalls != 1 {
		t.Fatalf("expected cleanup loop to run once, got %d", syncer.cleanupCalls)
	}
}

func TestStorageCleanupRunnerDoesNotLogPausedWaitCancellationAsFailure(t *testing.T) {
	logs := captureAppInfoLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	runner := NewStorageCleanupRunner(&canceledMaintenanceSyncStub{cancel: cancel})
	(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("取消维护等待应正常结束 runner：%v", err)
	}
	if strings.Contains(logs.String(), "storage cleanup failed") {
		t.Fatalf("暂停等待的生命周期取消不应记维护失败：%s", logs.String())
	}
}
