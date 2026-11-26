package poller_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

func TestUsageAggregationRunnerSchedulesQuotaHistoryCatchUpAndBackfill(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	// 写入一个带降档的周期与 2 条 OAuth 事件。
	resetAt := now.Add(2 * time.Hour)
	for _, obs := range []repositorydto.CodexMainQuotaObservation{
		{
			Provider:         "codex",
			AuthIndex:        "auth-q1",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 90,
			FirstObservedAt:  now.Add(-2 * time.Hour),
			LastObservedAt:   now.Add(-2 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-q1",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 85,
			FirstObservedAt:  now.Add(-time.Hour),
			LastObservedAt:   now.Add(-time.Hour),
			ObservationCount: 1,
		},
	} {
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{obs}); err != nil {
			t.Fatalf("write quota observation: %v", err)
		}
	}

	cost := 0.20
	costAvailable := true
	events := []entities.UsageEvent{
		{
			EventKey:      "q-evt-1",
			APIGroupKey:   "key-1",
			Provider:      "codex",
			AuthType:      "oauth",
			AuthIndex:     "auth-q1",
			Model:         "gpt-5",
			Timestamp:     now.Add(-90 * time.Minute),
			TotalTokens:   2000,
			CostUSD:       &cost,
			CostAvailable: &costAvailable,
		},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}

	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{
		NowFunc: func() time.Time { return now },
	})

	// 执行轮转直到 quota_history turn 完成。
	sawQuotaTurn := false
	for i := 0; i < 6; i++ {
		res, err := runner.RunOnce(ctx)
		if err != nil {
			t.Fatalf("run once %d: %v", i, err)
		}
		if res.Kind == poller.UsageAggregationKindQuotaHistory && res.Processed {
			sawQuotaTurn = true
			break
		}
	}
	if !sawQuotaTurn {
		t.Fatal("expected runner to execute quota_history turn on startup")
	}

	var cycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-q1").Take(&cycle).Error; err != nil {
		t.Fatalf("load quota cycle: %v", err)
	}
	if !cycle.UsageReady || cycle.Dirty || cycle.Requests != 1 || cycle.TotalTokens != 2000 {
		t.Fatalf("unexpected persisted quota cycle after runner catch-up: %+v", cycle)
	}

	// 新事件到达并通知后，RunOnce 应立刻重算进行中周期（不卡节流等待）。
	events2 := []entities.UsageEvent{
		{
			EventKey:      "q-evt-2",
			APIGroupKey:   "key-1",
			Provider:      "codex",
			AuthType:      "oauth",
			AuthIndex:     "auth-q1",
			Model:         "gpt-5",
			Timestamp:     now.Add(-30 * time.Minute),
			TotalTokens:   3000,
			CostUSD:       &cost,
			CostAvailable: &costAvailable,
		},
	}
	if _, _, err := repository.InsertUsageEvents(db, events2); err != nil {
		t.Fatalf("insert second usage event: %v", err)
	}
	var latestEvent entities.UsageEvent
	if err := db.Order("id DESC").Take(&latestEvent).Error; err != nil {
		t.Fatalf("load latest event: %v", err)
	}
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{latestEvent})

	for i := 0; i < 6; i++ {
		if _, err := runner.RunOnce(ctx); err != nil {
			t.Fatalf("run once after second event %d: %v", i, err)
		}
	}
	if err := db.Where("auth_index = ?", "auth-q1").Take(&cycle).Error; err != nil {
		t.Fatalf("reload quota cycle: %v", err)
	}
	if cycle.Requests != 2 || cycle.TotalTokens != 5000 || cycle.Dirty {
		t.Fatalf("expected RunOnce to recompute active cycle immediately without throttle wait: %+v", cycle)
	}
}

