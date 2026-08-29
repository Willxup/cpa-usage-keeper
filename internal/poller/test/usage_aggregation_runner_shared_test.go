package poller_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestUsageAggregationRunnerSharedTurnReadsOneEventPageAndAdvancesAllRollups(t *testing.T) {
	// 三个 cursor 相等时，核心收益必须是同一事件页只读一次，而不是三个兼容入口各查一次。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	generate := true
	ttftMS := int64(120)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "shared-1", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-time.Minute), Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 900, TotalTokens: 10},
		{EventKey: "shared-2", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 1100, TotalTokens: 20},
	}); err != nil {
		t.Fatalf("insert shared runner events: %v", err)
	}

	var eventPageQueries atomic.Int64
	callbackName := "test:count_shared_usage_event_pages"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		// 固定投影只出现在共享/回退事件页，MAX(id) 与 Identity 查询不计入页读取次数。
		if tx.Statement.Table == "usage_events" && len(tx.Statement.Selects) == 1 && tx.Statement.Selects[0] == entities.UsageAggregationEventProjectionColumns {
			eventPageQueries.Add(1)
		}
	}); err != nil {
		t.Fatalf("register usage event page counter: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	runner := newUsageAggregationRunnerAt(db, now, 10*time.Millisecond)
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run shared rollups turn: %v", err)
	}
	if result.Kind != poller.UsageAggregationKindRollups || !result.Processed || result.DeferredForInbox {
		t.Fatalf("unexpected shared rollups result: %+v", result)
	}
	if got := eventPageQueries.Load(); got != 1 {
		t.Fatalf("expected one shared usage_events page query, got %d", got)
	}
	assertUsageAggregationCheckpoint(t, db, string(entities.UsageAggregationCheckpointOverview), 2)
	assertUsageAggregationCheckpoint(t, db, string(entities.UsageAggregationCheckpointActivity), 2)
	assertUsageAggregationCheckpoint(t, db, string(entities.UsageAggregationCheckpointLatency), 2)

	var overviewRows int64
	if err := db.Model(&entities.UsageOverviewHourlyStat{}).Count(&overviewRows).Error; err != nil {
		t.Fatalf("count shared overview rows: %v", err)
	}
	var activityRows int64
	if err := db.Model(&entities.UsageActivityStat{}).Count(&activityRows).Error; err != nil {
		t.Fatalf("count shared activity rows: %v", err)
	}
	var latencyRows int64
	if err := db.Model(&entities.UsageLatencyStat{}).Count(&latencyRows).Error; err != nil {
		t.Fatalf("count shared latency rows: %v", err)
	}
	if overviewRows == 0 || activityRows == 0 || latencyRows != 2 {
		t.Fatalf("expected all three rollups, got overview=%d activity=%d latency=%d", overviewRows, activityRows, latencyRows)
	}
}

