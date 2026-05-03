package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNextDailyBackupAtUsesLocal0400(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("Test/Local", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	before := time.Date(2026, 4, 16, 3, 30, 0, 0, time.Local)
	if got := nextDailyBackupAt(before); !got.Equal(time.Date(2026, 4, 16, 4, 0, 0, 0, time.Local)) {
		t.Fatalf("expected same-day 04:00 backup, got %s", got)
	}
	after := time.Date(2026, 4, 16, 4, 1, 0, 0, time.Local)
	if got := nextDailyBackupAt(after); !got.Equal(time.Date(2026, 4, 17, 4, 0, 0, 0, time.Local)) {
		t.Fatalf("expected next-day 04:00 backup, got %s", got)
	}
}

func TestDatabaseBackupRunnerRunsAtScheduledTime(t *testing.T) {
	writer := &databaseBackupWriterStub{}
	cleaner := &databaseBackupCleanerStub{}
	runner := NewDatabaseBackupRunner(writer, cleaner, 24*time.Hour, 30)
	now := time.Date(2026, 4, 16, 3, 45, 0, 0, time.Local)
	runner.now = func() time.Time { return now }
	sleepCalls := 0
	runner.sleep = func(context.Context, time.Duration) bool {
		sleepCalls++
		if sleepCalls == 1 {
			return true
		}
		return false
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

func TestDatabaseBackupRunnerWaitsUntilNext0400AfterDailyScheduleWithoutTodaysBackup(t *testing.T) {
	writer := &databaseBackupWriterStub{}
	runner := NewDatabaseBackupRunner(writer, nil, 24*time.Hour, 0)
	now := time.Date(2026, 4, 16, 4, 5, 0, 0, time.Local)
	writer.lastBackupAt = now.AddDate(0, 0, -1)
	runner.now = func() time.Time { return now }
	var delay time.Duration
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		delay = d
		return false
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	expected := time.Date(2026, 4, 17, 4, 0, 0, 0, time.Local).Sub(now)
	if delay != expected {
		t.Fatalf("expected next 04:00 backup delay %s, got %s", expected, delay)
	}
}

func TestDatabaseBackupRunnerWaitsUntilTomorrowAfterTodaysDailyBackup(t *testing.T) {
	writer := &databaseBackupWriterStub{}
	runner := NewDatabaseBackupRunner(writer, nil, 24*time.Hour, 0)
	now := time.Date(2026, 4, 16, 4, 5, 0, 0, time.Local)
	writer.lastBackupAt = time.Date(2026, 4, 16, 4, 0, 0, 0, time.Local)
	runner.now = func() time.Time { return now }
	var delay time.Duration
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		delay = d
		return false
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	expected := time.Date(2026, 4, 17, 4, 0, 0, 0, time.Local).Sub(now)
	if delay != expected {
		t.Fatalf("expected next daily backup delay %s, got %s", expected, delay)
	}
}

func TestDatabaseBackupRunnerUsesDailyScheduleForMultipleOf24Hours(t *testing.T) {
	writer := &databaseBackupWriterStub{}
	runner := NewDatabaseBackupRunner(writer, nil, 48*time.Hour, 0)
	now := time.Date(2026, 4, 16, 4, 5, 0, 0, time.Local)
	writer.lastBackupAt = time.Date(2026, 4, 15, 10, 0, 0, 0, time.Local)
	runner.now = func() time.Time { return now }
	var delay time.Duration
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		delay = d
		return false
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	expected := time.Date(2026, 4, 17, 4, 0, 0, 0, time.Local).Sub(now)
	if delay != expected {
		t.Fatalf("expected next 48h daily schedule delay %s, got %s", expected, delay)
	}
}

func TestDatabaseBackupRunnerUsesExistingBackupForIntervalSchedule(t *testing.T) {
	writer := &databaseBackupWriterStub{}
	runner := NewDatabaseBackupRunner(writer, nil, 10*time.Second, 0)
	now := time.Date(2026, 4, 16, 3, 45, 0, 0, time.Local)
	writer.lastBackupAt = now.Add(-4 * time.Second)
	runner.now = func() time.Time { return now }
	var delays []time.Duration
	sleepCalls := 0
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		sleepCalls++
		delays = append(delays, d)
		return sleepCalls == 1
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(delays) == 0 || delays[0] != 6*time.Second {
		t.Fatalf("expected first interval delay 6s, got %+v", delays)
	}
	if writer.calls != 1 {
		t.Fatalf("expected one database backup, got %d", writer.calls)
	}
}

func TestDatabaseBackupRunnerRunsImmediatelyWhenIntervalBackupIsExpired(t *testing.T) {
	writer := &databaseBackupWriterStub{}
	runner := NewDatabaseBackupRunner(writer, nil, 10*time.Second, 0)
	now := time.Date(2026, 4, 16, 3, 45, 0, 0, time.Local)
	writer.lastBackupAt = now.Add(-11 * time.Second)
	runner.now = func() time.Time { return now }
	var delays []time.Duration
	sleepCalls := 0
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		sleepCalls++
		delays = append(delays, d)
		return sleepCalls == 1
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(delays) == 0 || delays[0] != 0 {
		t.Fatalf("expected immediate backup for expired interval, got delays %+v", delays)
	}
}

func TestDatabaseBackupRunnerCleansAfterBackupFailure(t *testing.T) {
	logs := captureAppInfoLogs(t)
	writer := &databaseBackupWriterStub{err: errors.New("disk full")}
	cleaner := &databaseBackupCleanerStub{}
	runner := NewDatabaseBackupRunner(writer, cleaner, time.Second, 30)
	runner.now = func() time.Time { return time.Date(2026, 4, 16, 3, 45, 0, 0, time.Local) }
	sleepCalls := 0
	runner.sleep = func(context.Context, time.Duration) bool {
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
	runner.now = func() time.Time { return now }
	var delays []time.Duration
	sleepCalls := 0
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		delays = append(delays, d)
		sleepCalls++
		switch sleepCalls {
		case 1:
			now = time.Date(2026, 4, 16, 4, 0, 0, 0, time.Local)
		case 2:
			now = time.Date(2026, 4, 16, 4, 15, 0, 0, time.Local)
		case 3:
			now = time.Date(2026, 4, 16, 4, 30, 0, 0, time.Local)
		case 4:
			now = time.Date(2026, 4, 16, 4, 45, 0, 0, time.Local)
		default:
			return false
		}
		return true
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	expectedDelays := []time.Duration{time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute, 23*time.Hour + 15*time.Minute}
	if len(delays) != len(expectedDelays) {
		t.Fatalf("expected delays %+v, got %+v", expectedDelays, delays)
	}
	for i, expected := range expectedDelays {
		if delays[i] != expected {
			t.Fatalf("expected delay %d to be %s, got %s", i, expected, delays[i])
		}
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
	now           time.Time
	err           error
}

func (s *databaseBackupCleanerStub) Cleanup(retentionDays int, now time.Time) (int, error) {
	s.calls++
	s.retentionDays = retentionDays
	s.now = now
	return 0, s.err
}
