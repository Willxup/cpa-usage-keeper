package test

import (
	"context"
	. "cpa-usage-keeper/internal/app"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNextDailyBackupAtUsesLocal0400(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("Test/Local", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	before := time.Date(2026, 4, 15, 19, 30, 0, 0, time.UTC)
	if got := nextDailyBackupAt(before); !got.Equal(time.Date(2026, 4, 16, 4, 0, 0, 0, time.Local)) {
		t.Fatalf("expected same-day 04:00 backup, got %s", got)
	}
	after := time.Date(2026, 4, 15, 20, 1, 0, 0, time.UTC)
	if got := nextDailyBackupAt(after); !got.Equal(time.Date(2026, 4, 17, 4, 0, 0, 0, time.Local)) {
		t.Fatalf("expected next-day 04:00 backup, got %s", got)
	}
}

func TestDatabaseBackupRunnerRunsAtScheduledTime(t *testing.T) {
	writer := &databaseBackupWriterStub{}
	cleaner := &databaseBackupCleanerStub{}
	runner := NewDatabaseBackupRunner(writer, cleaner, 24*time.Hour, 30)
	now := time.Date(2026, 4, 16, 3, 45, 0, 0, time.Local)
	(*appTestField[func() time.Time](runner, "now")) = func() time.Time { return now }
	sleepCalls := 0
	(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(context.Context, time.Duration) bool {
		sleepCalls++
		return sleepCalls == 1
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("expected one database backup, got %d", writer.calls)
	}
	if cleaner.calls != 1 {
		t.Fatalf("expected one backup cleanup, got %d", cleaner.calls)
	}
	if cleaner.retentionDays != 30 {
		t.Fatalf("expected cleanup retention 30, got %d", cleaner.retentionDays)
	}
}

func TestDatabaseBackupRunnerUsesDailySchedule(t *testing.T) {
	now := time.Date(2026, 4, 16, 4, 5, 0, 0, time.Local)
	for _, tc := range []struct {
		name         string
		interval     time.Duration
		lastBackupAt time.Time
	}{
		{"no backup today", 24 * time.Hour, now.AddDate(0, 0, -1)},
		{"already backed up today", 24 * time.Hour, time.Date(2026, 4, 16, 4, 0, 0, 0, time.Local)},
		{"48 hour interval", 48 * time.Hour, time.Date(2026, 4, 15, 10, 0, 0, 0, time.Local)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := &databaseBackupWriterStub{lastBackupAt: tc.lastBackupAt}
			runner := NewDatabaseBackupRunner(writer, nil, tc.interval, 0)
			(*appTestField[func() time.Time](runner, "now")) = func() time.Time { return now }
			var delay time.Duration
			(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(_ context.Context, d time.Duration) bool {
				delay = d
				return false
			}
			if err := runner.Run(context.Background()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			want := time.Date(2026, 4, 17, 4, 0, 0, 0, time.Local).Sub(now)
			if delay != want {
				t.Fatalf("backup delay = %s, want %s", delay, want)
			}
		})
	}
}

func TestDatabaseBackupRunnerResumesIntervalSchedule(t *testing.T) {
	for _, tc := range []struct {
		name       string
		age, delay time.Duration
	}{
		{"recent backup", 4 * time.Second, 6 * time.Second},
		{"expired backup", 11 * time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 4, 16, 3, 45, 0, 0, time.Local)
			writer := &databaseBackupWriterStub{lastBackupAt: now.Add(-tc.age)}
			runner := NewDatabaseBackupRunner(writer, nil, 10*time.Second, 0)
			(*appTestField[func() time.Time](runner, "now")) = func() time.Time { return now }
			var delays []time.Duration
			(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(_ context.Context, delay time.Duration) bool {
				delays = append(delays, delay)
				return len(delays) == 1
			}
			if err := runner.Run(context.Background()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if len(delays) == 0 || delays[0] != tc.delay {
				t.Fatalf("delays = %v, want first delay %s", delays, tc.delay)
			}
			if writer.calls != 1 {
				t.Fatalf("expected one database backup, got %d", writer.calls)
			}
		})
	}
}

func TestDatabaseBackupRunnerCleansAfterBackupFailure(t *testing.T) {
	logs := captureAppInfoLogs(t)
	writer := &databaseBackupWriterStub{err: errors.New("disk full")}
	cleaner := &databaseBackupCleanerStub{}
	runner := NewDatabaseBackupRunner(writer, cleaner, time.Second, 30)
	(*appTestField[func() time.Time](runner, "now")) = func() time.Time { return time.Date(2026, 4, 16, 3, 45, 0, 0, time.Local) }
	sleepCalls := 0
	(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(context.Context, time.Duration) bool {
		sleepCalls++
		return sleepCalls == 1
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("expected one database backup attempt, got %d", writer.calls)
	}
	if cleaner.calls != 1 {
		t.Fatalf("expected cleanup after failed backup, got %d calls", cleaner.calls)
	}
	content := logs.String()
	if !strings.Contains(content, "level=error") || !strings.Contains(content, "msg=\"database backup failed\"") {
		t.Fatalf("expected database backup failure error log, got %q", content)
	}
}

func TestDatabaseBackupRunnerRetriesDailyBackupAfterFailure(t *testing.T) {
	writer := &databaseBackupWriterStub{err: errors.New("temporary failure")}
	runner := NewDatabaseBackupRunner(writer, nil, 24*time.Hour, 0)
	now := time.Date(2026, 4, 16, 3, 59, 0, 0, time.Local)
	(*appTestField[func() time.Time](runner, "now")) = func() time.Time { return now }
	var delays []time.Duration
	retryTimes := []time.Time{
		time.Date(2026, 4, 16, 4, 0, 0, 0, time.Local),
		time.Date(2026, 4, 16, 4, 15, 0, 0, time.Local),
		time.Date(2026, 4, 16, 4, 30, 0, 0, time.Local),
		time.Date(2026, 4, 16, 4, 45, 0, 0, time.Local),
	}
	(*appTestField[func(context.Context, time.Duration) bool](runner, "sleep")) = func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		if len(delays) > len(retryTimes) {
			return false
		}
		now = retryTimes[len(delays)-1]
		return true
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	expectedDelays := []time.Duration{time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute, 23*time.Hour + 15*time.Minute}
	if !slices.Equal(delays, expectedDelays) {
		t.Fatalf("delays = %v, want %v", delays, expectedDelays)
	}
	if writer.calls != 4 {
		t.Fatalf("expected initial attempt plus 3 retries, got %d attempts", writer.calls)
	}
}

type databaseBackupWriterStub struct {
	calls        int
	err          error
	lastBackupAt time.Time
}

func (s *databaseBackupWriterStub) WriteDatabase(_ context.Context, backupAt time.Time) (string, error) {
	s.calls++
	if s.err == nil {
		s.lastBackupAt = backupAt
	}
	return "/tmp/database.db", s.err
}

func (s *databaseBackupWriterStub) LastBackupAt() (time.Time, bool, error) {
	return s.lastBackupAt, !s.lastBackupAt.IsZero(), nil
}

type databaseBackupCleanerStub struct {
	calls         int
	retentionDays int
}

func (s *databaseBackupCleanerStub) Cleanup(retentionDays int, _ time.Time) (int, error) {
	s.calls++
	s.retentionDays = retentionDays
	return 0, nil
}