func TestUsageAggregationRunnerFallbackCatchesUpEachCursorThenRestoresSharedReads(t *testing.T) {
	// 水位不一致时只依赖三行 checkpoint 各自追赶；追平后下一页必须自动恢复单次共享读取。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	generate := true
	ttftMS := int64(100)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "fallback-1", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-2 * time.Minute), Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 800},
		{EventKey: "fallback-2", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-time.Minute), Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 900},
		{EventKey: "fallback-3", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 1000},
	}); err != nil {
		t.Fatalf("insert fallback events: %v", err)
	}
	var stored []entities.UsageEvent
	if err := db.Order("id asc").Find(&stored).Error; err != nil {
		t.Fatalf("load fallback events: %v", err)
	}
	// Overview 先到 2、Activity 只到 1、Latency 仍为 0，稳定进入 fallback。
	hourly, daily, _ := overview.BuildRows(stored[:2])
	if err := repository.ApplyUsageOverviewAggregationPage(context.Background(), db, 0, 2, hourly, daily, now); err != nil {
		t.Fatalf("seed overview fallback cursor: %v", err)
	}
	activityRows, err := activity.BuildRows(stored[:1], now)
	if err != nil {
		t.Fatalf("build activity fallback seed: %v", err)
	}
	if err := repository.ApplyUsageActivityAggregationPage(context.Background(), db, 0, 1, activityRows, now); err != nil {
		t.Fatalf("seed activity fallback cursor: %v", err)
	}

	var eventPageQueries atomic.Int64
	callbackName := "test:count_fallback_usage_event_pages"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_events" && len(tx.Statement.Selects) == 1 && tx.Statement.Selects[0] == entities.UsageAggregationEventProjectionColumns {
			eventPageQueries.Add(1)
		}
	}); err != nil {
		t.Fatalf("register fallback page counter: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	runner := newUsageAggregationRunnerAt(db, now, 0)
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run fallback rollups turn: %v", err)
	}
	if result.Kind != poller.UsageAggregationKindRollups || !result.Processed {
		t.Fatalf("unexpected fallback result: %+v", result)
	}
	if got := eventPageQueries.Load(); got != 3 {
		t.Fatalf("expected three independent fallback page queries, got %d", got)
	}
	for _, name := range []entities.UsageAggregationCheckpointName{
		entities.UsageAggregationCheckpointOverview,
		entities.UsageAggregationCheckpointActivity,
		entities.UsageAggregationCheckpointLatency,
	} {
		assertUsageAggregationCheckpoint(t, db, string(name), 3)
	}

	// 跳过下一次 Identity turn，再提交第四条事件；新 rollups turn 应恢复一次共享读取。
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("run fallback identity turn: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "fallback-4", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(time.Minute), Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 1100,
	}}); err != nil {
		t.Fatalf("insert post-fallback event: %v", err)
	}
	var fourth entities.UsageEvent
	if err := db.Where("event_key = ?", "fallback-4").Take(&fourth).Error; err != nil {
		t.Fatalf("load post-fallback event: %v", err)
	}
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{fourth})
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("run restored shared rollups turn: %v", err)
	}
	if got := eventPageQueries.Load(); got != 4 {
		t.Fatalf("expected one shared query after fallback recovery, total=%d", got)
	}
}

func TestUsageAggregationRunnerPreservesEarlierRollupCommitsWhenLaterWriteFails(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		triggerSQL         string
		wantOverviewCursor int64
		wantActivityCursor int64
		wantLatencyCursor  int64
	}{
		{
			name: "overview failure does not block activity or latency",
			triggerSQL: `CREATE TRIGGER fail_runner_overview
				BEFORE INSERT ON usage_overview_hourly_stats
				BEGIN SELECT RAISE(ABORT, 'forced overview failure'); END`,
			wantActivityCursor: 1,
			wantLatencyCursor:  1,
		},
		{
			name: "activity failure does not block overview or latency",
			triggerSQL: `CREATE TRIGGER fail_runner_activity
				BEFORE INSERT ON usage_activity_stats
				BEGIN SELECT RAISE(ABORT, 'forced activity failure'); END`,
			wantOverviewCursor: 1,
			wantLatencyCursor:  1,
		},
		{
			name: "latency failure does not roll back overview or activity",
			triggerSQL: `CREATE TRIGGER fail_runner_latency
				BEFORE INSERT ON usage_latency_stats
				BEGIN SELECT RAISE(ABORT, 'forced latency failure'); END`,
			wantOverviewCursor: 1,
			wantActivityCursor: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openUsageAggregationRunnerDatabase(t)
			now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
			generate := true
			ttftMS := int64(100)
			if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
				EventKey: "write-failure", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 900,
			}}); err != nil {
				t.Fatalf("insert write-failure event: %v", err)
			}
			if err := db.Exec(testCase.triggerSQL).Error; err != nil {
				t.Fatalf("create write-failure trigger: %v", err)
			}

			runner := newUsageAggregationRunnerAt(db, now, 0)
			result, err := runner.RunOnce(context.Background())
			if err == nil {
				t.Fatal("expected forced later rollup write failure")
			}
			if result.Kind != poller.UsageAggregationKindRollups {
				t.Fatalf("unexpected failed turn: %+v", result)
			}
			assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, testCase.wantOverviewCursor)
			assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointActivity, testCase.wantActivityCursor)
			assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointLatency, testCase.wantLatencyCursor)
		})
	}
}