func TestUsageAggregationRunnerQuotaHistoryYieldsToInboxAndPause(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-inbox",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          now.Add(2 * time.Hour),
		RemainingPercent: 95,
		FirstObservedAt:  now.Add(-time.Hour),
		LastObservedAt:   now.Add(-time.Hour),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write observation: %v", err)
	}

	// 插入待处理 inbox，验证限额历史汇总主动让路。
	if _, err := repository.InsertRedisUsageInboxRawMessages(db, "redis_pull:usage", []string{"{}"}, now); err != nil {
		t.Fatalf("insert inbox message: %v", err)
	}

	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{
		NowFunc: func() time.Time { return now },
	})
	res, err := runner.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run once with inbox: %v", err)
	}
	if !res.DeferredForInbox {
		t.Fatalf("expected runner to defer for pending inbox, got %+v", res)
	}

	// 验证 Pause 门禁同时阻止限额历史 turn。
	resume, err := runner.Pause(ctx)
	if err != nil {
		t.Fatalf("pause runner: %v", err)
	}
	defer resume()

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := runner.RunOnce(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected paused runner RunOnce to block until context deadline, got %v", err)
	}
}

type stubQuotaHistoryNotifier struct {
	calls atomic.Int64
}

func (n *stubQuotaHistoryNotifier) NotifyQuotaHistoryChanged() {
	n.calls.Add(1)
}

func TestQuotaServiceNotifiesRunnerOnDeleteCycle(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	activeStart := now.Add(-24 * time.Hour)
	identity := entities.UsageIdentity{
		Identity:     "auth-del",
		LookupKey:    "credential:auth-del",
		AuthType:     entities.UsageIdentityAuthTypeCredential,
		AuthTypeName: "credential",
		Type:         "codex",
		Provider:     "codex",
		Name:         "codex-account",
		ActiveStart:  &activeStart,
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("create identity: %v", err)
	}
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-del",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          now.Add(2 * time.Hour),
		RemainingPercent: 90,
		FirstObservedAt:  now.Add(-time.Hour),
		LastObservedAt:   now.Add(-time.Hour),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write observation: %v", err)
	}
	var cycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-del").Take(&cycle).Error; err != nil {
		t.Fatalf("load cycle: %v", err)
	}

	notifier := &stubQuotaHistoryNotifier{}
	svc := quota.NewServiceWithOptions(db, nil, quota.ServiceOptions{
		QuotaHistoryChangeNotifier: notifier,
	})
	defer svc.StopRefreshTasks()

	if err := svc.DeleteCodexQuotaHistoryCycle(ctx, "auth-del", cycle.ID); err != nil {
		t.Fatalf("delete quota cycle: %v", err)
	}
	if notifier.calls.Load() != 1 {
		t.Fatalf("expected NotifyQuotaHistoryChanged to be called once after delete, got %d", notifier.calls.Load())
	}
}

