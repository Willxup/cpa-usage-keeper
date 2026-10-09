package test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

func TestDeleteCodexQuotaHistoryCycleClearsActualDeferredCycle(t *testing.T) {
	db := openQuotaTestDatabase(t)
	auth := "diverged-history-auth"
	seedUsageIdentity(t, db, codexHistoryUsageIdentity(auth))
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	weeklyReset := base.Add(7 * 24 * time.Hour)
	weeklySnapshot := func(at time.Time, remaining int) UsageHeaderSnapshot {
		return codexUsageHeaderSnapshotWithHeaders(auth, at, http.Header{
			"X-Codex-Primary-Used-Percent":   []string{strconv.Itoa(100 - remaining)},
			"X-Codex-Primary-Window-Minutes": []string{"10080"},
			"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(weeklyReset.Unix(), 10)},
		})
	}
	weekly := weeklySnapshot(base, 33)
	fiveHour := codexHistoryPrimarySnapshot(auth, base.Add(time.Minute), 80, base.Add(5*time.Hour))
	for _, snapshot := range []UsageHeaderSnapshot{weekly, fiveHour} {
		if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, snapshot.MainQuotaObservations); err != nil {
			t.Fatal(err)
		}
	}
	cycles := loadCodexQuotaCycles(t, db, auth)
	var weeklyID, fiveHourID int64
	for _, cycle := range cycles {
		if cycle.WindowSeconds == 604800 {
			weeklyID = cycle.ID
		} else if cycle.WindowSeconds == 18000 {
			fiveHourID = cycle.ID
		}
	}
	if weeklyID == 0 || fiveHourID == 0 {
		t.Fatalf("missing initial cycles: %+v", cycles)
	}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour, CodexQuotaHistoryFlushInterval: time.Hour,
		CodexQuotaHistoryHeartbeatInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest(),
	})
	t.Cleanup(service.StopRefreshTasks)
	timers := make(chan usageHeaderManualTimer, 4)
	setCodexQuotaHistoryTimerFactory(service, func(time.Duration) (<-chan time.Time, func()) {
		timer := usageHeaderManualTimer{fire: make(chan time.Time, 1)}
		timers <- timer
		return timer.fire, func() {}
	})
	writes := make(chan error, 4)
	setCodexQuotaHistoryWriter(service, func(ctx context.Context, writerDB *gorm.DB, observations []repositorydto.CodexMainQuotaObservation) error {
		err := repository.WriteCodexMainQuotaObservations(ctx, writerDB, observations)
		writes <- err
		return err
	})
	// 跨窗口 Header 回升被 writer 无错误忽略，但 runner 已接受 Weekly 比较状态。
	rebound := weeklySnapshot(base.Add(2*time.Minute), 100)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(rebound))
	waitForCodexQuotaHistoryManualTimer(t, timers).fire <- time.Now()
	select {
	case err := <-writes:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected ignored Weekly rebound to finish its writer call")
	}
	stable := weeklySnapshot(base.Add(3*time.Minute), 100)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(stable))
	waitForCodexQuotaHistoryManualTimer(t, timers).fire <- time.Now()
	deadline := time.Now().Add(time.Second)
	for codexQuotaHistoryHeaderQueueLength(service) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.DeleteCodexQuotaHistoryCycle(ctx, auth, weeklyID); err != nil {
		t.Fatal(err)
	}
	service.StopRefreshTasks()
	remaining := loadCodexQuotaCycles(t, db, auth)
	if len(remaining) != 1 || remaining[0].ID != fiveHourID {
		t.Fatalf("deleted Weekly cycle was recreated by deferred data: %+v", remaining)
	}
}

func TestDeleteCodexQuotaHistoryCycleAllowsObservationsReceivedAfterDeletion(t *testing.T) {
	db := openQuotaTestDatabase(t)
	auth := "late-after-delete-auth"
	seedUsageIdentity(t, db, codexHistoryUsageIdentity(auth))
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	snapshot := codexHistoryPrimarySnapshot(auth, base, 90, base.Add(5*time.Hour))
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, snapshot.MainQuotaObservations); err != nil {
		t.Fatal(err)
	}
	cycle := loadCodexQuotaCycles(t, db, auth)[0]
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour, CodexQuotaHistoryFlushInterval: time.Hour,
		PricingCatalog: emptyPricingCatalogForTest(),
	})
	t.Cleanup(service.StopRefreshTasks)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.DeleteCodexQuotaHistoryCycle(ctx, auth, cycle.ID); err != nil {
		t.Fatal(err)
	}
	// 删除只清当时已有数据；之后送达的采样即使观察时间较早，也继续走正常记录逻辑。
	late := codexHistoryPrimarySnapshot(auth, base.Add(time.Minute), 89, cycle.ResetAt)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(late))
	service.StopRefreshTasks()
	remaining := loadCodexQuotaCycles(t, db, auth)
	if len(remaining) != 1 || remaining[0].ID == cycle.ID {
		t.Fatalf("later delivery was blocked by a deletion marker: %+v", remaining)
	}
	segments := loadCodexQuotaSegments(t, db, remaining[0].ID)
	if len(segments) != 1 || segments[0].RemainingPercent != 89 {
		t.Fatalf("later observation not recorded normally: %+v", segments)
	}
}

