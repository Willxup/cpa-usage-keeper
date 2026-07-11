package test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"gorm.io/gorm"
)

func TestStartAutoRefreshWithNilServiceReturns(t *testing.T) {
	var service *Service
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.StartAutoRefresh(ctx); err != nil {
		t.Fatalf("expected nil service auto refresh to return nil, got %v", err)
	}
}

func TestSleepAutoRefreshDelayAllowsNilService(t *testing.T) {
	var service *Service
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("expected nil service sleep to return safely, recovered %v", recovered)
		}
	}()

	sleepAutoRefreshDelay(service, ctx, time.Millisecond)
}

func TestNextAutoRefreshDelayAllowsNilService(t *testing.T) {
	var service *Service

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 1},
	}, time.Date(2026, 5, 26, 12, 0, 0, 0, time.Local))

	if delay != time.Minute {
		t.Fatalf("expected nil service scheduler delay to recheck after 1m, got %s", delay)
	}
}

func TestStartAutoRefreshSuppressesSettingsLookupCancellationLog(t *testing.T) {
	db := openQuotaTestDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callbackName := "test:cancel_auto_refresh_settings_lookup"
	cancelledDuringLookup := false
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if cancelledDuringLookup || !statementIncludesSettingKey(tx, "quota.auto_refresh.enabled") {
			return
		}
		cancelledDuringLookup = true
		cancel()
		tx.AddError(context.Canceled)
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	setAutoRefreshDelay(service, func(context.Context, time.Duration) bool {
		return false
	})
	hook := logrustest.NewGlobal()
	t.Cleanup(func() {
		hook.Reset()
	})

	if err := service.StartAutoRefresh(ctx); err != nil {
		t.Fatalf("StartAutoRefresh returned error: %v", err)
	}

	if !cancelledDuringLookup {
		t.Fatal("expected test hook to cancel the context during settings lookup")
	}
	assertNoAutoRefreshErrorLog(t, hook, "quota auto refresh settings lookup failed")
}

func TestStartAutoRefreshSuppressesRunCancellationLog(t *testing.T) {
	db := openQuotaTestDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	setAutoRefreshDelay(service, func(context.Context, time.Duration) bool {
		cancel()
		return true
	})
	if _, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 1},
	}); err != nil {
		t.Fatalf("UpdateAutoRefreshSettings returned error: %v", err)
	}
	hook := logrustest.NewGlobal()
	t.Cleanup(func() {
		hook.Reset()
	})

	if err := service.StartAutoRefresh(ctx); err != nil {
		t.Fatalf("StartAutoRefresh returned error: %v", err)
	}

	assertNoAutoRefreshErrorLog(t, hook, "quota auto refresh failed")
}

func TestStartAutoRefreshRunsAfterInitialConfiguredDelay(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{}, 1)
	handler := &refreshHandlerStub{
		output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}},
		onCheck: func() {
			select {
			case called <- struct{}{}:
			default:
			}
			cancel()
		},
	}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.SetRefreshContext(ctx)
	setRefreshCooldown(service, func(time.Duration) {})
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.Local)
	setAutoRefreshNow(service, func() time.Time { return now })
	var delays []time.Duration
	setAutoRefreshDelay(service, func(ctx context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		now = now.Add(delay)
		return true
	})
	if _, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 1},
	}); err != nil {
		t.Fatalf("UpdateAutoRefreshSettings returned error: %v", err)
	}
	select {
	case <-autoRefreshSettingsChanged(service):
	default:
	}

	err := service.StartAutoRefresh(ctx)
	service.WaitRefreshTasks()
	if err != nil {
		t.Fatalf("StartAutoRefresh returned error: %v", err)
	}
	select {
	case <-called:
	default:
		t.Fatal("expected scheduler to run refresh after the initial configured delay")
	}
	if len(delays) == 0 || delays[0] != time.Minute {
		t.Fatalf("expected first scheduler delay to be 1m, got %v", delays)
	}
}

