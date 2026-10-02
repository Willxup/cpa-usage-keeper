package test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// TestPricingRecalculationUsesOneTaskAndStoredFees 验证真实文件库重算只改目标费用，重复启动和配置写被同一任务状态挡住。
func TestPricingRecalculationUsesOneTaskAndStoredFees(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	anchorCost, targetCost, available := 1.0, 1.0, true
	events := []entities.UsageEvent{
		{EventKey: "before-range", Model: "model-a", Timestamp: now.Add(-150 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &anchorCost, CostAvailable: &available},
		{EventKey: "inside-range", Model: "model-a", Timestamp: now.Add(-50 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &targetCost, CostAvailable: &available},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	provider, readGate, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	saved, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 2))
	if err != nil {
		t.Fatal(err)
	}
	options, err := provider.GetPricingRecalculationOptions(context.Background())
	if err != nil || options.EarliestStart == nil || options.LatestStart == nil || options.ConfigRevision != saved.ConfigRevision || options.StepSeconds != 3600 || options.MaxDays != 30 {
		t.Fatalf("pricing recalculation options: %+v err=%v", options, err)
	}
	releaseRead, ok := readGate.Acquire()
	if !ok {
		t.Fatal("could not hold an existing fee read")
	}
	defer releaseRead()
	start := now.Add(-90 * time.Minute)
	started, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{StartAt: start, ConfigRevision: saved.ConfigRevision})
	if err != nil || !started.Started || started.Task.Status != servicedto.RecalculationRunning || !started.Task.StartAt.Equal(start) || !started.Task.EndAt.Equal(now) {
		t.Fatalf("start recalculation: %+v err=%v", started, err)
	}
	again, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{StartAt: start, ConfigRevision: 0})
	if err != nil || again.Started || again.Task.TaskID != started.Task.TaskID {
		t.Fatalf("duplicate start should return active task before revision check: %+v err=%v", again, err)
	}
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 3)); !errors.Is(err, service.ErrPricingBusy) {
		t.Fatalf("configuration save during task: %v", err)
	}
	releaseRead()
	provider.WaitPricingRecalculation()
	current, err := provider.CurrentPricingRecalculation(context.Background())
	if err != nil || current == nil || current.Status != servicedto.RecalculationCompleted || current.ProcessedCount != 1 || current.TotalCount == nil || *current.TotalCount != 1 || current.Error != nil {
		t.Fatalf("completed task: %+v err=%v", current, err)
	}
	var stored []entities.UsageEvent
	if err := db.Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].CostUSD == nil || !usageFilterCostClose(*stored[0].CostUSD, 1) || stored[1].CostUSD == nil || !usageFilterCostClose(*stored[1].CostUSD, 2) {
		t.Fatalf("target and outside-range stored fees: %+v", stored)
	}
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 3)); err != nil {
		t.Fatalf("pricing remained busy after task: %v", err)
	}
}

// TestPricingRecalculationOptionsIncludeFirstPartialHotHour 验证最早事件10:15时仍可从所属10:00小时重算。
func TestPricingRecalculationOptionsIncludeFirstPartialHotHour(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 9, 23, 10, 30, 0, 0, time.UTC)
	cost, available := 1.0, true
	if err := db.Create(&entities.UsageEvent{EventKey: "first-hot", Model: "model-a", Timestamp: now.Add(-15 * time.Minute), CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
		t.Fatal(err)
	}
	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	options, err := provider.GetPricingRecalculationOptions(context.Background())
	start := now.Truncate(time.Hour)
	if err != nil || options.EarliestStart == nil || options.LatestStart == nil || !options.EarliestStart.Equal(start) || !options.LatestStart.Equal(start) {
		t.Fatalf("first partial hot hour must be available: options=%+v err=%v", options, err)
	}
}

