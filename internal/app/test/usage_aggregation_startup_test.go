package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestAppWiredUsageAggregationRunnerCatchesUpExistingEventsAndStops(t *testing.T) {
	// 准备：先用生产数据库入口写入一个尚未聚合的历史事件，再关闭连接模拟进程重启。
	databasePath := filepath.Join(t.TempDir(), "app-usage-aggregation-startup.db")
	cfg := usageAggregationStartupTestConfig(databasePath)
	seedDB, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	eventTime := time.Date(2026, 7, 20, 10, 0, 0, 0, time.Local)
	if _, _, err := repository.InsertUsageEvents(seedDB, []entities.UsageEvent{{
		EventKey: "app-startup-activity", APIGroupKey: "provider-a", Model: "model-a",
		Timestamp: eventTime, InputTokens: 10, TotalTokens: 10,
	}}); err != nil {
		t.Fatalf("seed startup usage event: %v", err)
	}
	closeUsageAggregationStartupDB(t, seedDB)

	// 执行：通过真实 App 构造拿到生产 wiring 的 Runner，并启动其 startup wake 生命周期。
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	if application.UsageAggregation == nil {
		application.Close()
		t.Fatal("expected App to wire a usage aggregation runner")
	}
	runnerContext, cancelRunner := context.WithCancel(context.Background())
	runnerDone := make(chan error, 1)
	// runnerStopped 防止正常断言已经消费退出结果后，cleanup 再次等待同一 channel。
	runnerStopped := false
	go func() {
		// 这里运行的就是 App 字段中的真实单 writer Runner，不使用 stub。
		runnerDone <- application.UsageAggregation.Run(runnerContext)
	}()
	t.Cleanup(func() {
		// 任一失败路径都先停止 Runner，再关闭 App 持有的缓存、quota 和数据库资源。
		cancelRunner()
		if !runnerStopped {
			select {
			case <-runnerDone:
				// 失败路径也等待真实 Runner 退出后再关闭数据库。
			case <-time.After(2 * time.Second):
				t.Errorf("usage aggregation runner did not stop during cleanup")
			}
		}
		if closeErr := application.Close(); closeErr != nil {
			t.Errorf("close application: %v", closeErr)
		}
	})

	// 断言：没有新 usage 通知时，startup wake 也必须推进 Overview 与 Activity 两个独立 checkpoint。
	waitForAppUsageAggregationCheckpoints(t, application.DB, 1)
	var hourlyCount int64
	if err := application.DB.Model(&entities.UsageOverviewHourlyStat{}).Count(&hourlyCount).Error; err != nil {
		t.Fatalf("count startup hourly rows: %v", err)
	}
	var activityCount int64
	if err := application.DB.Model(&entities.UsageActivityStat{}).Count(&activityCount).Error; err != nil {
		t.Fatalf("count startup Activity rows: %v", err)
	}
	if hourlyCount == 0 || activityCount == 0 {
		t.Fatalf("expected startup catch-up rows, got hourly=%d activity=%d", hourlyCount, activityCount)
	}

	// 执行：取消与 App 后台任务相同形态的 context，验证真实 Runner 完成当前短事务后退出。
	cancelRunner()
	select {
	case runErr := <-runnerDone:
		// 标记退出结果已经由主断言消费，cleanup 无需再次等待。
		runnerStopped = true
		// 正常 shutdown 不应把 context cancellation 暴露为 Runner 错误。
		if runErr != nil {
			t.Fatalf("usage aggregation runner returned error: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("usage aggregation runner did not stop after cancellation")
	}
}

func waitForAppUsageAggregationCheckpoints(t *testing.T, db *gorm.DB, targetEventID int64) {
	// 准备：用短轮询等待异步 Runner 提交两个独立 checkpoint。
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// 每轮都重新读取已提交行，不能用内存状态代替数据库结果。
		var overview entities.UsageOverviewAggregationCheckpoint
		overviewErr := db.Where("name = ?", "overview").Take(&overview).Error
		var activity entities.UsageActivityAggregationCheckpoint
		activityErr := db.Where("name = ?", "activity").Take(&activity).Error
		// 两个 cursor 都覆盖目标时，startup catch-up 已真实完成。
		if overviewErr == nil && activityErr == nil && overview.LastAggregatedUsageEventID >= targetEventID && activity.LastAggregatedUsageEventID >= targetEventID {
			return
		}
		// Runner 使用有界短事务，测试只做低频轻量重试。
		time.Sleep(10 * time.Millisecond)
	}
	// 超时后读取一次最终状态，给失败输出保留准确数据库证据。
	var overview entities.UsageOverviewAggregationCheckpoint
	overviewErr := db.Where("name = ?", "overview").Take(&overview).Error
	var activity entities.UsageActivityAggregationCheckpoint
	activityErr := db.Where("name = ?", "activity").Take(&activity).Error
	t.Fatalf("usage aggregation checkpoints did not reach %d: overview=%+v err=%v activity=%+v err=%v", targetEventID, overview, overviewErr, activity, activityErr)
}

func usageAggregationStartupTestConfig(databasePath string) config.Config {
	// App 构造只需要本地数据库和稳定后台间隔，不启动 HTTP 或远端同步。
	return config.Config{
		AppPort: "invalid-port", CPABaseURL: "https://cpa.example.com", CPAManagementKey: "secret",
		RedisQueueIdleInterval: time.Second, MetadataSyncInterval: 30 * time.Second,
		SQLitePath: databasePath, RequestTimeout: 5 * time.Second,
		LogLevel: "info", LogFileEnabled: false, LogRetentionDays: 7,
	}
}

func closeUsageAggregationStartupDB(t *testing.T, db *gorm.DB) {
	// 关闭 seed 连接，确保 App 以真实重启方式重新打开同一文件。
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load seed sql database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close seed database: %v", err)
	}
}