func TestStartAutoRefreshSkipsWhenScheduleIsMissing(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan struct{}, 1)
	handler := &refreshHandlerStub{
		output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}},
		onCheck: func() {
			select {
			case called <- struct{}{}:
			default:
			}
			cancel()
		},
	}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.SetRefreshContext(ctx)
	setRefreshCooldown(service, func(time.Duration) {})
	delayCalls := 0
	setAutoRefreshDelay(service, func(context.Context, time.Duration) bool {
		delayCalls++
		if delayCalls == 1 {
			return true
		}
		cancel()
		return false
	})

	if err := service.StartAutoRefresh(ctx); err != nil {
		t.Fatalf("StartAutoRefresh returned error: %v", err)
	}
	service.WaitRefreshTasks()
	select {
	case <-called:
		t.Fatal("expected missing auto refresh schedule to skip the scheduled refresh task")
	default:
	}
	if delayCalls != 2 {
		t.Fatalf("expected scheduler to re-check settings after missing schedule, got %d delay calls", delayCalls)
	}
	if lastRoundAt := lastAutoRefreshRoundAt(service); !lastRoundAt.IsZero() {
		t.Fatalf("expected missing auto refresh schedule not to start a round, got last round at %s", lastRoundAt)
	}
}

func TestStartAutoRefreshWakesWhenSettingsChange(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan struct{}, 1)
	handler := &refreshHandlerStub{
		output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}},
		onCheck: func() {
			select {
			case called <- struct{}{}:
			default:
			}
			cancel()
		},
	}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.SetRefreshContext(ctx)
	setRefreshCooldown(service, func(time.Duration) {})
	var nowMu sync.RWMutex
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.Local)
	setAutoRefreshNow(service, func() time.Time {
		nowMu.RLock()
		defer nowMu.RUnlock()
		return now
	})
	if _, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitHour, Value: 24},
	}); err != nil {
		t.Fatalf("UpdateAutoRefreshSettings initial returned error: %v", err)
	}
	select {
	case <-autoRefreshSettingsChanged(service):
	default:
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- service.StartAutoRefresh(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(time.Second):
			t.Fatal("expected auto refresh scheduler to exit after cleanup cancel")
		}
		service.WaitRefreshTasks()
	})

	select {
	case <-called:
		t.Fatal("expected long day schedule not to run before settings change")
	case <-time.After(50 * time.Millisecond):
	}
	nowMu.Lock()
	now = time.Date(2026, 5, 26, 23, 59, 59, int(999*time.Millisecond), time.Local)
	nowMu.Unlock()
	if _, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitDay, Value: 1},
	}); err != nil {
		t.Fatalf("UpdateAutoRefreshSettings shortened returned error: %v", err)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("expected settings change to wake scheduler before the old long delay")
	}
}

func TestRunAutoRefreshQueuesOnlyActiveAuthFiles(t *testing.T) {
	db := openQuotaTestDatabase(t)
	disabled := true
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "  ", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "disabled-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile, Disabled: &disabled})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "deleted-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile, IsDeleted: true})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "provider-1", Provider: "openai", Type: "openai", AuthType: entities.UsageIdentityAuthTypeAIProvider})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "unsupported-1", Provider: "vertex", Type: "vertex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	setRefreshCooldown(service, func(time.Duration) {})

	if err := service.RunAutoRefresh(context.Background()); err != nil {
		t.Fatalf("RunAutoRefresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	service.WaitRefreshTasks()
	if handler.callCount() != 1 {
		t.Fatalf("expected only active auth file to refresh, got %d calls", handler.callCount())
	}
	for _, authIndex := range []string{"disabled-1", "deleted-1", "provider-1", "unsupported-1"} {
		if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), authIndex); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("expected %s to stay out of auto refresh queue, got %v", authIndex, err)
		}
	}
	_, hasBlankTask := refreshTasks(service)["  "]
	if hasBlankTask {
		t.Fatal("expected blank identity to stay out of auto refresh queue")
	}
}