func TestUsageAggregationRunnerQuotaHistoryObservationBypassesActiveThrottle(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)

	for _, obs := range []repositorydto.CodexMainQuotaObservation{
		{
			Provider:         "codex",
			AuthIndex:        "auth-throttle",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 90,
			FirstObservedAt:  now.Add(-2 * time.Hour),
			LastObservedAt:   now.Add(-2 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-throttle",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 83,
			FirstObservedAt:  now.Add(-time.Hour),
			LastObservedAt:   now.Add(-time.Hour),
			ObservationCount: 1,
		},
	} {
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{obs}); err != nil {
			t.Fatalf("write initial observation: %v", err)
		}
	}

	cost := 0.25
	costAvailable := true
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:      "throttle-evt-1",
		APIGroupKey:   "key-1",
		Provider:      "codex",
		AuthType:      "oauth",
		AuthIndex:     "auth-throttle",
		Model:         "gpt-5",
		Timestamp:     now.Add(-90 * time.Minute),
		TotalTokens:   2000,
		CostUSD:       &cost,
		CostAvailable: &costAvailable,
	}}); err != nil {
		t.Fatalf("insert first usage event: %v", err)
	}

	// 固定时钟在 20 秒节流窗口内，验证纯事件触发被节流，而新降档观测通知立刻解除节流并重算。
	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{
		DebounceInterval:           20 * time.Millisecond,
		QuotaHistoryActiveThrottle: 20 * time.Second,
		NowFunc:                    func() time.Time { return now },
	})
	stop := startUsageAggregationTestRunner(t, runner)

	loadCycle := func() entities.QuotaCycle {
		var cycle entities.QuotaCycle
		if err := db.Where("auth_index = ?", "auth-throttle").Take(&cycle).Error; err != nil {
			t.Fatalf("load quota cycle: %v", err)
		}
		return cycle
	}

	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		cycle := loadCycle()
		return cycle.UsageReady && !cycle.Dirty && cycle.Requests == 1 && cycle.TotalTokens == 2000
	})

	// 刚重算完进入 20 秒节流期：写入第二条事件并通知，应仅推进标脏游标但暂不重算进行中周期。
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:      "throttle-evt-2",
		APIGroupKey:   "key-1",
		Provider:      "codex",
		AuthType:      "oauth",
		AuthIndex:     "auth-throttle",
		Model:         "gpt-5",
		Timestamp:     now.Add(-30 * time.Minute),
		TotalTokens:   3000,
		CostUSD:       &cost,
		CostAvailable: &costAvailable,
	}}); err != nil {
		t.Fatalf("insert second usage event: %v", err)
	}
	var secondEvent entities.UsageEvent
	if err := db.Where("event_key = ?", "throttle-evt-2").Take(&secondEvent).Error; err != nil {
		t.Fatalf("load second usage event: %v", err)
	}
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{secondEvent})

	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		return usageAggregationCheckpointCursor(t, db, entities.UsageAggregationCheckpointQuotaHistory) == secondEvent.ID &&
			loadCycle().Dirty
	})
	time.Sleep(60 * time.Millisecond)
	throttledCycle := loadCycle()
	if !throttledCycle.Dirty || throttledCycle.Requests != 1 {
		t.Fatalf("expected active cycle to stay throttled during 20s window before observation change, got %+v", throttledCycle)
	}

	// 节流期内到达 83% -> 79% 新降档观测并调用 NotifyQuotaHistoryChanged，必须立即重算不卡 20 秒节流。
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-throttle",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          resetAt,
		RemainingPercent: 79,
		FirstObservedAt:  now.Add(-10 * time.Minute),
		LastObservedAt:   now.Add(-10 * time.Minute),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write drop observation: %v", err)
	}
	runner.NotifyQuotaHistoryChanged()

	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		cycle := loadCycle()
		return !cycle.Dirty && cycle.UsageReady && cycle.Requests == 2 && cycle.TotalTokens == 5000
	})

	cycle := loadCycle()
	var dropSeg entities.QuotaPercentSegment
	if err := db.Where("cycle_id = ? AND remaining_percent = ?", cycle.ID, 79).Take(&dropSeg).Error; err != nil {
		t.Fatalf("load 79%% segment: %v", err)
	}
	if !dropSeg.TransitionUsageReady || dropSeg.TransitionRequests != 1 || dropSeg.TransitionTotalTokens != 3000 {
		t.Fatalf("expected 83%%->79%% segment to be recomputed immediately, got %+v", dropSeg)
	}
	stop()
}

func TestUsageAggregationRunnerQuotaHistoryRetriesAfterConcurrentBoundaryConflict(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-conflict",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          now.Add(2 * time.Hour),
		RemainingPercent: 90,
		FirstObservedAt:  now.Add(-time.Hour),
		LastObservedAt:   now.Add(-time.Hour),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write observation: %v", err)
	}
	cost := 0.15
	costAvailable := true
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:      "conflict-evt-1",
		APIGroupKey:   "key-1",
		Provider:      "codex",
		AuthType:      "oauth",
		AuthIndex:     "auth-conflict",
		Model:         "gpt-5",
		Timestamp:     now.Add(-30 * time.Minute),
		TotalTokens:   1500,
		CostUSD:       &cost,
		CostAvailable: &costAvailable,
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	var cycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-conflict").Take(&cycle).Error; err != nil {
		t.Fatalf("load cycle: %v", err)
	}

	// 首次计算期间注入一次并发 boundary_version 递增，使 ApplyComputedQuotaCycleUsage 返回 applied=false。
	// 验证 runner 保持 stillPending=true，无需外部再次通知即可在下一 turn 自动重试落库。
	var computeAttempts atomic.Int64
	const callbackName = "test:quota_history_runner_boundary_conflict"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "quota_percent_segments" {
			return
		}
		if computeAttempts.Add(1) == 1 {
			if err := db.Session(&gorm.Session{NewDB: true}).Clauses(dbresolver.Write).
				Model(&entities.QuotaCycle{}).
				Where("id = ?", cycle.ID).
				Update("boundary_version", gorm.Expr("boundary_version + 1")).Error; err != nil {
				t.Errorf("bump boundary_version: %v", err)
			}
		}
	}); err != nil {
		t.Fatalf("register boundary conflict callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{
		DebounceInterval: 20 * time.Millisecond,
		NowFunc:          func() time.Time { return now },
	})
	stop := startUsageAggregationTestRunner(t, runner)

	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		var current entities.QuotaCycle
		if err := db.Where("id = ?", cycle.ID).Take(&current).Error; err != nil {
			t.Fatalf("reload cycle: %v", err)
		}
		return current.UsageReady && !current.Dirty && current.Requests == 1 && current.TotalTokens == 1500
	})
	if computeAttempts.Load() < 2 {
		t.Fatalf("expected at least 2 compute attempts after boundary conflict, got %d", computeAttempts.Load())
	}
	stop()
}