func TestUsageAggregationRunnerIdentityFailureDoesNotFreezeLaterRollups(t *testing.T) {
	// Identity 持续失败时，已经到达的新 usage 目标仍必须开启自己的 5 秒窗口并推进三类全局聚合。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	identity := entities.UsageIdentity{Name: "blocked-auth", AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "blocked-auth", Type: "codex"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed identity that will fail aggregation: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "identity-block-first", APIGroupKey: "provider-a", Model: "model-a", AuthType: "oauth", AuthIndex: identity.Identity, Timestamp: now, TotalTokens: 1,
	}}); err != nil {
		t.Fatalf("seed first event: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_identity_without_blocking_rollups
		BEFORE UPDATE ON usage_identities
		BEGIN SELECT RAISE(ABORT, 'forced identity failure'); END`).Error; err != nil {
		t.Fatalf("create identity failure trigger: %v", err)
	}

	runner := newUsageAggregationRunnerAt(db, now, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		return usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointOverview) == 1 &&
			usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointActivity) == 1 &&
			usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointLatency) == 1
	})

	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "identity-block-second", APIGroupKey: "provider-a", Model: "model-a", AuthType: "oauth", AuthIndex: identity.Identity, Timestamp: now.Add(time.Minute), TotalTokens: 1,
	}}); err != nil {
		cancel()
		<-done
		t.Fatalf("seed second event: %v", err)
	}
	var second entities.UsageEvent
	if err := db.Where("event_key = ?", "identity-block-second").Take(&second).Error; err != nil {
		cancel()
		<-done
		t.Fatalf("load second event: %v", err)
	}
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{second})
	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		return usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointOverview) == second.ID &&
			usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointActivity) == second.ID &&
			usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointLatency) == second.ID
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stop identity failure isolation runner: %v", err)
	}
}

func TestUsageAggregationRunnerSkipsEmptyIdentityTurnBetweenRollupPages(t *testing.T) {
	// Identity 启动扫描已经追平后，剩余 rollup 页必须连续推进，不能每页插入一次空数据库 turn。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	events := make([]entities.UsageEvent, 0, 2001)
	for index := 1; index <= 2001; index++ {
		events = append(events, entities.UsageEvent{
			EventKey: fmt.Sprintf("skip-empty-identity-%04d", index), APIGroupKey: "provider-a", Model: "model-a", Timestamp: now, TotalTokens: 1,
		})
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed multi-page rollup backlog: %v", err)
	}

	runner := newUsageAggregationRunnerAt(db, now, 0)
	for turn := 1; turn <= 3; turn++ {
		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("prepare turn %d: %v", turn, err)
		}
	}
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run page after caught-up identity: %v", err)
	}
	if result.Kind != poller.UsageAggregationKindRollups || !result.Processed {
		t.Fatalf("expected third rollup page without an empty Identity turn, got %+v", result)
	}
}

func TestUsageAggregationRunnerStopsBetweenRollupWritesWhenCPAInboxAppears(t *testing.T) {
	// Overview 提交期间新出现的 CPA inbox 必须阻止后续 Activity/Latency，占用 writer 的优先级更高。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	generate := true
	ttftMS := int64(100)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "inbox-between-writes", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: 900,
	}}); err != nil {
		t.Fatalf("insert inbox-priority event: %v", err)
	}

	// SQLite trigger 与 Overview checkpoint 同事务插入 pending inbox，不需要回调重入或第二条 writer 连接。
	if err := db.Exec(`CREATE TRIGGER insert_inbox_after_overview_checkpoint
		AFTER UPDATE OF last_aggregated_usage_event_id ON usage_aggregation_checkpoints
		WHEN NEW.name = 'overview'
		BEGIN
			INSERT INTO redis_usage_inboxes
				(source, message_hash, raw_message, status, attempt_count, last_error, usage_event_key, popped_at, created_at, updated_at)
			VALUES
				('redis_pull:usage', 'between-write', '{}', 'pending', 0, '', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
		END`).Error; err != nil {
		t.Fatalf("create between-write inbox trigger: %v", err)
	}

	runner := newUsageAggregationRunnerAt(db, now, 0)
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run inbox-priority turn: %v", err)
	}
	if !result.Processed || !result.DeferredForInbox {
		t.Fatalf("expected partial rollup then inbox defer, got %+v", result)
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 1)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointActivity, 0)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointLatency, 0)
}

func TestUsageAggregationRunnerAlternatesOneRollupPageWithOneIdentityPage(t *testing.T) {
	// 1001 events 和 27 identities 都超过一页，turn 顺序必须保持 rollups→Identity→rollups→Identity。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	identities := make([]entities.UsageIdentity, 0, 27)
	events := make([]entities.UsageEvent, 0, 1001)
	for index := 1; index <= 27; index++ {
		identity := fmt.Sprintf("fair-auth-%02d", index)
		identities = append(identities, entities.UsageIdentity{Name: identity, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: identity, Type: "codex"})
	}
	for index := 1; index <= 1001; index++ {
		identity := identities[(index-1)%len(identities)].Identity
		events = append(events, entities.UsageEvent{EventKey: fmt.Sprintf("fair-event-%04d", index), APIGroupKey: "provider-a", Model: "model-a", AuthType: "oauth", AuthIndex: identity, Timestamp: now, TotalTokens: 1})
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatalf("seed fair identities: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed fair events: %v", err)
	}

	runner := newUsageAggregationRunnerAt(db, now, 0)
	wantKinds := []poller.UsageAggregationKind{
		poller.UsageAggregationKindRollups,
		poller.UsageAggregationKindIdentity,
		poller.UsageAggregationKindRollups,
		poller.UsageAggregationKindIdentity,
	}
	for index, wantKind := range wantKinds {
		result, err := runner.RunOnce(context.Background())
		if err != nil {
			t.Fatalf("run fair turn %d: %v", index+1, err)
		}
		if result.Kind != wantKind {
			t.Fatalf("turn %d expected %s, got %+v", index+1, wantKind, result)
		}
	}
	assertUsageAggregationCheckpoint(t, db, string(entities.UsageAggregationCheckpointOverview), 1001)
	assertUsageAggregationCheckpoint(t, db, string(entities.UsageAggregationCheckpointActivity), 1001)
	assertUsageAggregationCheckpoint(t, db, string(entities.UsageAggregationCheckpointLatency), 1001)
}

func TestUsageAggregationRunnerRescansIdentityHeadAfterNotificationDuringPagedPass(t *testing.T) {
	// 分页中途到达的 metadata 通知不能被尾页误认为已经覆盖，结束后必须从 Identity 头部重扫。
	db := openUsageAggregationRunnerDatabase(t)
	identities := make([]entities.UsageIdentity, 0, repository.UsageIdentityAggregationBatchSize+1)
	for index := 1; index <= repository.UsageIdentityAggregationBatchSize+1; index++ {
		identity := fmt.Sprintf("rescan-auth-%02d", index)
		identities = append(identities, entities.UsageIdentity{Name: identity, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: identity, Type: "codex"})
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatalf("seed rescan identities: %v", err)
	}
	var identityPageQueries atomic.Int64
	callbackName := "test:count_identity_rescan_pages"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_identities" {
			identityPageQueries.Add(1)
		}
	}); err != nil {
		t.Fatalf("register identity page counter: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	runner := poller.NewUsageAggregationRunner(db)

	if result, err := runner.RunOnce(context.Background()); err != nil || result.Kind != poller.UsageAggregationKindRollups {
		t.Fatalf("run initial empty rollups turn: result=%+v err=%v", result, err)
	}
	firstPage, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run first identity page: %v", err)
	}
	if firstPage.Kind != poller.UsageAggregationKindIdentity || !firstPage.Processed {
		t.Fatalf("unexpected first identity page: %+v", firstPage)
	}
	runner.NotifyUsageIdentitiesChanged()

	// 当前没有 rollup 工作，因此直接完成旧代际尾页；不能为了轮转插入空数据库 turn。
	secondPage, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run second identity page: %v", err)
	}
	if secondPage.Kind != poller.UsageAggregationKindIdentity || !secondPage.Processed {
		t.Fatalf("unexpected second identity page: %+v", secondPage)
	}
	// 尾页期间已收到更新代际，下一次仍应直接从 Identity 头部重扫。
	rescan, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run identity head rescan: %v", err)
	}
	if rescan.Kind != poller.UsageAggregationKindIdentity {
		t.Fatalf("expected notification to force identity head rescan, got %+v", rescan)
	}
	if got := identityPageQueries.Load(); got != 3 {
		t.Fatalf("expected first page, tail page, and head rescan queries, got %d", got)
	}
}

func TestUsageAggregationRunnerStaysDatabaseSilentAfterStartupCatchUp(t *testing.T) {
	// 先同步完成空库启动 catch-up，再观察后台静默阶段不能按固定间隔轮询数据库。
	db := openUsageAggregationRunnerDatabase(t)
	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{DebounceInterval: 20 * time.Millisecond})
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("complete startup rollups turn: %v", err)
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("complete startup identity turn: %v", err)
	}

	var queryCount atomic.Int64
	callbackName := "test:count_silent_runner_queries"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		queryCount.Add(1)
	}); err != nil {
		t.Fatalf("register silent query counter: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	if got := queryCount.Load(); got != 0 {
		cancel()
		<-done
		t.Fatalf("caught-up runner polled database %d times without notification", got)
	}
	// Identity-only 通知可以立即扫描身份，但不能创建 rollup checkpoint 或 usage debounce 工作。
	runner.NotifyUsageIdentitiesChanged()
	waitForUsageAggregationRunnerCondition(t, time.Second, func() bool { return queryCount.Load() > 0 })
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 0)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointActivity, 0)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointLatency, 0)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stop silent runner: %v", err)
	}
}

func TestUsageAggregationRunnerDebounceDoesNotResetAndStartupDoesNotWait(t *testing.T) {
	t.Run("startup catch-up is immediate", func(t *testing.T) {
		db := openUsageAggregationRunnerDatabase(t)
		now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
		if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "startup-immediate", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now}}); err != nil {
			t.Fatalf("insert startup event: %v", err)
		}
		runner := newUsageAggregationRunnerAt(db, now, 500*time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- runner.Run(ctx) }()
		waitForUsageAggregationRunnerCondition(t, 300*time.Millisecond, func() bool {
			return usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointOverview) == 1
		})
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("stop startup runner: %v", err)
		}
	})

	t.Run("later notifications share the first fixed window", func(t *testing.T) {
		db := openUsageAggregationRunnerDatabase(t)
		now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
		runner := newUsageAggregationRunnerAt(db, now, 500*time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- runner.Run(ctx) }()
		// 等待启动 MAX(id) 与空 Identity 扫描结束，后续事件只能走 debounce 路径。
		time.Sleep(80 * time.Millisecond)
		if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "debounce-1", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now}}); err != nil {
			t.Fatalf("insert first debounce event: %v", err)
		}
		var first entities.UsageEvent
		if err := db.Where("event_key = ?", "debounce-1").Take(&first).Error; err != nil {
			t.Fatalf("load first debounce event: %v", err)
		}
		startedAt := time.Now()
		runner.NotifyUsageEventsCommitted([]entities.UsageEvent{first})
		time.Sleep(300 * time.Millisecond)
		if usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointOverview) != 0 {
			cancel()
			<-done
			t.Fatal("rollups ran before the fixed debounce window elapsed")
		}
		if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "debounce-2", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(time.Minute)}}); err != nil {
			t.Fatalf("insert second debounce event: %v", err)
		}
		var second entities.UsageEvent
		if err := db.Where("event_key = ?", "debounce-2").Take(&second).Error; err != nil {
			t.Fatalf("load second debounce event: %v", err)
		}
		runner.NotifyUsageEventsCommitted([]entities.UsageEvent{second})
		waitForUsageAggregationRunnerCondition(t, 350*time.Millisecond, func() bool {
			return usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointOverview) == second.ID
		})
		if elapsed := time.Since(startedAt); elapsed >= 700*time.Millisecond {
			cancel()
			<-done
			t.Fatalf("second notification reset debounce window, elapsed=%s", elapsed)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("stop debounce runner: %v", err)
		}
	})
}

func TestUsageAggregationRunnerBackgroundFailureStillLetsIdentityRun(t *testing.T) {
	// Activity 持续失败时 Overview、Latency 仍独立提交，下一 turn 的 Identity 也必须获得 writer。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	identity := entities.UsageIdentity{Name: "failure-auth", AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "failure-auth", Type: "codex"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("insert failure identity: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "failure-event", APIGroupKey: "provider-a", Model: "model-a", AuthType: "oauth", AuthIndex: identity.Identity, Timestamp: now, TotalTokens: 1}}); err != nil {
		t.Fatalf("insert failure event: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_background_activity
		BEFORE INSERT ON usage_activity_stats
		BEGIN SELECT RAISE(ABORT, 'forced background activity failure'); END`).Error; err != nil {
		t.Fatalf("create background failure trigger: %v", err)
	}
	runner := newUsageAggregationRunnerAt(db, now, 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		var stored entities.UsageIdentity
		return db.First(&stored, identity.ID).Error == nil && stored.TotalRequests == 1 && stored.LastAggregatedUsageEventID == 1
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stop failure-isolation runner: %v", err)
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 1)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointActivity, 0)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointLatency, 1)
}

