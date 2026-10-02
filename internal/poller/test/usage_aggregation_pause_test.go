package poller_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type pausedAggregationResult struct {
	resume func()
	err    error
}

func TestPausedAggregationCatchesUpOverviewDespitePendingInbox(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	events := make([]entities.UsageEvent, 0, 1002)
	for index := 1; index <= 1002; index++ {
		events = append(events, entities.UsageEvent{EventKey: fmt.Sprintf("controlled-%04d", index), APIGroupKey: "key-a", Model: "model-a", Timestamp: now, TotalTokens: 1})
	}
	if _, _, err := repository.InsertUsageEvents(db, priceRunnerFixtureEvents(events)); err != nil {
		t.Fatalf("写入待聚合事件：%v", err)
	}
	// ID 空洞不能当作数量或终点；最后一条仍定义本次 H。
	if err := db.Where("id = ?", 501).Delete(&entities.UsageEvent{}).Error; err != nil {
		t.Fatalf("创建事件 ID 空洞：%v", err)
	}
	if _, err := repository.InsertRedisUsageInboxRawMessages(db, "redis_pull:usage", []string{"{}"}, now); err != nil {
		t.Fatalf("写入持续积压的 inbox：%v", err)
	}
	var nowCalls atomic.Int64
	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{NowFunc: func() time.Time {
		if nowCalls.Add(1) == 2 {
			// 第二页开始前证明前页已经归还唯一 writer，CPA 接收仍可落 inbox。
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := repository.InsertRedisUsageInboxRawMessages(db.WithContext(ctx), "redis_pull:usage", []string{"second-page"}, now); err != nil {
				t.Errorf("前页未归还 writer：%v", err)
			}
		}
		return now
	}})
	resume, err := runner.Pause(context.Background())
	if err != nil {
		t.Fatalf("暂停普通聚合：%v", err)
	}
	defer resume()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer waitCancel()
	if _, err := runner.RunOnce(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("暂停期间 RunOnce 应等待许可且可取消：%v", err)
	}
	if err := runner.CatchUpOverviewTo(context.Background(), 1002); err != nil {
		t.Fatalf("有 inbox 积压时受控追平 Overview：%v", err)
	}
	if nowCalls.Load() != 2 {
		t.Fatalf("1001 条有效事件应分两页，实际 %d 页", nowCalls.Load())
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 1002)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointActivity, 0)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointLatency, 0)
	var hourly entities.UsageOverviewHourlyStat
	if err := db.Where("model = ?", "model-a").Take(&hourly).Error; err != nil || hourly.RequestCount != 1001 {
		t.Fatalf("Overview 只能累计已有的 1001 条事件：row=%+v err=%v", hourly, err)
	}
	// 已经覆盖 H 再执行一次不应重放计数和 Token。
	if err := runner.CatchUpOverviewTo(context.Background(), 1002); err != nil {
		t.Fatalf("重复追平：%v", err)
	}
	var repeated entities.UsageOverviewHourlyStat
	if err := db.Where("model = ?", "model-a").Take(&repeated).Error; err != nil || repeated.RequestCount != 1001 {
		t.Fatalf("重复追平不得重放原统计：row=%+v err=%v", repeated, err)
	}
}