func TestDeleteCodexQuotaHistoryCyclePreservesQueuedTrustedRefresh(t *testing.T) {
	for _, source := range []RefreshSource{RefreshSourceManual, RefreshSourceScheduled, RefreshSourceInspection} {
		t.Run(string(source), func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			auth := "trusted-delete-auth"
			seedUsageIdentity(t, db, codexHistoryUsageIdentity(auth))
			base := time.Now().Add(-time.Hour).Truncate(time.Second)
			resetAt := base.Add(5 * time.Hour)
			snapshot := codexHistoryPrimarySnapshot(auth, base, 90, resetAt)
			if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, snapshot.MainQuotaObservations); err != nil {
				t.Fatal(err)
			}
			cycle := loadCodexQuotaCycles(t, db, auth)[0]
			handler := &recordingProviderHandler{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
				RateLimit: &CodexRateLimitInfo{PrimaryWindow: codexHistoryUsageWindow(11, 18000, resetAt)},
			}}}}
			service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour, CodexQuotaHistoryFlushInterval: time.Hour,
				PricingCatalog: emptyPricingCatalogForTest(),
			})
			t.Cleanup(service.StopRefreshTasks)
			writes := make(chan error, 1)
			setCodexQuotaHistoryWriter(service, func(ctx context.Context, writerDB *gorm.DB, observations []repositorydto.CodexMainQuotaObservation) error {
				err := repository.WriteCodexMainQuotaObservations(ctx, writerDB, observations)
				writes <- err
				return err
			})
			// 在删除尚未完成时送入真实 Check 结果，固定可信事实已排队、runner 尚未处理的交错。
			callbackName := "test:refresh_during_quota_cycle_delete"
			if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement.Table == "quota_cycles" {
					_, err := service.Check(tx.Statement.Context, CheckRequest{AuthIndex: auth, Source: source})
					tx.AddError(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer func() {
				service.StopRefreshTasks()
				_ = db.Callback().Delete().Remove(callbackName)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := service.DeleteCodexQuotaHistoryCycle(ctx, auth, cycle.ID); err != nil {
				t.Fatal(err)
			}
			// 不触发 Header timer 或停机刷新；可信队列原有的唤醒通知应让结果立即正常落库。
			select {
			case err := <-writes:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("queued trusted refresh was not recorded after deletion")
			}
			cycles := loadCodexQuotaCycles(t, db, auth)
			if len(cycles) != 1 || cycles[0].ID == cycle.ID || !cycles[0].ResetAt.Equal(resetAt) {
				t.Fatalf("trusted refresh did not establish a fresh cycle: %+v", cycles)
			}
			segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
			if len(segments) != 1 || segments[0].RemainingPercent != 89 || segments[0].ObservationCount != 1 {
				t.Fatalf("trusted refresh was changed by deletion: %+v", segments)
			}
		})
	}
}

