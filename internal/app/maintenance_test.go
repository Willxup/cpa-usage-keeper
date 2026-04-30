package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

type maintenanceSyncStub struct {
	cleanupCalls int
}

func (s *maintenanceSyncStub) CleanupStorage(context.Context) error {
	s.cleanupCalls++
	return nil
}

func TestNextDailyCleanupAtUsesLocalThreeAM(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	before := nextDailyCleanupAt(time.Date(2026, 4, 26, 18, 30, 0, 0, time.UTC))
	if !before.Equal(time.Date(2026, 4, 26, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected same local day 03:00 cleanup, got %s", before)
	}
	after := nextDailyCleanupAt(time.Date(2026, 4, 26, 20, 30, 0, 0, time.UTC))
	if !after.Equal(time.Date(2026, 4, 27, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected next local day 03:00 cleanup, got %s", after)
	}
}

func TestStorageCleanupRunnerLogsTaskStart(t *testing.T) {
	logs := captureMaintenanceInfoLogs(t)
	syncer := &maintenanceSyncStub{}
	runner := NewStorageCleanupRunner(syncer)
	runner.now = func() time.Time { return time.Date(2026, 4, 26, 18, 30, 0, 0, time.UTC) }
	runner.sleep = func(context.Context, time.Duration) bool { return false }

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
	runner.now = func() time.Time { return time.Date(2026, 4, 26, 18, 30, 0, 0, time.UTC) }
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		calls++
		if calls == 1 {
			if d != 30*time.Minute {
				t.Fatalf("expected cleanup sleep until local 03:00, got %s", d)
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

func captureMaintenanceInfoLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}
