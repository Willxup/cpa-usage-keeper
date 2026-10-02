package test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type blockingUsageCommitNotifier struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (n *blockingUsageCommitNotifier) NotifyUsageEventsCommitted([]entities.UsageEvent) {
	n.once.Do(func() { close(n.started) })
	<-n.release
}

func (*blockingUsageCommitNotifier) NotifyUsageIdentitiesChanged() {}

type blockingUsageHeaderAppender struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (a *blockingUsageHeaderAppender) TryAppendUsageHeaderSnapshots([]*quota.UsageHeaderSnapshot) bool {
	a.once.Do(func() { close(a.started) })
	<-a.release
	return true
}

type drainingRecentUsageAppender struct {
	started chan struct{}
	release chan struct{}
}

func (*drainingRecentUsageAppender) TryAppend([]entities.UsageEvent) bool { return true }

func (a *drainingRecentUsageAppender) DrainAcceptedAppends(ctx context.Context) error {
	close(a.started)
	select {
	case <-a.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type pauseOutcome struct {
	resume func()
	err    error
}

func TestPauseUsageWorkWaitsForPostCommitNotificationsAndRecentDrain(t *testing.T) {
	db := openSyncTestDatabase(t)
	rows := seedRedisInboxMessagesForTest(t, db, usagePauseMessage("pause-first"))
	notifier := &blockingUsageCommitNotifier{started: make(chan struct{}), release: make(chan struct{})}
	header := &blockingUsageHeaderAppender{started: make(chan struct{}), release: make(chan struct{})}
	recent := &drainingRecentUsageAppender{started: make(chan struct{}), release: make(chan struct{})}
	defer releasePauseChannel(notifier.release)
	defer releasePauseChannel(header.release)
	defer releasePauseChannel(recent.release)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		PricingCatalog:           emptyPricingCatalogForTest(),
		UsageAggregationNotifier: notifier,
		UsageHeaderQuota:         header,
		RecentUsageEvents:        recent,
	})
	processed := make(chan error, 1)
	go func() { _, err := syncer.ProcessRedisUsageInbox(context.Background()); processed <- err }()
	awaitPauseSignal(t, notifier.started)
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil || inbox.Status != repository.RedisUsageInboxStatusProcessed {
		t.Fatalf("通知已开始时事件和 inbox 应已提交：inbox=%+v err=%v", inbox, err)
	}

	paused := make(chan pauseOutcome, 1)
	go func() {
		resume, err := syncer.PauseUsageWork(context.Background())
		paused <- pauseOutcome{resume, err}
	}()
	assertPausePending(t, paused)
	releasePauseChannel(notifier.release)
	awaitPauseSignal(t, header.started)
	assertPausePending(t, paused)
	releasePauseChannel(header.release)
	if err := awaitPauseSignal(t, processed); err != nil {
		t.Fatalf("在途批次提交后通知失败：%v", err)
	}
	awaitPauseSignal(t, recent.started)
	assertPausePending(t, paused)
	releasePauseChannel(recent.release)
	outcome := awaitPauseSignal(t, paused)
	if outcome.err != nil {
		t.Fatalf("停稳已提交批次失败：%v", outcome.err)
	}
	defer outcome.resume()

	// 停稳期间 CPA 接收仍可落盘，但直接处理和两种维护入口都不得进入仓储。
	newRows := seedRedisInboxMessagesForTest(t, db, usagePauseMessage("pause-second"))
	workDone := make(chan error, 3)
	go func() { _, err := syncer.ProcessRedisUsageInbox(context.Background()); workDone <- err }()
	go func() { workDone <- syncer.CleanupStorage(context.Background()) }()
	go func() { workDone <- syncer.CleanupRedisUsageInbox(context.Background()) }()
	select {
	case err := <-workDone:
		t.Fatalf("停稳期间业务或维护提前完成：%v", err)
	case <-time.After(40 * time.Millisecond):
	}
	var pending entities.RedisUsageInbox
	if err := db.First(&pending, newRows[0].ID).Error; err != nil || pending.Status != repository.RedisUsageInboxStatusPending || pending.AttemptCount != 0 {
		t.Fatalf("停稳期间新 inbox 不应被处理或消耗重试：inbox=%+v err=%v", pending, err)
	}
	outcome.resume()
	for range 3 {
		if err := awaitPauseSignal(t, workDone); err != nil {
			t.Fatalf("恢复后业务或维护失败：%v", err)
		}
	}
}

