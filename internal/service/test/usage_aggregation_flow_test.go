package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"

	"gorm.io/gorm"
)

type recordingUsageAggregationNotifier struct {
	usageCalls    int
	identityCalls int
	events        []entities.UsageEvent
}

func (n *recordingUsageAggregationNotifier) NotifyUsageEventsCommitted(events []entities.UsageEvent) {
	n.usageCalls++
	n.events = append(n.events, events...)
}

func (n *recordingUsageAggregationNotifier) NotifyUsageIdentitiesChanged() {
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
	db := openUsageServiceTestDatabase(t)
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

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}

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
	db := openUsageServiceTestDatabase(t)
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

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("empty ProcessRedisUsageInbox returned error: %v", err)
	}

	if result == nil || !result.Empty || result.ProcessedRows != 0 {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	for _, name := range []entities.UsageAggregationCheckpointName{entities.UsageAggregationCheckpointOverview, entities.UsageAggregationCheckpointActivity, entities.UsageAggregationCheckpointLatency} {
		var checkpoint entities.UsageAggregationCheckpoint
		if err := db.Where("name = ?", name).Take(&checkpoint).Error; err != nil {
			t.Fatalf("load fallback %s checkpoint: %v", name, err)
		}
		if checkpoint.LastAggregatedUsageEventID != 1 {
			t.Fatalf("expected fallback %s checkpoint at 1, got %+v", name, checkpoint)
		}
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
	db := openUsageServiceTestDatabase(t)
	notifier := &recordingUsageAggregationNotifier{}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		MetadataFetcher:          newMetadataTestFetcher(),
		UsageAggregationNotifier: notifier,
	})

	if err := syncService.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}

	if notifier.identityCalls != 1 {
		t.Fatalf("expected one identity aggregation notification, got %d", notifier.identityCalls)
	}
	assertUsageAggregationFlowOverviewCheckpointMissing(t, db)
}

func assertUsageAggregationFlowOverviewCheckpointMissing(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	if err := db.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&count).Error; err != nil {
		t.Fatalf("count overview checkpoints: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected overview checkpoint to remain missing, got %d rows", count)
	}
}