func TestDeleteCodexQuotaHistoryCyclePreservesCalibratedNeighbor(t *testing.T) {
	for _, arrival := range []string{"queued", "late"} {
		t.Run(arrival, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			auth := "neighbor-auth"
			seedUsageIdentity(t, db, codexHistoryUsageIdentity(auth))
			base := time.Now().Add(-time.Hour).Truncate(time.Second)
			reset := base.Add(5 * time.Hour)
			old := codexHistoryPrimarySnapshot(auth, base, 60, reset)
			neighbor := codexHistoryPrimarySnapshot(auth, base.Add(time.Minute), 90, reset.Add(3*time.Minute))
			calibrated := codexHistoryPrimarySnapshot(auth, base.Add(2*time.Minute), 90, reset.Add(90*time.Second))
			calibrated.MainQuotaObservations[0].Authoritative = true
			for _, snapshot := range []UsageHeaderSnapshot{old, neighbor, calibrated} {
				if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, snapshot.MainQuotaObservations); err != nil {
					t.Fatal(err)
				}
			}
			cycles := loadCodexQuotaCycles(t, db, auth)
			if len(cycles) != 2 || !cycles[1].ResetAt.Equal(calibrated.MainQuotaObservations[0].ResetAt) {
				t.Fatalf("expected two separate calibrated cycles: %+v", cycles)
			}
			service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour, CodexQuotaHistoryFlushInterval: time.Hour,
				CodexQuotaHistoryHeartbeatInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest(),
			})
			t.Cleanup(service.StopRefreshTasks)
			timers := make(chan usageHeaderManualTimer, 4)
			setCodexQuotaHistoryTimerFactory(service, func(time.Duration) (<-chan time.Time, func()) {
				timer := usageHeaderManualTimer{fire: make(chan time.Time, 1)}
				timers <- timer
				return timer.fire, func() {}
			})
			stableAt := base.Add(3 * time.Minute)
			stable := codexHistoryPrimarySnapshot(auth, stableAt, 90, cycles[1].ResetAt)
			service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(stable))
			waitForCodexQuotaHistoryManualTimer(t, timers).fire <- time.Now()
			deadline := time.Now().Add(time.Second)
			for codexQuotaHistoryHeaderQueueLength(service) != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			// 命令与采样串行：这个无效删除返回后，上一批稳定尾段已进入 runner 内存。
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := service.DeleteCodexQuotaHistoryCycle(ctx, auth, 99999); !errors.Is(err, ErrNotFound) {
				t.Fatalf("runner barrier: %v", err)
			}
			dropAt := base.Add(4 * time.Minute)
			drop := codexHistoryPrimarySnapshot(auth, dropAt, 89, cycles[1].ResetAt)
			if arrival == "queued" {
				service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(drop))
				waitForCodexQuotaHistoryManualTimer(t, timers)
			}
			if err := service.DeleteCodexQuotaHistoryCycle(ctx, auth, cycles[0].ID); err != nil {
				t.Fatal(err)
			}
			if arrival == "late" {
				service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(drop))
			}
			service.StopRefreshTasks()
			remaining := loadCodexQuotaCycles(t, db, auth)
			segments := loadCodexQuotaSegments(t, db, cycles[1].ID)
			if len(remaining) != 1 || remaining[0].ID != cycles[1].ID || len(segments) != 2 {
				t.Fatalf("neighbor history lost: cycles=%+v segments=%+v", remaining, segments)
			}
			if segments[0].ObservationCount != 3 || !segments[0].LastObservedAt.Equal(stableAt) || segments[1].RemainingPercent != 89 || !segments[1].FirstObservedAt.Equal(dropAt) {
				t.Fatalf("neighbor pending observations changed: %+v", segments)
			}
		})
	}
}