func TestRunAutoRefreshRunsWithoutFrontendActivation(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-2", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	setRefreshCooldown(service, func(time.Duration) {})

	if err := service.RunAutoRefresh(context.Background()); err != nil {
		t.Fatalf("RunAutoRefresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	waitForRefreshTask(t, service, "auth-2", RefreshTaskStatusCompleted)
	service.WaitRefreshTasks()
	if handler.callCount() != 2 {
		t.Fatalf("expected backend scheduled refresh to refresh all auth files, got %d calls", handler.callCount())
	}
}

func TestRunAutoRefreshStoresAliasDisplayName(t *testing.T) {
	db := openQuotaTestDatabase(t)
	alias := "Friendly Alias"
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Alias: &alias, Name: "Upstream Name", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	setRefreshCooldown(service, func(time.Duration) {})

	if err := service.RunAutoRefresh(context.Background()); err != nil {
		t.Fatalf("RunAutoRefresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	service.WaitRefreshTasks()

	task := refreshTaskRecord(service, "auth-1")
	if task == nil || task.Name != "Friendly Alias" {
		t.Fatalf("expected auto refresh task name to use alias display name, got %+v", task)
	}
}

func TestRunAutoRefreshSkipsWhenPreviousAutoRoundIsActive(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	setRefreshCooldown(service, func(time.Duration) {})
	hook := logrustest.NewGlobal()
	t.Cleanup(func() {
		hook.Reset()
	})

	if err := service.RunAutoRefresh(context.Background()); err != nil {
		t.Fatalf("first RunAutoRefresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusRunning)
	if err := service.RunAutoRefresh(context.Background()); err != nil {
		t.Fatalf("second RunAutoRefresh returned error: %v", err)
	}

	assertAutoRefreshRoundLogs(t, hook, 1, 0)
	close(block)
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	service.WaitRefreshTasks()
	assertAutoRefreshRoundLogs(t, hook, 1, 1)
}

func TestRunAutoRefreshSkipsCachedHTTPFailures(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{err: ProviderHTTPError{StatusCode: 401, Message: "expired token"}}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	setRefreshCooldown(service, func(time.Duration) {})

	first, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, first.Tasks[0].AuthIndex, RefreshTaskStatusFailed)
	if handler.callCount() != 1 {
		t.Fatalf("expected one manual provider call, got %d", handler.callCount())
	}
	if err := service.RunAutoRefresh(context.Background()); err != nil {
		t.Fatalf("RunAutoRefresh returned error: %v", err)
	}
	if handler.callCount() != 1 {
		t.Fatalf("expected auto refresh to skip cached 401, got %d calls", handler.callCount())
	}
}

func TestRunAutoRefreshLogsRoundStartAndEndOnce(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	setRefreshCooldown(service, func(time.Duration) {})
	hook := logrustest.NewGlobal()
	t.Cleanup(func() {
		hook.Reset()
	})

	if err := service.RunAutoRefresh(context.Background()); err != nil {
		t.Fatalf("RunAutoRefresh returned error: %v", err)
	}

	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	service.WaitRefreshTasks()
	assertAutoRefreshRoundLogs(t, hook, 1, 1)
}

func TestRunAutoRefreshLogsRoundEndWhenIdentityScanFails(t *testing.T) {
	db := openQuotaTestDatabase(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db returned error: %v", err)
	}
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	hook := logrustest.NewGlobal()
	t.Cleanup(func() {
		hook.Reset()
	})

	if err := service.RunAutoRefresh(context.Background()); err == nil {
		t.Fatal("expected RunAutoRefresh to return scan error")
	}

	assertAutoRefreshRoundLogs(t, hook, 1, 1)
	lastRoundAt := lastAutoRefreshRoundAt(service)
	lastAttemptAt := lastAutoRefreshAttemptAt(service)
	if !lastRoundAt.IsZero() {
		t.Fatalf("expected failed identity scan not to update last auto refresh round time, got %v", lastRoundAt)
	}
	if lastAttemptAt.IsZero() {
		t.Fatal("expected failed identity scan to record last auto refresh attempt time for backoff")
	}
}

func assertAutoRefreshRoundLogs(t *testing.T, hook *logrustest.Hook, wantStart int, wantEnd int) {
	t.Helper()
	startLogs := 0
	endLogs := 0
	for _, entry := range hook.AllEntries() {
		if entry.Level != logrus.InfoLevel {
			continue
		}
		switch entry.Message {
		case "quota auto refresh round started":
			startLogs++
		case "quota auto refresh round completed":
			endLogs++
		}
	}
	if startLogs != wantStart || endLogs != wantEnd {
		t.Fatalf("expected start=%d end=%d info logs, got start=%d end=%d entries=%+v", wantStart, wantEnd, startLogs, endLogs, hook.AllEntries())
	}
}

func assertNoAutoRefreshErrorLog(t *testing.T, hook *logrustest.Hook, messagePrefix string) {
	t.Helper()
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.ErrorLevel && strings.HasPrefix(entry.Message, messagePrefix) {
			t.Fatalf("expected no %q error log, got entries=%+v", messagePrefix, hook.AllEntries())
		}
	}
}

func TestNextAutoRefreshDelayUsesLastRoundTime(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.Local)
	setLastAutoRefreshRoundAt(service, now.Add(-4*time.Minute))

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{Enabled: true, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 5}}, now)

	if delay != time.Minute {
		t.Fatalf("expected next auto refresh delay 1m after recent scheduled round, got %s", delay)
	}
}

func TestNextAutoRefreshDelayUsesLastAttemptAfterScanFailure(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.Local)
	setLastAutoRefreshAttemptAt(service, now.Add(-4*time.Minute))

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{Enabled: true, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 5}}, now)

	if delay != time.Minute {
		t.Fatalf("expected next auto refresh delay 1m after failed attempt, got %s", delay)
	}
}