// TestPricingRecalculationOptionsNeverCrossThirtyDayFloor 验证30天边界落在最早事件小时中间时仍向后对齐。
func TestPricingRecalculationOptionsNeverCrossThirtyDayFloor(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 9, 23, 10, 30, 0, 0, time.UTC)
	cost, available := 1.0, true
	first := now.Add(-30*24*time.Hour - 15*time.Minute)
	if err := db.Create(&entities.UsageEvent{EventKey: "older-than-thirty-days", Timestamp: first, CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
		t.Fatal(err)
	}
	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	options, err := provider.GetPricingRecalculationOptions(context.Background())
	want := now.Add(-30 * 24 * time.Hour).Truncate(time.Hour).Add(time.Hour)
	if err != nil || options.EarliestStart == nil || !options.EarliestStart.Equal(want) {
		t.Fatalf("thirty-day floor must ceil to %s: options=%+v err=%v", want, options, err)
	}
	if _, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{StartAt: want.Add(-time.Hour)}); !errors.Is(err, service.ErrInvalidPricingRecalculationStart) {
		t.Fatalf("accepted a start before thirty-day floor: %v", err)
	}
}

// TestPricingRecalculationEmptyTargetKeepsOverviewBehind 验证合法空范围只结束任务，不追平无关旧事件。
func TestPricingRecalculationEmptyTargetKeepsOverviewBehind(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	cost, available := 1.0, true
	event := entities.UsageEvent{EventKey: "outside-target", Model: "model-a", Timestamp: now.Add(-150 * time.Minute), CostUSD: &cost, CostAvailable: &available}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	started, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{StartAt: now.Add(-90 * time.Minute), ConfigRevision: 0})
	if err != nil || !started.Started {
		t.Fatalf("start empty target: %+v err=%v", started, err)
	}
	provider.WaitPricingRecalculation()
	current, err := provider.CurrentPricingRecalculation(context.Background())
	if err != nil || current == nil || current.Status != servicedto.RecalculationCompleted || current.ProcessedCount != 0 || current.TotalCount == nil || *current.TotalCount != 0 {
		t.Fatalf("empty target task: %+v err=%v", current, err)
	}
	checkpoint, err := repository.LoadUsageAggregationCheckpointSnapshot(context.Background(), db)
	if err != nil || checkpoint.OverviewCursor != 0 {
		t.Fatalf("empty task advanced unrelated Overview: %+v err=%v", checkpoint, err)
	}
}

// TestPricingRecalculationConcurrentStartCreatesOneTask 验证并发点击只占用一个内存槽且不排队。
func TestPricingRecalculationConcurrentStartCreatesOneTask(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	cost, available := 1.0, true
	if err := db.Create(&entities.UsageEvent{EventKey: "anchor", Timestamp: now.Add(-150 * time.Minute), CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
		t.Fatal(err)
	}
	provider, gate, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	releaseRead, ok := gate.Acquire()
	if !ok {
		t.Fatal("could not hold fee read")
	}
	defer releaseRead()
	request := servicedto.StartRecalculationRequest{StartAt: now.Add(-90 * time.Minute), ConfigRevision: 0}
	type outcome struct {
		response servicedto.StartRecalculationResponse
		err      error
	}
	results := make(chan outcome, 8)
	begin := make(chan struct{})
	var calls sync.WaitGroup
	for range 8 {
		calls.Add(1)
		go func() {
			defer calls.Done()
			<-begin
			response, err := provider.StartPricingRecalculation(context.Background(), request)
			results <- outcome{response: response, err: err}
		}()
	}
	close(begin)
	calls.Wait()
	close(results)
	started := 0
	taskID := ""
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent Start: %v", result.err)
		}
		if result.response.Started {
			started++
		}
		if taskID != "" && result.response.Task.TaskID != taskID {
			t.Fatalf("concurrent Start returned multiple task IDs: %q and %q", taskID, result.response.Task.TaskID)
		}
		taskID = result.response.Task.TaskID
	}
	if started != 1 || taskID == "" {
		t.Fatalf("concurrent Start created %d tasks, ID=%q", started, taskID)
	}
	releaseRead()
	provider.WaitPricingRecalculation()
}