func TestDeleteCodexQuotaHistoryCycleRestoresOldCycleWithoutRestart(t *testing.T) {
	db := openQuotaTestDatabase(t)
	auth := "delete-history-auth"
	seedUsageIdentity(t, db, codexHistoryUsageIdentity(auth))
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	oldReset, badReset := base.Add(5*time.Hour), base.Add(6*time.Hour)
	old := codexHistoryPrimarySnapshot(auth, base, 33, oldReset)
	bad := codexHistoryPrimarySnapshot(auth, base.Add(time.Minute), 100, badReset)
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, append(old.MainQuotaObservations, bad.MainQuotaObservations...)); err != nil {
		t.Fatal(err)
	}
	cycles := loadCodexQuotaCycles(t, db, auth)
	oldID, badID := cycles[0].ID, cycles[1].ID
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour, CodexQuotaHistoryFlushInterval: time.Hour,
		PricingCatalog: emptyPricingCatalogForTest(),
	})
	t.Cleanup(service.StopRefreshTasks)
	timers := make(chan usageHeaderManualTimer, 10)
	setCodexQuotaHistoryTimerFactory(service, func(time.Duration) (<-chan time.Time, func()) {
		timer := usageHeaderManualTimer{fire: make(chan time.Time, 1)}
		timers <- timer
		return timer.fire, func() {}
	})
	// 先让 runner 恢复错误周期并缓存尚未物化的稳定尾段。
	stable := codexHistoryPrimarySnapshot(auth, base.Add(2*time.Minute), 100, badReset)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(stable))
	waitForCodexQuotaHistoryManualTimer(t, timers).fire <- time.Now()
	deadline := time.Now().Add(time.Second)
	for codexQuotaHistoryHeaderQueueLength(service) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	// 删除时已在队列里的错误 Primary 应清掉，同一不可变快照的 Secondary 应保留。
	queued := codexHistoryMainWindowsSnapshot(auth, base.Add(3*time.Minute), 100, badReset, 70, oldReset)
	otherAuth := "other-queued-auth"
	seedUsageIdentity(t, db, codexHistoryUsageIdentity(otherAuth))
	otherQueued := codexHistoryPrimarySnapshot(otherAuth, base.Add(3*time.Minute), 50, badReset)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(queued, otherQueued))
	queuedTimer := waitForCodexQuotaHistoryManualTimer(t, timers)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.DeleteCodexQuotaHistoryCycle(ctx, auth, badID); err != nil {
		t.Fatal(err)
	}
	restored := codexHistoryPrimarySnapshot(auth, time.Now().Add(time.Second), 32, oldReset)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(restored))
	queuedTimer.fire <- time.Now()
	if len(queued.MainQuotaObservations) != 2 || queued.MainQuotaObservations[0].RemainingPercent != 100 {
		t.Fatal("deletion mutated the shared quota snapshot")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		segments := loadCodexQuotaSegments(t, db, oldID)
		if len(segments) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	segments := loadCodexQuotaSegments(t, db, oldID)
	if len(segments) != 2 || segments[1].RemainingPercent != 32 {
		t.Fatalf("old cycle did not resume: %+v", segments)
	}
	for _, cycle := range loadCodexQuotaCycles(t, db, auth) {
		if cycle.ID == badID {
			t.Fatal("deleted cycle returned")
		}
	}
	// 删除是清理已有事实，不永久屏蔽之后的新观察。
	fresh := codexHistoryPrimarySnapshot(auth, time.Now().Add(2*time.Second), 99, badReset)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(fresh))
	service.StopRefreshTasks()
	cycles = loadCodexQuotaCycles(t, db, auth)
	if len(cycles) != 3 {
		t.Fatalf("expected restored primary, secondary and fresh cycle: %+v", cycles)
	}
	otherCycles := loadCodexQuotaCycles(t, db, otherAuth)
	if len(otherCycles) != 1 {
		t.Fatalf("other account's matching-reset queue data was removed: %+v", otherCycles)
	}
	otherSegments := loadCodexQuotaSegments(t, db, otherCycles[0].ID)
	if len(otherSegments) != 1 || otherSegments[0].RemainingPercent != 50 {
		t.Fatalf("other account's observation changed: %+v", otherSegments)
	}
	for _, cycle := range cycles {
		if cycle.ResetAt.Equal(badReset) && cycle.QuotaKey == "rate_limit.primary_window" && cycle.ID == badID {
			t.Fatal("old parent was reused")
		}
	}
}

func TestDeleteCodexQuotaHistoryCycleValidatesIdentityAndShutdown(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("delete-auth"))
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
	t.Cleanup(service.StopRefreshTasks)
	if err := service.DeleteCodexQuotaHistoryCycle(context.Background(), "", 1); !errors.Is(err, ErrValidation) {
		t.Fatalf("validation: %v", err)
	}
	if err := service.DeleteCodexQuotaHistoryCycle(context.Background(), "missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("identity: %v", err)
	}
	if err := service.DeleteCodexQuotaHistoryCycle(context.Background(), "delete-auth", 123); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing cycle: %v", err)
	}
	service.StopRefreshTasks()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.DeleteCodexQuotaHistoryCycle(ctx, "delete-auth", 123); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestDeleteCodexQuotaHistoryCycleInterruptsHeaderWait(t *testing.T) {
	db := openQuotaTestDatabase(t)
	auth := "delete-wait-auth"
	seedUsageIdentity(t, db, codexHistoryUsageIdentity(auth))
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	snapshot := codexHistoryPrimarySnapshot(auth, base, 80, base.Add(5*time.Hour))
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, snapshot.MainQuotaObservations); err != nil {
		t.Fatal(err)
	}
	cycle := loadCodexQuotaCycles(t, db, auth)[0]
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour, CodexQuotaHistoryFlushInterval: time.Hour,
		PricingCatalog: emptyPricingCatalogForTest(),
	})
	t.Cleanup(service.StopRefreshTasks)
	timers := make(chan usageHeaderManualTimer, 1)
	setCodexQuotaHistoryTimerFactory(service, func(time.Duration) (<-chan time.Time, func()) {
		timer := usageHeaderManualTimer{fire: make(chan time.Time, 1)}
		timers <- timer
		return timer.fire, func() {}
	})
	queued := codexHistoryPrimarySnapshot(auth, base.Add(time.Minute), 79, cycle.ResetAt)
	service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(queued))
	waitForCodexQuotaHistoryManualTimer(t, timers) // 不触发 timer，删除仍须立即完成。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.DeleteCodexQuotaHistoryCycle(ctx, auth, cycle.ID); err != nil {
		t.Fatal(err)
	}
	service.StopRefreshTasks()
	if cycles := loadCodexQuotaCycles(t, db, auth); len(cycles) != 0 {
		t.Fatalf("queued observation recreated deleted cycle: %+v", cycles)
	}
}