func TestPauseUsageWorkCanceledWhileWaitingRestoresAdmission(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db, usagePauseMessage("pause-cancel-first"))
	notifier := &blockingUsageCommitNotifier{started: make(chan struct{}), release: make(chan struct{})}
	defer releasePauseChannel(notifier.release)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		PricingCatalog:           emptyPricingCatalogForTest(),
		UsageAggregationNotifier: notifier,
	})
	first := make(chan error, 1)
	go func() { _, err := syncer.ProcessRedisUsageInbox(context.Background()); first <- err }()
	awaitPauseSignal(t, notifier.started)
	ctx, cancel := context.WithCancel(context.Background())
	paused := make(chan pauseOutcome, 1)
	go func() { resume, err := syncer.PauseUsageWork(ctx); paused <- pauseOutcome{resume, err} }()
	assertPausePending(t, paused)
	cancel()
	outcome := awaitPauseSignal(t, paused)
	if !errors.Is(outcome.err, context.Canceled) || outcome.resume != nil {
		t.Fatalf("取消停稳应撤销门禁：%+v", outcome)
	}
	releasePauseChannel(notifier.release)
	if err := awaitPauseSignal(t, first); err != nil {
		t.Fatalf("在途批次失败：%v", err)
	}
	seedRedisInboxMessagesForTest(t, db, usagePauseMessage("pause-cancel-second"))
	// 第二条若仍被遗留的暂停许可挡住，会在这里超时。
	processCtx, processCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer processCancel()
	result, err := syncer.ProcessRedisUsageInbox(processCtx)
	if err != nil || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("取消后应正常处理下一批：result=%+v err=%v", result, err)
	}
}

func TestPauseUsageWorkCanceledDuringRecentDrainRestoresAdmission(t *testing.T) {
	db := openSyncTestDatabase(t)
	recent := &drainingRecentUsageAppender{started: make(chan struct{}), release: make(chan struct{})}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		PricingCatalog:    emptyPricingCatalogForTest(),
		RecentUsageEvents: recent,
	})
	ctx, cancel := context.WithCancel(context.Background())
	paused := make(chan pauseOutcome, 1)
	go func() { resume, err := syncer.PauseUsageWork(ctx); paused <- pauseOutcome{resume, err} }()
	awaitPauseSignal(t, recent.started)
	cancel()
	outcome := awaitPauseSignal(t, paused)
	if !errors.Is(outcome.err, context.Canceled) || outcome.resume != nil {
		t.Fatalf("排空取消后应恢复处理许可：%+v", outcome)
	}
	seedRedisInboxMessagesForTest(t, db, usagePauseMessage("pause-drain-cancel"))
	processCtx, processCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer processCancel()
	result, err := syncer.ProcessRedisUsageInbox(processCtx)
	if err != nil || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("排空取消后应正常处理下一批：result=%+v err=%v", result, err)
	}
}