// TestPricingRecalculationTaskDisappearsAfterDatabaseReopen 验证状态不落库，重启只读取已提交费用。
func TestPricingRecalculationTaskDisappearsAfterDatabaseReopen(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	path := filepath.Join(t.TempDir(), "recalculation-restart.db")
	open := func() (*gorm.DB, *gorm.DB) {
		db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: path})
		if err != nil {
			t.Fatal(err)
		}
		return db, reader
	}
	closePools := func(db, reader *gorm.DB) {
		readSQL, _ := reader.DB()
		writeSQL, _ := db.DB()
		if err := readSQL.Close(); err != nil {
			t.Fatal(err)
		}
		if err := writeSQL.Close(); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	db, reader := open()
	cost, available := 1.0, true
	if err := db.Create(&entities.UsageEvent{EventKey: "old", Timestamp: now.Add(-150 * time.Minute), CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
		t.Fatal(err)
	}
	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	response, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{StartAt: now.Add(-90 * time.Minute), ConfigRevision: 0})
	if err != nil || !response.Started {
		t.Fatalf("start before reopen: %+v err=%v", response, err)
	}
	provider.WaitPricingRecalculation()
	stop()
	closePools(db, reader)
	reopened, reopenedReader := open()
	defer closePools(reopened, reopenedReader)
	newProvider, _, _, _, newStop := newPricingRecalculationTestProvider(t, reopened, now)
	defer newStop()
	current, err := newProvider.CurrentPricingRecalculation(context.Background())
	if err != nil || current != nil {
		t.Fatalf("reopened database resurrected in-memory task: %+v err=%v", current, err)
	}
}

// TestPricingRecalculationCanceledDuringReadDrainReleasesEarlierPermits 验证尚未全部停稳时取消不会错误重载缓存或留下处理暂停。
func TestPricingRecalculationCanceledDuringReadDrainReleasesEarlierPermits(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	cost, available := 1.0, true
	event := entities.UsageEvent{EventKey: "anchor", Model: "model-a", Timestamp: now.Add(-2 * time.Hour), CostUSD: &cost, CostAvailable: &available}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	provider, reads, syncer, runner, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	saved, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 2))
	if err != nil {
		t.Fatal(err)
	}
	releaseRead, ok := reads.Acquire()
	if !ok {
		t.Fatal("could not hold a fee read during stop")
	}
	defer releaseRead()
	started, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{StartAt: now.Add(-90 * time.Minute), ConfigRevision: saved.ConfigRevision})
	if err != nil || !started.Started {
		t.Fatalf("start recalculation: %+v err=%v", started, err)
	}
	// 等第三道读取许可确实进入 blocked 状态，再取消，避免只测到第一道停稳失败。
	deadline := time.After(2 * time.Second)
	for {
		probe, allowed := reads.Acquire()
		if !allowed {
			break
		}
		probe()
		select {
		case <-deadline:
			t.Fatal("recalculation never reached read drain")
		case <-time.After(time.Millisecond):
		}
	}
	// 关闭 App 生命周期模拟第三种许可仍在等待时退出；没有费用写入就不调用结束缓存处理。
	stop()
	releaseRead()
	provider.WaitPricingRecalculation()
	current, err := provider.CurrentPricingRecalculation(context.Background())
	if err != nil || current == nil || current.Status != servicedto.RecalculationFailed || current.ProcessedCount != 0 {
		t.Fatalf("early stop task: %+v err=%v", current, err)
	}
	if release, ok := reads.Acquire(); !ok {
		t.Fatal("fee reads remained blocked after early stop")
	} else {
		release()
	}
	workCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := syncer.CleanupRedisUsageInbox(workCtx); err != nil {
		t.Fatalf("maintenance did not resume: %v", err)
	}
	if _, err := syncer.ProcessRedisUsageInbox(workCtx); err != nil {
		t.Fatalf("processing did not resume: %v", err)
	}
	if _, err := runner.RunOnce(workCtx); err != nil {
		t.Fatalf("aggregation did not resume: %v", err)
	}
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 3)); err != nil {
		t.Fatalf("pricing remained busy after early stop: %v", err)
	}
}