func TestUsageAggregationRunnerQuotaHistoryPreservesConcurrentEventNotificationDuringTurn(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(-1 * time.Hour)

	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-concurrent-evt",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          resetAt,
		RemainingPercent: 85,
		FirstObservedAt:  now.Add(-4 * time.Hour),
		LastObservedAt:   now.Add(-2 * time.Hour),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write completed observation: %v", err)
	}

	cost := 0.10
	costAvailable := true
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:      "concurrent-evt-1",
		APIGroupKey:   "key-1",
		Provider:      "codex",
		AuthType:      "oauth",
		AuthIndex:     "auth-concurrent-evt",
		Model:         "gpt-5",
		Timestamp:     now.Add(-3 * time.Hour),
		TotalTokens:   1000,
		CostUSD:       &cost,
		CostAvailable: &costAvailable,
	}}); err != nil {
		t.Fatalf("insert first event: %v", err)
	}

	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{
		DebounceInterval: 20 * time.Millisecond,
		NowFunc:          func() time.Time { return now },
	})

	// 在首个 quota_history turn 已扫完 evt-1 正在重算周期时，并发提交 evt-2 并调用 NotifyUsageEventsCommitted；
	// 验证 turn 结束不会用旧的 stillPending=false 覆盖并发到达的通知。
	var injected atomic.Bool
	const callbackName = "test:quota_history_concurrent_event_notify"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "quota_percent_segments" || !injected.CompareAndSwap(false, true) {
			return
		}
		if _, _, err := repository.InsertUsageEvents(db.Session(&gorm.Session{NewDB: true}).Clauses(dbresolver.Write), []entities.UsageEvent{{
			EventKey:      "concurrent-evt-2",
			APIGroupKey:   "key-1",
			Provider:      "codex",
			AuthType:      "oauth",
			AuthIndex:     "auth-concurrent-evt",
			Model:         "gpt-5",
			Timestamp:     now.Add(-2 * time.Hour),
			TotalTokens:   2000,
			CostUSD:       &cost,
			CostAvailable: &costAvailable,
		}}); err != nil {
			t.Errorf("insert concurrent event: %v", err)
			return
		}
		var evt2 entities.UsageEvent
		if err := db.Session(&gorm.Session{NewDB: true}).Clauses(dbresolver.Read).Where("event_key = ?", "concurrent-evt-2").Take(&evt2).Error; err != nil {
			t.Errorf("load concurrent event: %v", err)
			return
		}
		runner.NotifyUsageEventsCommitted([]entities.UsageEvent{evt2})
	}); err != nil {
		t.Fatalf("register concurrent event callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	stop := startUsageAggregationTestRunner(t, runner)
	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		var cycle entities.QuotaCycle
		if err := db.Where("auth_index = ?", "auth-concurrent-evt").Take(&cycle).Error; err != nil {
			t.Fatalf("load cycle: %v", err)
		}
		return cycle.UsageReady && !cycle.Dirty && cycle.Requests == 2 && cycle.TotalTokens == 3000
	})
	stop()
}
