package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"

	"gorm.io/gorm"
)

type recordingUsageAggregationNotifier struct {
	// usageCalls 记录 usage 事务提交后的通知次数。
	usageCalls int
	// identityCalls 记录 metadata 成功持久化后的通知次数。
	identityCalls int
	// events 保存 notifier 收到的已提交 usage events。
	events []entities.UsageEvent
}

func (n *recordingUsageAggregationNotifier) NotifyUsageEventsCommitted(events []entities.UsageEvent) {
	// 测试 recorder 复制输入，避免 service 后续复用切片影响断言。
	n.usageCalls++
	// events 必须已经带有数据库分配的自增 ID。
	n.events = append(n.events, events...)
}

func (n *recordingUsageAggregationNotifier) NotifyUsageIdentitiesChanged() {
	// metadata notifier 只记录轻量唤醒，不主动执行任何聚合。
	n.identityCalls++
}

type recordingUsageHeaderSnapshotAppender struct {
	calls     int
	snapshots []*quota.UsageHeaderSnapshot
}

func (a *recordingUsageHeaderSnapshotAppender) TryAppendUsageHeaderSnapshots(snapshots []*quota.UsageHeaderSnapshot) bool {
	// Header 接收方和聚合 notifier 独立，测试保留原始批次顺序和重复身份。
	a.calls++
	a.snapshots = append(a.snapshots, snapshots...)
	return true
}

func TestProcessRedisUsageInboxReturnsAfterCommitWithoutSynchronousAggregation(t *testing.T) {
	// 准备：写入一条带完整 header snapshot 的 inbox 消息，并注入只记录调用的 notifier。
	db := openUsageAggregationFlowDatabase(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []repositorydto.RedisInboxInsert{{
		Source: "redis_pull:usage",
		RawMessage: `{
			"timestamp":"2026-07-20T11:59:00Z",
			"provider":"codex",
			"auth_type":"oauth",
			"auth_index":"auth-a",
			"model":"gpt-5.5",
			"request_id":"async-aggregation",
			"tokens":{"input_tokens":10,"output_tokens":2,"cached_tokens":7,"cache_read_tokens":3,"total_tokens":12},
			"response_headers":{"X-Codex-Primary-Used-Percent":["5"],"X-Codex-Primary-Window-Minutes":["300"],"X-Codex-Primary-Reset-After-Seconds":["60"]}
		}`,
		PoppedAt: now,
	}})
	if err != nil {
		t.Fatalf("seed async aggregation inbox: %v", err)
	}
	notifier := &recordingUsageAggregationNotifier{}
	headerAppender := &recordingUsageHeaderSnapshotAppender{}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		Now:                      func() time.Time { return now },
		UsageAggregationNotifier: notifier,
		UsageHeaderQuota:         headerAppender,
	})

	// 执行：处理 inbox；返回前只允许提交 usage_events、processed 状态和内存通知。
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}

	// 断言：前台结果保持成功，notifier 收到带 ID 的事件，但旧 Overview 仍等待后台 runner。
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected async process result: %+v", result)
	}
	if notifier.usageCalls != 1 || len(notifier.events) != 1 || notifier.events[0].ID <= 0 {
		t.Fatalf("expected one committed event notification with ID, got calls=%d events=%+v", notifier.usageCalls, notifier.events)
	}
	if headerAppender.calls != 1 || len(headerAppender.snapshots) != 1 || headerAppender.snapshots[0].AuthIndex != "auth-a" {
		t.Fatalf("expected independent committed header snapshot append, got calls=%d snapshots=%+v", headerAppender.calls, headerAppender.snapshots)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load processed async inbox: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed {
		t.Fatalf("expected processed inbox before notification, got %+v", inbox)
	}
	assertUsageAggregationFlowOverviewCheckpointMissing(t, db)
}