// TestPricingRecalculationFailureKeepsCommittedPageAndRetriesFromStart 验证第二页失败保留首批，并能无持久游标地重新开始。
func TestPricingRecalculationFailureKeepsCommittedPageAndRetriesFromStart(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	cost, available := 1.0, true
	events := make([]entities.UsageEvent, 1001)
	for index := range events {
		events[index] = entities.UsageEvent{EventKey: fmt.Sprintf("target-%d", index), Model: "model-a",
			Timestamp: now.Add(-50 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000,
			CostUSD: &cost, CostAvailable: &available}
	}
	if err := db.CreateInBatches(&events, 100).Error; err != nil {
		t.Fatal(err)
	}
	anchor := entities.UsageEvent{EventKey: "anchor", Model: "model-a", Timestamp: now.Add(-150 * time.Minute), CostUSD: &cost, CostAvailable: &available}
	if err := db.Create(&anchor).Error; err != nil {
		t.Fatal(err)
	}
	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	saved, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 2))
	if err != nil {
		t.Fatal(err)
	}
	trigger := fmt.Sprintf(`CREATE TRIGGER fail_second_recalculation_page BEFORE UPDATE OF cost_usd ON usage_events WHEN NEW.id = %d BEGIN SELECT RAISE(FAIL, 'second page failed'); END`, events[1000].ID)
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	request := servicedto.StartRecalculationRequest{StartAt: now.Add(-90 * time.Minute), ConfigRevision: saved.ConfigRevision}
	first, err := provider.StartPricingRecalculation(context.Background(), request)
	if err != nil || !first.Started {
		t.Fatalf("start first recalculation: %+v err=%v", first, err)
	}
	provider.WaitPricingRecalculation()
	failed, err := provider.CurrentPricingRecalculation(context.Background())
	if err != nil || failed == nil || failed.Status != servicedto.RecalculationFailed || failed.ProcessedCount != 1000 || failed.TotalCount == nil || *failed.TotalCount != 1001 {
		t.Fatalf("failed second page state: %+v err=%v", failed, err)
	}
	var firstEvent, lastEvent entities.UsageEvent
	if err := db.First(&firstEvent, events[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&lastEvent, events[1000].ID).Error; err != nil {
		t.Fatal(err)
	}
	if firstEvent.CostUSD == nil || !usageFilterCostClose(*firstEvent.CostUSD, 2) || lastEvent.CostUSD == nil || !usageFilterCostClose(*lastEvent.CostUSD, 1) {
		t.Fatalf("committed and failed page fees: first=%+v last=%+v", firstEvent.CostUSD, lastEvent.CostUSD)
	}
	if err := db.Exec("DROP TRIGGER fail_second_recalculation_page").Error; err != nil {
		t.Fatal(err)
	}
	second, err := provider.StartPricingRecalculation(context.Background(), request)
	if err != nil || !second.Started || second.Task.TaskID == first.Task.TaskID {
		t.Fatalf("restart after failed page: %+v err=%v", second, err)
	}
	provider.WaitPricingRecalculation()
	finished, err := provider.CurrentPricingRecalculation(context.Background())
	if err != nil || finished == nil || finished.Status != servicedto.RecalculationCompleted || finished.ProcessedCount != 1001 {
		t.Fatalf("from-start rerun: %+v err=%v", finished, err)
	}
	var targetHour entities.UsageOverviewHourlyStat
	if err := db.Where("bucket_start = ? AND model = ?", timeutil.FormatStorageTime(now.Add(-time.Hour).Truncate(time.Hour)), "model-a").Take(&targetHour).Error; err != nil {
		t.Fatal(err)
	}
	if targetHour.CostUSD == nil || !usageFilterCostClose(*targetHour.CostUSD, 2002) || targetHour.RequestCount != 1001 {
		t.Fatalf("retry doubled fee or changed count: %+v", targetHour)
	}
}

func newPricingRecalculationTestProvider(t *testing.T, db *gorm.DB, now time.Time) (service.PricingProvider, *service.CostReadGate, *service.SyncService, *poller.UsageAggregationRunner, func()) {
	t.Helper()
	// 与 App 启动一致，从已保存配置构建目录；文件库重开不能把现有模型当成缺价。
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := pricing.NewCatalog(snapshot)
	recent, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	quotaService := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{}))
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, RecentUsageEvents: recent})
	runner := poller.NewUsageAggregationRunner(db)
	readGate := service.NewCostReadGate()
	lifecycle, cancel := context.WithCancel(context.Background())
	provider := service.NewPricingServiceWithRecalculation(db, catalog, service.PricingRecalculationDependencies{
		LifecycleCtx: lifecycle, Sync: syncer, Aggregation: runner, CostReadGate: readGate,
		Recent: recent, Quota: quotaService, Now: func() time.Time { return now },
	})
	stop := sync.OnceFunc(func() {
		cancel()
		provider.WaitPricingRecalculation()
		quotaService.StopRefreshTasks()
		recent.Close()
	})
	return provider, readGate, syncer, runner, stop
}