func TestUsageAggregationRunnerPersistentRollupFailureDoesNotFreezeHealthyRollupsAtOlderTarget(t *testing.T) {
	// Activity 持续失败后，新 5 秒窗口仍必须让 Overview/Latency 按自己的 checkpoint 处理后续事件。
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "persistent-failure-1", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now, TotalTokens: 1,
	}}); err != nil {
		t.Fatalf("insert first persistent-failure event: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_persistent_activity
		BEFORE INSERT ON usage_activity_stats
		BEGIN SELECT RAISE(ABORT, 'forced persistent activity failure'); END`).Error; err != nil {
		t.Fatalf("create persistent Activity failure trigger: %v", err)
	}

	runner := newUsageAggregationRunnerAt(db, now, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		return usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointOverview) == 1 &&
			usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointLatency) == 1
	})

	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "persistent-failure-2", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(time.Minute), TotalTokens: 2,
	}}); err != nil {
		cancel()
		<-done
		t.Fatalf("insert second persistent-failure event: %v", err)
	}
	var second entities.UsageEvent
	if err := db.Where("event_key = ?", "persistent-failure-2").Take(&second).Error; err != nil {
		cancel()
		<-done
		t.Fatalf("load second persistent-failure event: %v", err)
	}
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{second})
	waitForUsageAggregationRunnerCondition(t, 2*time.Second, func() bool {
		return usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointOverview) == second.ID &&
			usageAggregationCheckpointCursor(db, entities.UsageAggregationCheckpointLatency) == second.ID
	})

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stop persistent-failure runner: %v", err)
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, second.ID)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointActivity, 0)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointLatency, second.ID)
}

func assertUsageAggregationCheckpointValue(t *testing.T, db *gorm.DB, name entities.UsageAggregationCheckpointName, want int64) {
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	err := db.Where("name = ?", name).Take(&checkpoint).Error
	if want == 0 {
		if err == nil && checkpoint.LastAggregatedUsageEventID != 0 {
			t.Fatalf("expected checkpoint %s to remain 0, got %+v", name, checkpoint)
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("load checkpoint %s: %v", name, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("load checkpoint %s: %v", name, err)
	}
	if checkpoint.LastAggregatedUsageEventID != want {
		t.Fatalf("expected checkpoint %s=%d, got %+v", name, want, checkpoint)
	}
}

func usageAggregationCheckpointCursor(db *gorm.DB, name entities.UsageAggregationCheckpointName) int64 {
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", name).Take(&checkpoint).Error; err != nil {
		return 0
	}
	return checkpoint.LastAggregatedUsageEventID
}

func waitForUsageAggregationRunnerCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !condition() {
		t.Fatal("usage aggregation runner condition was not reached before timeout")
	}
}

func newUsageAggregationRunnerAt(db *gorm.DB, now time.Time, debounceInterval time.Duration) *poller.UsageAggregationRunner {
	return poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{
		DebounceInterval: debounceInterval,
		NowFunc:          func() time.Time { return now },
	})
}