func TestPauseUsageWorkWaitsForInFlightStorageCleanup(t *testing.T) {
	db := openSyncTestDatabase(t)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{})
	started := make(chan struct{})
	release := make(chan struct{})
	defer releasePauseChannel(release)
	var once sync.Once
	if err := db.Callback().Delete().Before("gorm:delete").Register("test:pause_cleanup", func(*gorm.DB) {
		once.Do(func() { close(started) })
		<-release
	}); err != nil {
		t.Fatalf("注册维护事务阻塞点：%v", err)
	}
	cleanup := make(chan error, 1)
	go func() { cleanup <- syncer.CleanupStorage(context.Background()) }()
	awaitPauseSignal(t, started)
	paused := make(chan pauseOutcome, 1)
	go func() {
		resume, err := syncer.PauseUsageWork(context.Background())
		paused <- pauseOutcome{resume, err}
	}()
	assertPausePending(t, paused)
	releasePauseChannel(release)
	if err := awaitPauseSignal(t, cleanup); err != nil {
		t.Fatalf("在途维护失败：%v", err)
	}
	outcome := awaitPauseSignal(t, paused)
	if outcome.err != nil {
		t.Fatalf("维护结束后停稳失败：%v", outcome.err)
	}
	outcome.resume()
}

func TestPausedProcessOnceCancellationDoesNotRecordFailure(t *testing.T) {
	db := openSyncTestDatabase(t)
	rows := seedRedisInboxMessagesForTest(t, db, usagePauseMessage("pause-runner"))
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
	resume, err := syncer.PauseUsageWork(context.Background())
	if err != nil {
		t.Fatalf("暂停处理：%v", err)
	}
	defer resume()
	runner := poller.NewRedisProcessRunner(syncer)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, processErr := runner.ProcessOnce(ctx); result <- processErr }()
	select {
	case processErr := <-result:
		t.Fatalf("暂停期间 ProcessOnce 提前返回：%v", processErr)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	if processErr := awaitPauseSignal(t, result); !errors.Is(processErr, context.Canceled) {
		t.Fatalf("取消等待应返回 context.Canceled，得到 %v", processErr)
	}
	if status := runner.Status(); status.LastError != "" {
		t.Fatalf("暂停等待取消不应记录为批次失败：%+v", status)
	}
	// 后台 Run 复用 ProcessOnce，也必须在同一暂停许可上等待且可由生命周期 context 退出。
	runCtx, stopRun := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- runner.Run(runCtx) }()
	deadline := time.After(2 * time.Second)
	for !runner.Status().Running {
		select {
		case <-deadline:
			t.Fatal("后台处理循环未启动")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case runErr := <-runDone:
		t.Fatalf("后台循环在暂停期间提前退出：%v", runErr)
	case <-time.After(30 * time.Millisecond):
	}
	stopRun()
	if runErr := awaitPauseSignal(t, runDone); runErr != nil {
		t.Fatalf("后台循环取消失败：%v", runErr)
	}
	if status := runner.Status(); status.LastError != "" {
		t.Fatalf("后台等待取消不应记录为批次失败：%+v", status)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil || inbox.Status != repository.RedisUsageInboxStatusPending || inbox.AttemptCount != 0 {
		t.Fatalf("取消暂停等待不得消耗 inbox 重试：inbox=%+v err=%v", inbox, err)
	}
}

func usagePauseMessage(requestID string) string {
	return `{"timestamp":"2026-04-27T08:00:00Z","provider":"codex","auth_type":"oauth","auth_index":"auth-a","model":"gpt-5.5","request_id":"` + requestID + `","tokens":{"input_tokens":10,"output_tokens":2},"response_headers":{"X-Codex-Primary-Used-Percent":["5"],"X-Codex-Primary-Window-Minutes":["300"],"X-Codex-Primary-Reset-After-Seconds":["60"]}}`
}

func awaitPauseSignal[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("等待停稳阶段信号超时")
		var zero T
		return zero
	}
}

func assertPausePending(t *testing.T, ch <-chan pauseOutcome) {
	t.Helper()
	select {
	case outcome := <-ch:
		t.Fatalf("在途提交后通知尚未完成时停稳提前返回：%+v", outcome)
	case <-time.After(30 * time.Millisecond):
	}
}

func releasePauseChannel(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