func TestProcessRedisUsageInboxEmptyBatchKeepsLegacyCatchUpWithoutNotifier(t *testing.T) {
	// 准备：直接写入一个尚未聚合的身份事件，并使用没有后台 Runner 的兼容构造路径。
	db := openUsageAggregationFlowDatabase(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	identity := entities.UsageIdentity{Name: "compat-auth", AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "compat-auth", Type: "codex"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("insert compatibility identity: %v", err)
	}
	events := []entities.UsageEvent{{
		EventKey: "empty-no-catchup", APIGroupKey: "provider-a", Model: "model-a", AuthType: "oauth", AuthIndex: identity.Identity,
		Timestamp: now.Add(-time.Minute), InputTokens: 10, TotalTokens: 10,
	}}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert pending raw event: %v", err)
	}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", Now: func() time.Time { return now }})

	// 执行：空 inbox 轮次通过兼容 fallback 追平三类全局水位和 Identity 自己的水位。
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("empty ProcessRedisUsageInbox returned error: %v", err)
	}

	// 断言：原有 empty 信号保留，同时兼容路径不能相对 main 静默停止聚合。
	if result == nil || !result.Empty || result.ProcessedRows != 0 {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	var overviewCheckpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&overviewCheckpoint).Error; err != nil {
		t.Fatalf("load fallback overview checkpoint: %v", err)
	}
	var activityCheckpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointActivity).Take(&activityCheckpoint).Error; err != nil {
		t.Fatalf("load fallback activity checkpoint: %v", err)
	}
	var latencyCheckpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointLatency).Take(&latencyCheckpoint).Error; err != nil {
		t.Fatalf("load fallback latency checkpoint: %v", err)
	}
	if overviewCheckpoint.LastAggregatedUsageEventID != 1 || activityCheckpoint.LastAggregatedUsageEventID != 1 || latencyCheckpoint.LastAggregatedUsageEventID != 1 {
		t.Fatalf("expected all fallback checkpoints at 1, got overview=%+v activity=%+v latency=%+v", overviewCheckpoint, activityCheckpoint, latencyCheckpoint)
	}
	var updatedIdentity entities.UsageIdentity
	if err := db.First(&updatedIdentity, identity.ID).Error; err != nil {
		t.Fatalf("load fallback identity: %v", err)
	}
	if updatedIdentity.LastAggregatedUsageEventID != 1 || updatedIdentity.TotalRequests != 1 {
		t.Fatalf("expected fallback identity to catch up, got %+v", updatedIdentity)
	}
}

func TestSyncMetadataNotifiesIdentityAggregationWithoutRunningCatchUp(t *testing.T) {
	// 准备：使用全成功空 metadata fetcher 和只记录调用的 aggregation notifier。
	db := openUsageAggregationFlowDatabase(t)
	notifier := &recordingUsageAggregationNotifier{}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		MetadataFetcher:          newMetadataTestFetcher(),
		UsageAggregationNotifier: notifier,
	})

	// 执行：三类 metadata 持久化全部成功后结束本轮同步。
	if err := syncService.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}

	// 断言：只发送一次 identity 变化通知，不在 metadata 热路径执行旧聚合事务。
	if notifier.identityCalls != 1 {
		t.Fatalf("expected one identity aggregation notification, got %d", notifier.identityCalls)
	}
	assertUsageAggregationFlowOverviewCheckpointMissing(t, db)
}

func openUsageAggregationFlowDatabase(t *testing.T) *gorm.DB {
	// 准备：每个测试使用项目真实迁移和单连接 SQLite 配置。
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-aggregation-flow.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	// 测试退出时关闭唯一底层连接。
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr != nil {
			t.Errorf("load sql database: %v", dbErr)
			return
		}
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close sql database: %v", closeErr)
		}
	})
	// 返回已完成 migration 的数据库。
	return db
}

func assertUsageAggregationFlowOverviewCheckpointMissing(t *testing.T, db *gorm.DB) {
	// 准备：只统计通用表中的固定 name=overview 行。
	t.Helper()
	var count int64
	// 执行：读取 checkpoint 行数，不触发任何创建逻辑。
	if err := db.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&count).Error; err != nil {
		t.Fatalf("count overview checkpoints: %v", err)
	}
	// 断言：同步热路径没有创建或推进 Overview checkpoint。
	if count != 0 {
		t.Fatalf("expected overview checkpoint to remain missing, got %d rows", count)
	}
}