func TestAggregationPauseWaitsForReaderPageAndBlocksRunOnce(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, priceRunnerFixtureEvents([]entities.UsageEvent{{EventKey: "old-page", Model: "model-a", Timestamp: now, TotalTokens: 1}})); err != nil {
		t.Fatal(err)
	}
	pageStarted := make(chan struct{})
	releasePage := make(chan struct{})
	defer releaseAggregationChannel(releasePage)
	var blockOnce sync.Once
	const callback = "test:block_aggregation_page"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_events" && len(tx.Statement.Selects) == 1 && tx.Statement.Selects[0] == entities.UsageAggregationEventProjectionColumns {
			blockOnce.Do(func() { close(pageStarted); <-releasePage })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	runner := poller.NewUsageAggregationRunner(db)
	turnDone := make(chan error, 1)
	go func() { _, err := runner.RunOnce(context.Background()); turnDone <- err }()
	awaitAggregationSignal(t, pageStarted)
	paused := make(chan pausedAggregationResult, 1)
	go func() {
		resume, err := runner.Pause(context.Background())
		paused <- pausedAggregationResult{resume, err}
	}()
	assertAggregationPausePending(t, paused)
	releaseAggregationChannel(releasePage)
	if err := awaitAggregationSignal(t, turnDone); err != nil {
		t.Fatalf("旧预读页提交失败：%v", err)
	}
	outcome := awaitAggregationSignal(t, paused)
	if outcome.err != nil {
		t.Fatalf("预读页完成后停稳失败：%v", outcome.err)
	}
	defer outcome.resume()
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 1)
	if _, _, err := repository.InsertUsageEvents(db, priceRunnerFixtureEvents([]entities.UsageEvent{{EventKey: "after-pause", Model: "model-a", Timestamp: now, TotalTokens: 1}})); err != nil {
		t.Fatal(err)
	}
	var second entities.UsageEvent
	if err := db.Where("event_key = ?", "after-pause").Take(&second).Error; err != nil {
		t.Fatal(err)
	}
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{second})
	blockedTurn := make(chan error, 1)
	go func() { _, err := runner.RunOnce(context.Background()); blockedTurn <- err }()
	select {
	case err := <-blockedTurn:
		t.Fatalf("暂停期间 RunOnce 提前返回：%v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if err := runner.CatchUpOverviewTo(context.Background(), 2); err != nil {
		t.Fatalf("停稳后追平：%v", err)
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 2)
	outcome.resume()
	if err := awaitAggregationSignal(t, blockedTurn); err != nil {
		t.Fatalf("恢复后的 RunOnce 失败：%v", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("恢复原公平调度：%v", err)
		}
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 2)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointActivity, 2)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointLatency, 2)
	var hourly entities.UsageOverviewHourlyStat
	if err := db.Where("model = ?", "model-a").Take(&hourly).Error; err != nil || hourly.RequestCount != 2 {
		t.Fatalf("恢复后不得重放受控 Overview：row=%+v err=%v", hourly, err)
	}
}

func TestAggregationPauseCancellationWhilePageActiveRestoresRunner(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, priceRunnerFixtureEvents([]entities.UsageEvent{{EventKey: "cancel-active-page", Model: "model-a", Timestamp: time.Now(), TotalTokens: 1}})); err != nil {
		t.Fatal(err)
	}
	pageStarted, releasePage := make(chan struct{}), make(chan struct{})
	defer releaseAggregationChannel(releasePage)
	var once sync.Once
	const callback = "test:cancel_active_aggregation_page"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_events" && len(tx.Statement.Selects) == 1 && tx.Statement.Selects[0] == entities.UsageAggregationEventProjectionColumns {
			once.Do(func() { close(pageStarted); <-releasePage })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	runner := poller.NewUsageAggregationRunner(db)
	turnDone := make(chan error, 1)
	go func() { _, err := runner.RunOnce(context.Background()); turnDone <- err }()
	awaitAggregationSignal(t, pageStarted)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	paused := make(chan pausedAggregationResult, 1)
	go func() { resume, err := runner.Pause(ctx); paused <- pausedAggregationResult{resume, err} }()
	assertAggregationPausePending(t, paused)
	cancel()
	outcome := awaitAggregationSignal(t, paused)
	if !errors.Is(outcome.err, context.Canceled) || outcome.resume != nil {
		t.Fatalf("取消正在等待在途页的暂停应恢复许可：%+v", outcome)
	}
	releaseAggregationChannel(releasePage)
	if err := awaitAggregationSignal(t, turnDone); err != nil {
		t.Fatalf("取消暂停不得中断原聚合页：%v", err)
	}
	nextCtx, stopNext := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopNext()
	if _, err := runner.RunOnce(nextCtx); err != nil {
		t.Fatalf("取消后普通聚合不得留在暂停状态：%v", err)
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 1)
}

func TestControlledOverviewRejectsMissingTargetAndCanceledPauseRestoresRunner(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	runner := poller.NewUsageAggregationRunner(db)
	if err := runner.CatchUpOverviewTo(context.Background(), 1); err == nil {
		t.Fatal("未暂停时不得执行受控追平")
	}
	resume, err := runner.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.CatchUpOverviewTo(context.Background(), 1); err == nil {
		t.Fatal("水位未覆盖 H 且无事件页时不得报告成功")
	}
	resume()
	if err := db.Create(&entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 9}).Error; err != nil {
		t.Fatalf("写入已有更大 Overview 水位：%v", err)
	}
	resume, err = runner.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.CatchUpOverviewTo(context.Background(), 1); err != nil {
		t.Fatalf("已有更大水位应直接保留：%v", err)
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 9)
	resume()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resume, err = runner.Pause(ctx)
	if !errors.Is(err, context.Canceled) || resume != nil {
		t.Fatalf("取消停稳应恢复普通许可：resume=%v err=%v", resume != nil, err)
	}
	resultCtx, resultCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer resultCancel()
	if _, err := runner.RunOnce(resultCtx); err != nil {
		t.Fatalf("取消停稳后普通 RunOnce 应可入场：%v", err)
	}
}