func TestNextAutoRefreshDelayRetriesSoonAfterScanFailureForDaySchedule(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 10, 30, 0, 0, time.Local)
	setLastAutoRefreshAttemptAt(service, now)

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{Enabled: true, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitDay, Value: 1}}, now)

	if delay != time.Minute {
		t.Fatalf("expected day schedule to retry soon after failed scan, got %s", delay)
	}
}

func TestNextAutoRefreshDelayWaitsForNextTriggerWhenRoundIsRunning(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.Local)
	setAutoRefreshRunning(service, true)
	setLastAutoRefreshRoundAt(service, now.Add(-5*time.Minute))

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{Enabled: true, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 5}}, now)

	if delay != time.Minute {
		t.Fatalf("expected running round at due time to recheck soon, got %s", delay)
	}
}

func TestNextAutoRefreshDelaySkipsWhenSettingsDisabledOrEmpty(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.Local)

	for _, settings := range []AutoRefreshSettings{
		{Enabled: false, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 5}},
		{Enabled: true, Schedule: nil},
	} {
		if delay := nextAutoRefreshDelay(service, settings, now); delay <= 0 {
			t.Fatalf("expected disabled or empty settings to wait before rechecking, got %s for %+v", delay, settings)
		}
	}
}

func TestNextAutoRefreshDelayUsesProjectTimezoneMidnightForDaySchedule(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 10, 30, 0, 0, time.Local)

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{Enabled: true, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitDay, Value: 1}}, now)

	want := time.Date(2026, 5, 27, 0, 0, 0, 0, time.Local).Sub(now)
	if delay != want {
		t.Fatalf("expected day schedule to wait until next local midnight, got %s want %s", delay, want)
	}
}

func TestNextAutoRefreshDelayUsesNextMidnightForFirstDayScheduleRun(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 23, 55, 0, 0, time.Local)

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{Enabled: true, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitDay, Value: 30}}, now)

	want := time.Date(2026, 5, 27, 0, 0, 0, 0, time.Local).Sub(now)
	if delay != want {
		t.Fatalf("expected first day schedule run to happen at next local midnight, got %s want %s", delay, want)
	}
}

func TestNextAutoRefreshDelayUsesWeekdayAtProjectTimezoneMidnight(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	now := time.Date(2026, 5, 25, 10, 30, 0, 0, time.Local)

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{Enabled: true, Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitWeek, Value: 2}}, now)

	want := time.Date(2026, 5, 26, 0, 0, 0, 0, time.Local).Sub(now)
	if delay != want {
		t.Fatalf("expected weekly schedule value 2 to mean Tuesday local midnight, got %s want %s", delay, want)
	}
}

func TestRefreshCacheableHTTPStatusCodesAlsoControlAutoRefreshSkip(t *testing.T) {
	if _, ok := RefreshCacheableHTTPStatusCodes[401]; !ok {
		t.Fatal("expected 401 to be configured as cacheable and auto-refresh-skipped")
	}
}