func TestAggregationRunResumesWithNotificationReceivedDuringPause(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, priceRunnerFixtureEvents([]entities.UsageEvent{{EventKey: "before-pause", Model: "model-a", Timestamp: now, TotalTokens: 1}})); err != nil {
		t.Fatal(err)
	}
	runner := newUsageAggregationRunnerAt(db, now, 10*time.Millisecond)
	resume, err := runner.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	// RunOnce 在许可外读取启动目标并冻结窗口；取消等待不能丢掉之后的新事件通知。
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer waitCancel()
	if _, err := runner.RunOnce(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("暂停中的 RunOnce 应可取消等待：%v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, priceRunnerFixtureEvents([]entities.UsageEvent{{EventKey: "during-pause", Model: "model-a", Timestamp: now, TotalTokens: 1}})); err != nil {
		t.Fatal(err)
	}
	var second entities.UsageEvent
	if err := db.Where("event_key = ?", "during-pause").Take(&second).Error; err != nil {
		t.Fatal(err)
	}
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{second})
	runCtx, stopRun := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- runner.Run(runCtx) }()
	select {
	case err := <-runDone:
		t.Fatalf("后台 Run 暂停期间提前退出：%v", err)
	case <-time.After(40 * time.Millisecond):
	}
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 0)
	resume()
	deadline := time.After(5 * time.Second)
	for {
		if usageAggregationCheckpointCursor(t, db, entities.UsageAggregationCheckpointOverview) == 2 {
			break
		}
		select {
		case <-deadline:
			stopRun()
			t.Fatal("恢复后未处理暂停期间收到的新事件通知")
		case <-time.After(5 * time.Millisecond):
		}
	}
	stopRun()
	if err := awaitAggregationSignal(t, runDone); err != nil {
		t.Fatalf("停止正常聚合 Run：%v", err)
	}
}

func TestAggregationResumeWaitsForControlledPageAndRejectsSecondCatchUp(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, priceRunnerFixtureEvents([]entities.UsageEvent{{EventKey: "exclusive-catchup", Model: "model-a", Timestamp: now, TotalTokens: 1}})); err != nil {
		t.Fatal(err)
	}
	pageStarted := make(chan struct{})
	releasePage := make(chan struct{})
	defer releaseAggregationChannel(releasePage)
	runner := poller.NewUsageAggregationRunnerWithOptions(db, poller.UsageAggregationRunnerOptions{NowFunc: func() time.Time {
		close(pageStarted)
		<-releasePage
		return now
	}})
	resume, err := runner.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	catchUpDone := make(chan error, 1)
	go func() { catchUpDone <- runner.CatchUpOverviewTo(context.Background(), 1) }()
	awaitAggregationSignal(t, pageStarted)
	if err := runner.CatchUpOverviewTo(context.Background(), 1); err == nil {
		t.Fatal("同一暂停期不得并发读取旧 checkpoint 并提交重复页")
	}
	resumeDone := make(chan struct{})
	go func() { resume(); close(resumeDone) }()
	select {
	case <-resumeDone:
		t.Fatal("受控页仍在写时不得恢复普通 turn")
	case <-time.After(30 * time.Millisecond):
	}
	releaseAggregationChannel(releasePage)
	if err := awaitAggregationSignal(t, catchUpDone); err != nil {
		t.Fatalf("受控页提交失败：%v", err)
	}
	awaitAggregationSignal(t, resumeDone)
	assertUsageAggregationCheckpointValue(t, db, entities.UsageAggregationCheckpointOverview, 1)
}

func awaitAggregationSignal[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("等待聚合停稳信号超时")
		var zero T
		return zero
	}
}

func assertAggregationPausePending(t *testing.T, ch <-chan pausedAggregationResult) {
	t.Helper()
	select {
	case result := <-ch:
		t.Fatalf("事件页仍在预读时停稳提前返回：%+v", result)
	case <-time.After(30 * time.Millisecond):
	}
}

func releaseAggregationChannel(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
