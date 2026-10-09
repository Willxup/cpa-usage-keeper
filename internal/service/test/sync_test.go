package test

import (
	"bytes"
	"context"
	. "cpa-usage-keeper/internal/service"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const redisUsageInboxTestSource = "redis_pull:usage"

type recordingRecentUsageAppender struct {
	calls   int
	events  []entities.UsageEvent
	allowed bool
}

type recordingUsageHeaderQuotaAppender struct {
	eventCalls int
	calls      int
	snapshots  []*quota.UsageHeaderSnapshot
	allowed    bool
}

type aggregationAwareUsageHeaderQuotaAppender struct {
	db                  *gorm.DB
	calls               int
	snapshots           []*quota.UsageHeaderSnapshot
	hourlyStatsAtAppend int64
	countErr            error
}

func (r *recordingRecentUsageAppender) TryAppend(events []entities.UsageEvent) bool {
	r.calls++
	r.events = append(r.events, events...)
	return r.allowed
}

func (r *recordingUsageHeaderQuotaAppender) NotifyUsageEventsCommitted(_ []entities.UsageEvent) {
	r.eventCalls++
}

func (r *recordingUsageHeaderQuotaAppender) NotifyUsageIdentitiesChanged() {}

func (r *recordingUsageHeaderQuotaAppender) TryAppendUsageHeaderSnapshots(snapshots []*quota.UsageHeaderSnapshot) bool {
	r.calls++
	r.snapshots = append(r.snapshots, snapshots...)
	return r.allowed
}

func (r *aggregationAwareUsageHeaderQuotaAppender) NotifyUsageEventsCommitted(_ []entities.UsageEvent) {
}

func (r *aggregationAwareUsageHeaderQuotaAppender) TryAppendUsageHeaderSnapshots(snapshots []*quota.UsageHeaderSnapshot) bool {
	r.calls++
	r.snapshots = append(r.snapshots, snapshots...)
	r.countErr = r.db.Model(&entities.UsageOverviewHourlyStat{}).Where("auth_index = ?", "codex-auth").Count(&r.hourlyStatsAtAppend).Error
	return true
}

func (r *aggregationAwareUsageHeaderQuotaAppender) NotifyUsageIdentitiesChanged() {}

func TestProcessRedisUsageInboxPersistsEventsWithoutSnapshot(t *testing.T) {
	for _, asynchronous := range []bool{false, true} {
		t.Run(fmt.Sprintf("asynchronous=%t", asynchronous), func(t *testing.T) {
			db := openSyncTestDatabase(t)
			rows := seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","endpoint":"/v1/messages","auth_type":"api_key","model":"sonnet","request_id":"process-only","tokens":{"input_tokens":1,"output_tokens":2}}`)
			notifier := &recordingUsageHeaderQuotaAppender{allowed: true}
			options := SyncServiceOptions{BaseURL: "https://cpa.example.com"}
			if asynchronous {
				options.UsageAggregationNotifier = notifier
				options.UsageHeaderQuota = notifier
			}
			service := NewSyncServiceWithOptions(db, options)

			result, err := service.ProcessRedisUsageInbox(context.Background())
			if err != nil {
				t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
			}
			if result == nil || result.Empty || result.Status != "completed" || result.InsertedEvents != 1 || result.DedupedEvents != 0 || result.ProcessedRows != 1 || result.BatchFull {
				t.Fatalf("unexpected process result: %+v", result)
			}
			var event entities.UsageEvent
			if err := db.First(&event).Error; err != nil {
				t.Fatalf("load usage event: %v", err)
			}
			if event.EventKey != "process-only" {
				t.Fatalf("expected Redis event without snapshot run id, got %+v", event)
			}
			if event.Provider != "claude" || event.Endpoint != "/v1/messages" || event.AuthType != "apikey" || event.RequestID != "process-only" {
				t.Fatalf("expected Redis identity fields to persist, got %+v", event)
			}
			var inbox entities.RedisUsageInbox
			if err := db.First(&inbox, rows[0].ID).Error; err != nil {
				t.Fatalf("load inbox row: %v", err)
			}
			if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "process-only" {
				t.Fatalf("expected processed inbox row without snapshot link, got %+v", inbox)
			}
			// 断言：有 notifier 的生产路径只发送事件通知，不在前台创建 Overview checkpoint。
			if asynchronous {
				var checkpointCount int64
				if err := db.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&checkpointCount).Error; err != nil {
					t.Fatalf("count overview aggregation checkpoint: %v", err)
				}
				if checkpointCount != 0 {
					t.Fatalf("expected process path to leave overview aggregation to runner, got %d checkpoints", checkpointCount)
				}
				if notifier.eventCalls != 1 || notifier.calls != 0 {
					t.Fatalf("expected only event notification, got events=%d headers=%d", notifier.eventCalls, notifier.calls)
				}
			}
		})
	}
}

func TestProcessRedisUsageInboxNotifiesRecentCacheAfterTransactionCommit(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(fmt.Sprintf("accepted=%t", allowed), func(t *testing.T) {
			db := openSyncTestDatabase(t)
			seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","auth_type":"oauth","source":"auth-user@example.com","auth_index":"auth-1","model":"sonnet","request_id":"notify-cache","tokens":{"input_tokens":1,"output_tokens":2}}`)
			cache := &recordingRecentUsageAppender{allowed: allowed}
			service := NewSyncServiceWithOptions(db, SyncServiceOptions{
				BaseURL:           "https://cpa.example.com",
				RecentUsageEvents: cache,
			})

			result, err := service.ProcessRedisUsageInbox(context.Background())
			if err != nil {
				t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
			}
			if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
				t.Fatalf("unexpected process result: %+v", result)
			}
			if cache.calls != 1 || len(cache.events) != 1 {
				t.Fatalf("expected one recent cache notification, got calls=%d events=%+v", cache.calls, cache.events)
			}
			if cache.events[0].EventKey != "notify-cache" || cache.events[0].AuthIndex != "auth-1" {
				t.Fatalf("unexpected notified event: %+v", cache.events[0])
			}
		})
	}
}

func TestProcessRedisUsageInboxReturnsBatchSignalWhenTransactionCannotStart(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"transaction-start-fails","tokens":{"input_tokens":1,"output_tokens":2}}`)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql database: %v", err)
	}
	callbackName := "test:close_db_after_redis_inbox_list"

	// 取出 inbox 后关闭数据库，确保故障发生在真正开始事务时。
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "redis_usage_inboxes" {
			if err := sqlDB.Close(); err != nil {
				t.Errorf("close database before transaction: %v", err)
			}
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil {
		t.Fatalf("expected transaction start failure, got nil")
	}
	if result == nil || result.Status != "failed" || result.ProcessedRows != 1 || result.BatchFull {
		t.Fatalf("expected failed result with one processed row, got %+v", result)
	}
}

func TestProcessRedisUsageInboxNotifiesUsageHeaderQuotaAfterTransactionCommit(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(fmt.Sprintf("accepted=%t", allowed), func(t *testing.T) {
			db := openSyncTestDatabase(t)
			if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
				Source: redisUsageInboxTestSource,
				RawMessage: `{
			"timestamp":"2026-06-22T11:10:43+08:00",
			"provider":"codex",
			"auth_type":"oauth",
			"auth_index":"codex-auth",
			"model":"gpt-5.5",
			"request_id":"header-quota-commit",
			"tokens":{"input_tokens":1,"output_tokens":2},
			"response_headers":{
				"X-Codex-Plan-Type":["pro"],
				"X-Codex-Primary-Used-Percent":["4"],
				"X-Codex-Primary-Window-Minutes":["300"],
				"X-Codex-Primary-Reset-After-Seconds":["60"]
			}
		}`,
				PoppedAt: time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local),
			}}); err != nil {
				t.Fatalf("seed inbox row: %v", err)
			}
			appender := &recordingUsageHeaderQuotaAppender{allowed: allowed}
			service := NewSyncServiceWithOptions(db, SyncServiceOptions{
				BaseURL:                  "https://cpa.example.com",
				UsageAggregationNotifier: appender,
				UsageHeaderQuota:         appender,
			})

			result, err := service.ProcessRedisUsageInbox(context.Background())
			if err != nil {
				t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
			}
			if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
				t.Fatalf("unexpected process result: %+v", result)
			}
			if appender.eventCalls != 1 || appender.calls != 1 || len(appender.snapshots) != 1 {
				t.Fatalf("expected one usage header quota notification, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
			}
			snapshot := appender.snapshots[0]
			if snapshot.AuthType != "oauth" || snapshot.AuthIndex != "codex-auth" || snapshot.Provider != "codex" {
				t.Fatalf("unexpected snapshot identity: %+v", snapshot)
			}
			if codexSnapshotPlan(snapshot) != "pro" {
				t.Fatalf("expected decoded Codex snapshot, got %#v", snapshot.CacheOutput)
			}
		})
	}
}

func TestProcessRedisUsageInboxNotifiesAggregationRunnerBeforeOverviewAggregation(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source: redisUsageInboxTestSource,
		RawMessage: `{
			"timestamp":"2026-06-22T11:10:43+08:00",
			"provider":"codex",
			"auth_type":"oauth",
			"auth_index":"codex-auth",
			"model":"gpt-5.5",
			"request_id":"header-quota-after-aggregation",
			"tokens":{"input_tokens":10,"output_tokens":20,"total_tokens":30},
			"response_headers":{
				"X-Codex-Primary-Used-Percent":["4"],
				"X-Codex-Primary-Window-Minutes":["300"],
				"X-Codex-Primary-Reset-After-Seconds":["60"]
			}
		}`,
		PoppedAt: time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	appender := &aggregationAwareUsageHeaderQuotaAppender{db: db}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		UsageAggregationNotifier: appender,
		UsageHeaderQuota:         appender,
		Now:                      func() time.Time { return time.Date(2026, 6, 22, 11, 15, 0, 0, time.Local) },
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if appender.calls != 1 || len(appender.snapshots) != 1 {
		t.Fatalf("expected one usage header quota notification, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
	}
	if appender.countErr != nil {
		t.Fatalf("count hourly stats during header notification: %v", appender.countErr)
	}
	if appender.hourlyStatsAtAppend != 0 {
		t.Fatalf("expected notifier before background overview aggregation, got %d hourly rows", appender.hourlyStatsAtAppend)
	}
}

func TestProcessRedisUsageInboxForwardsStructuredUsageHeaderSnapshotsToQuotaService(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{
		{
			Source: redisUsageInboxTestSource,
			RawMessage: `{
				"timestamp":"2026-06-22T11:00:00+08:00",
				"provider":"codex",
				"auth_type":"oauth",
				"auth_index":"codex-auth",
				"model":"gpt-5.5",
				"request_id":"header-quota-old",
				"tokens":{"input_tokens":1,"output_tokens":2},
				"response_headers":{
					"X-Codex-Primary-Used-Percent":["4"],
					"X-Codex-Primary-Window-Minutes":["300"],
					"X-Codex-Primary-Reset-After-Seconds":["60"]
				}
			}`,
			PoppedAt: time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		},
		{
			Source: redisUsageInboxTestSource,
			RawMessage: `{
				"timestamp":"2026-06-22T11:02:00+08:00",
				"provider":"codex",
				"auth_type":"oauth",
				"auth_index":"codex-auth",
				"model":"gpt-5.5",
				"request_id":"header-quota-new",
				"tokens":{"input_tokens":1,"output_tokens":2},
				"response_headers":{
					"X-Codex-Primary-Used-Percent":["8"],
					"X-Codex-Primary-Window-Minutes":["300"],
					"X-Codex-Primary-Reset-After-Seconds":["60"]
				}
			}`,
			PoppedAt: time.Date(2026, 6, 22, 11, 2, 0, 0, time.Local),
		},
		{
			Source: redisUsageInboxTestSource,
			RawMessage: `{
				"timestamp":"2026-06-22T11:01:00+08:00",
				"provider":"codex",
				"auth_type":"oauth",
				"auth_index":"other-codex-auth",
				"model":"gpt-5.5",
				"request_id":"header-quota-other",
				"tokens":{"input_tokens":1,"output_tokens":2},
				"response_headers":{
					"X-Codex-Primary-Used-Percent":["20"],
					"X-Codex-Primary-Window-Minutes":["300"],
					"X-Codex-Primary-Reset-After-Seconds":["60"]
				}
			}`,
			PoppedAt: time.Date(2026, 6, 22, 11, 1, 0, 0, time.Local),
		},
	}); err != nil {
		t.Fatalf("seed inbox rows: %v", err)
	}
	appender := &recordingUsageHeaderQuotaAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		UsageAggregationNotifier: appender,
		UsageHeaderQuota:         appender,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 3 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if appender.calls != 1 || len(appender.snapshots) != 3 {
		t.Fatalf("expected three structured snapshots for quota-side fan-out, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
	}
	firstUsed, firstOK := codexSnapshotPrimaryUsedPercent(appender.snapshots[0])
	if appender.snapshots[0].AuthIndex != "codex-auth" || !firstOK || firstUsed != 4 {
		t.Fatalf("expected first duplicate identity snapshot to remain structured, got %+v", appender.snapshots[0])
	}
	secondUsed, secondOK := codexSnapshotPrimaryUsedPercent(appender.snapshots[1])
	if appender.snapshots[1].AuthIndex != "codex-auth" || !secondOK || secondUsed != 8 {
		t.Fatalf("expected newer duplicate identity snapshot to remain structured, got %+v", appender.snapshots[1])
	}
	thirdUsed, thirdOK := codexSnapshotPrimaryUsedPercent(appender.snapshots[2])
	if appender.snapshots[2].AuthIndex != "other-codex-auth" || !thirdOK || thirdUsed != 20 {
		t.Fatalf("expected other identity snapshot to preserve order, got %+v", appender.snapshots[2])
	}
}

func TestProcessRedisUsageInboxIgnoresIncompleteUsageHeaderQuotaSnapshotDuringCoalesce(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{
		{
			Source: redisUsageInboxTestSource,
			RawMessage: `{
				"timestamp":"2026-06-22T11:00:00+08:00",
				"provider":"codex",
				"auth_type":"oauth",
				"auth_index":"codex-auth",
				"model":"gpt-5.5",
				"request_id":"header-quota-valid-earlier",
				"tokens":{"input_tokens":1,"output_tokens":2},
				"response_headers":{
					"X-Codex-Primary-Used-Percent":["4"],
					"X-Codex-Primary-Window-Minutes":["300"],
					"X-Codex-Primary-Reset-After-Seconds":["60"]
				}
			}`,
			PoppedAt: time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		},
		{
			Source: redisUsageInboxTestSource,
			RawMessage: `{
				"timestamp":"2026-06-22T11:02:00+08:00",
				"provider":"codex",
				"auth_type":"oauth",
				"auth_index":"codex-auth",
				"model":"gpt-5.5",
				"request_id":"header-quota-incomplete-later",
				"tokens":{"input_tokens":1,"output_tokens":2},
				"response_headers":{
					"X-Codex-Primary-Used-Percent":["8"],
					"X-Codex-Primary-Window-Minutes":["300"]
				}
			}`,
			PoppedAt: time.Date(2026, 6, 22, 11, 2, 0, 0, time.Local),
		},
	}); err != nil {
		t.Fatalf("seed inbox rows: %v", err)
	}
	appender := &recordingUsageHeaderQuotaAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		UsageAggregationNotifier: appender,
		UsageHeaderQuota:         appender,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 2 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if appender.calls != 1 || len(appender.snapshots) != 1 {
		t.Fatalf("expected only one complete usage header snapshot, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
	}
	if used, ok := codexSnapshotPrimaryUsedPercent(appender.snapshots[0]); !ok || used != 4 {
		t.Fatalf("expected incomplete later header to be filtered before coalesce, got %+v", appender.snapshots[0])
	}
}

func TestProcessRedisUsageInboxRollsBackEventsAndNotificationsWhenProcessedMarkFails(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source: redisUsageInboxTestSource,
		RawMessage: `{
			"timestamp":"2026-06-22T11:10:43+08:00",
			"provider":"codex",
			"auth_type":"oauth",
			"auth_index":"codex-auth",
			"model":"gpt-5.5",
			"request_id":"header-quota-rollback",
			"tokens":{"input_tokens":1,"output_tokens":2},
			"response_headers":{
				"X-Codex-Primary-Used-Percent":["4"],
				"X-Codex-Primary-Window-Minutes":["300"],
				"X-Codex-Primary-Reset-After-Seconds":["60"]
			}
		}`,
		PoppedAt: time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_header_quota_mark BEFORE UPDATE OF status ON redis_usage_inboxes WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'processed mark failed'); END;`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	appender := &recordingUsageHeaderQuotaAppender{allowed: true}
	cache := &recordingRecentUsageAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		UsageAggregationNotifier: appender,
		UsageHeaderQuota:         appender,
		RecentUsageEvents:        cache,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "processed mark failed") {
		t.Fatalf("expected transaction failure, got %v", err)
	}
	if appender.eventCalls != 0 || appender.calls != 0 || len(appender.snapshots) != 0 {
		t.Fatalf("expected no usage header quota notification on rollback, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
	}
	if result == nil || result.Status != "failed" || result.ProcessedRows != 1 || result.BatchFull {
		t.Fatalf("expected failed result with one processed row, got %+v", result)
	}
	if cache.calls != 0 || len(cache.events) != 0 {
		t.Fatalf("expected no recent cache notification on rollback, got %+v", cache)
	}
	assertUsageEventCount(t, db, 0)
}

func TestProcessRedisUsageInboxNotifiesEventsWithoutUsageHeaderQuotaSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-06-22T11:10:43+08:00","provider":"codex","auth_type":"oauth","auth_index":"codex-auth","model":"gpt-5.5","request_id":"header-quota-missing","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	appender := &recordingUsageHeaderQuotaAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		UsageAggregationNotifier: appender,
		UsageHeaderQuota:         appender,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if appender.eventCalls != 1 || appender.calls != 0 || len(appender.snapshots) != 0 {
		t.Fatalf("expected event wake without quota append, got events=%d calls=%d snapshots=%+v", appender.eventCalls, appender.calls, appender.snapshots)
	}
}

func TestProcessRedisUsageInboxSkipsAggregationWhenInboxAndEventsAreEmpty(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" || result.ProcessedRows != 0 || result.BatchFull {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	var checkpointCount int64
	if err := db.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count overview aggregation checkpoint: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("expected empty process without usage events not to create aggregation checkpoint, got %d", checkpointCount)
	}
}

func TestProcessRedisUsageInboxLeavesOverviewCatchUpToRunnerWhenInboxIsEmpty(t *testing.T) {
	// 准备：插入尚未聚合的 raw event，并显式注入生产 aggregation notifier。
	db := openSyncTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "stale-event", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC), TotalTokens: 10,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	notifier := &recordingUsageHeaderQuotaAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com", UsageAggregationNotifier: notifier})

	// 执行：空 inbox 不替后台 Runner 追平启动前已存在的 event。
	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	// 断言：生产 notifier 路径保持 Overview checkpoint 不变，等待 Runner startup wake。
	var checkpointCount int64
	if err := db.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count overview aggregation checkpoints: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("expected empty process to leave catch-up to runner, got %d checkpoints", checkpointCount)
	}
}

func TestProcessRedisUsageInboxNormalizesClaudeTokensForOAuthProvider(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db, `{
			"timestamp":"2026-04-27T08:00:00Z",
			"provider":"claude",
			"auth_type":"oauth",
			"auth_index":"auth-claude",
			"model":"claude-sonnet",
			"request_id":"oauth-claude-cache",
			"tokens":{
				"input_tokens":100,
				"output_tokens":30,
				"cached_tokens":999,
				"cache_read_tokens":20,
				"cache_creation_tokens":10,
				"total_tokens":160
			}
		}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	event := loadUsageEventByKey(t, db, "oauth-claude-cache")
	if event.InputTokens != 130 || event.CachedTokens != 20 || event.CacheReadTokens != 20 || event.CacheCreationTokens != 10 || event.OutputTokens != 30 || event.TotalTokens != 160 {
		t.Fatalf("expected Claude oauth tokens to be normalized, got %+v", event)
	}
}

func TestProcessRedisUsageInboxNormalizesAPIKeyTokensByUsageIdentityType(t *testing.T) {
	db := openSyncTestDatabase(t)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Claude Provider",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "provider-auth-index",
		Type:         "claude",
		Provider:     "Team Display Name",
	}).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	seedRedisInboxMessagesForTest(t, db, `{
			"timestamp":"2026-04-27T08:00:00Z",
			"provider":"Team Display Name",
			"auth_type":"api_key",
			"auth_index":"provider-auth-index",
			"model":"claude-sonnet",
			"request_id":"apikey-claude-cache",
			"tokens":{
				"input_tokens":100,
				"output_tokens":30,
				"cache_read_tokens":20,
				"cache_creation_tokens":10,
				"total_tokens":160
			}
		}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	if _, err := service.ProcessRedisUsageInbox(context.Background()); err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	event := loadUsageEventByKey(t, db, "apikey-claude-cache")
	if event.InputTokens != 130 || event.CachedTokens != 20 || event.CacheReadTokens != 20 || event.CacheCreationTokens != 10 || event.OutputTokens != 30 || event.TotalTokens != 160 {
		t.Fatalf("expected API key identity type to drive Claude normalization, got %+v", event)
	}
}

func TestProcessRedisUsageInboxNormalizesGeminiFamilyToCodexTokenFormat(t *testing.T) {
	db := openSyncTestDatabase(t)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Gemini CLI",
		AuthType:     entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     "gemini-auth-index",
		Type:         "gemini-cli",
		Provider:     "Gemini",
	}).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	seedRedisInboxMessagesForTest(t, db, `{
			"timestamp":"2026-04-27T08:00:00Z",
			"provider":"Google Account",
			"auth_type":"oauth",
			"auth_index":"gemini-auth-index",
			"model":"gemini-2.5-pro",
			"request_id":"gemini-thinking",
			"tokens":{
				"input_tokens":11,
				"output_tokens":7,
				"reasoning_tokens":3,
				"cached_tokens":5,
				"total_tokens":21
			}
		}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	if _, err := service.ProcessRedisUsageInbox(context.Background()); err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	event := loadUsageEventByKey(t, db, "gemini-thinking")
	if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
		t.Fatalf("expected Gemini family tokens to be normalized to Codex format, got %+v", event)
	}
}

func TestProcessRedisUsageInboxDoesNotFallbackWhenUsageTypeLookupErrors(t *testing.T) {
	db := openSyncTestDatabase(t)
	rows := seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"Team Display Name","auth_type":"apikey","auth_index":"provider-auth-index","model":"claude-sonnet","request_id":"type-lookup-error","tokens":{"input_tokens":100,"output_tokens":30,"cache_read_tokens":20,"cache_creation_tokens":10,"total_tokens":160}}`)
	if err := db.Migrator().DropTable(&entities.UsageIdentity{}); err != nil {
		t.Fatalf("drop usage identity table: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "load active usage identity types for redis usage") {
		t.Fatalf("expected usage type lookup error, got result=%+v err=%v", result, err)
	}
	// type 查询失败也应保留本轮取出的 inbox 行数，供 runner 和日志判断批次状态。
	if result == nil || result.Status != "failed" || result.ProcessedRows != 1 || result.BatchFull {
		t.Fatalf("expected failed result, got %+v", result)
	}
	assertUsageEventCount(t, db, 0)
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessFailed || !strings.Contains(inbox.LastError, "load active usage identity types for redis usage") {
		t.Fatalf("expected inbox row process_failed with lookup error, got %+v", inbox)
	}
}

func TestBuildUsageEventTypeResolverBatchesAPIKeyIdentityLookup(t *testing.T) {
	db := openSyncTestDatabase(t)
	identities := make([]entities.UsageIdentity, 0, 901)
	events := make([]entities.UsageEvent, 0, 901)
	for i := 0; i < 901; i++ {
		authIndex := fmt.Sprintf("provider-auth-%03d", i)
		identities = append(identities, entities.UsageIdentity{
			Name:         authIndex,
			AuthType:     entities.UsageIdentityAuthTypeAIProvider,
			AuthTypeName: "apikey",
			Identity:     authIndex,
			Type:         "claude",
			Provider:     "Claude",
		})
		events = append(events, entities.UsageEvent{
			AuthType:  "apikey",
			AuthIndex: authIndex,
		})
	}
	if err := db.CreateInBatches(&identities, 100).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}
	messages := make([]string, len(events))
	for i, event := range events {
		messages[i] = fmt.Sprintf(`{"timestamp":"2026-04-27T08:00:00Z","request_id":"%s","auth_type":"apikey","auth_index":"%s","model":"claude-sonnet","tokens":{"input_tokens":100,"output_tokens":30,"cache_read_tokens":20,"cache_creation_tokens":10,"total_tokens":160}}`, event.AuthIndex, event.AuthIndex)
	}
	seedRedisInboxMessagesForTest(t, db, messages...)
	usageIdentityQueries := 0
	callbackName := "test:capture_usage_identity_type_lookup_batches"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_identities" {
			usageIdentityQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	notifier := &recordingUsageAggregationNotifier{}
	syncer := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com", UsageAggregationNotifier: notifier})
	if _, err := syncer.ProcessRedisUsageInbox(context.Background()); err != nil {
		t.Fatalf("process identity batch: %v", err)
	}
	if len(notifier.events) != len(events) {
		t.Fatalf("expected %d resolved events, got %d", len(events), len(notifier.events))
	}
	for _, event := range notifier.events {
		if event.InputTokens != 130 || event.TotalTokens != 160 {
			t.Fatalf("expected Claude identity normalization for %q, got %+v", event.AuthIndex, event)
		}
	}
	if usageIdentityQueries != 2 {
		t.Fatalf("expected 901 auth indexes to be loaded in two SELECT batches, got %d queries", usageIdentityQueries)
	}
}

func TestBuildUsageEventTypeResolverIgnoresBlankActiveType(t *testing.T) {
	db := openSyncTestDatabase(t)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Blank Active",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "blank-active-auth-index",
		Type:         " ",
		Provider:     "Blank",
	}).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}

	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","request_id":"blank-active-type","auth_type":"apikey","auth_index":"blank-active-auth-index","model":"claude-sonnet","tokens":{"input_tokens":100,"output_tokens":30,"cache_read_tokens":20,"cache_creation_tokens":10}}`)
	syncer := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com", UsageAggregationNotifier: &recordingUsageAggregationNotifier{}})
	if _, err := syncer.ProcessRedisUsageInbox(context.Background()); err != nil {
		t.Fatalf("process blank identity type: %v", err)
	}
	event := loadUsageEventByKey(t, db, "blank-active-type")
	if event.InputTokens != 100 || event.OutputTokens != 30 || event.TotalTokens != 130 {
		t.Fatalf("expected blank active type to use strict token fallback, got %+v", event)
	}
}

func TestProcessRedisUsageInboxFallsBackToDeletedUsageIdentityType(t *testing.T) {
	db := openSyncTestDatabase(t)
	deletedAt := time.Date(2026, 4, 26, 8, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Deleted Claude Provider",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "deleted-auth-index",
		Type:         "claude",
		Provider:     "Deleted Team",
		IsDeleted:    true,
		DeletedAt:    &deletedAt,
	}).Error; err != nil {
		t.Fatalf("seed deleted usage identity: %v", err)
	}
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"Deleted Team","auth_type":"apikey","auth_index":"deleted-auth-index","model":"claude-sonnet","request_id":"deleted-identity-claude","tokens":{"input_tokens":100,"output_tokens":30,"cache_read_tokens":20,"cache_creation_tokens":10,"total_tokens":160}}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	if _, err := service.ProcessRedisUsageInbox(context.Background()); err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	event := loadUsageEventByKey(t, db, "deleted-identity-claude")
	if event.InputTokens != 130 || event.CachedTokens != 20 {
		t.Fatalf("expected deleted identity metadata fallback to normalize Claude tokens, got %+v", event)
	}
}

func TestProcessRedisUsageInboxUsesStrictTokensForKimiAndMissingType(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Kimi Provider",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "kimi-auth-index",
		Type:         "kimi",
		Provider:     "Kimi",
	}).Error; err != nil {
		t.Fatalf("seed kimi usage identity: %v", err)
	}
	_, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{
		{
			Source:     redisUsageInboxTestSource,
			RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"Kimi","auth_type":"apikey","auth_index":"kimi-auth-index","model":"kimi-k2","request_id":"kimi-openai-style","tokens":{"input_tokens":100,"output_tokens":30,"cached_tokens":20,"cache_read_tokens":20,"cache_creation_tokens":10}}`,
			PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
		},
		{
			Source:     redisUsageInboxTestSource,
			RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"Unknown","auth_type":"apikey","auth_index":"missing-auth-index","model":"unknown-model","request_id":"missing-type-default-style","tokens":{"input_tokens":100,"output_tokens":30,"reasoning_tokens":5,"cached_tokens":20,"cache_read_tokens":20,"cache_creation_tokens":10,"total_tokens":135}}`,
			PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("seed inbox rows: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	if _, err := service.ProcessRedisUsageInbox(context.Background()); err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	cases := map[string]struct {
		outputTokens    int64
		reasoningTokens int64
		totalTokens     int64
	}{
		"kimi-openai-style":          {outputTokens: 30, totalTokens: 130},
		"missing-type-default-style": {outputTokens: 30, reasoningTokens: 5, totalTokens: 135},
	}
	for eventKey, expected := range cases {
		event := loadUsageEventByKey(t, db, eventKey)
		if event.InputTokens != 100 ||
			event.CachedTokens != 20 ||
			event.CacheReadTokens != 20 ||
			event.CacheCreationTokens != 10 ||
			event.OutputTokens != expected.outputTokens ||
			event.ReasoningTokens != expected.reasoningTokens ||
			event.TotalTokens != expected.totalTokens {
			t.Fatalf("expected %s to use strict token normalization, got %+v", eventKey, event)
		}
	}
	// Token 入站日志不再输出可能对应邮箱或凭证标识的 auth_index，改用安全 event_key 定位该条事件。
	if output := logs.String(); !strings.Contains(output, "usage identity type not found for redis usage event") || !strings.Contains(output, "missing-type-default-style") {
		t.Fatalf("expected missing type warning log, got:\n%s", output)
	}
}

func seedRedisInboxMessagesForTest(t *testing.T, db *gorm.DB, messages ...string) []entities.RedisUsageInbox {
	t.Helper()
	inputs := make([]dto.RedisInboxInsert, 0, len(messages))
	for _, message := range messages {
		inputs = append(inputs, dto.RedisInboxInsert{
			Source:     redisUsageInboxTestSource,
			RawMessage: message,
			PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
		})
	}
	rows, err := repository.InsertRedisUsageInboxMessages(db, inputs)
	if err != nil {
		t.Fatalf("seed redis inbox messages: %v", err)
	}
	return rows
}

func TestProcessRedisUsageInboxPersistsValidRowsWhenBatchContainsMalformedMessage(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db,
		`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-valid","tokens":{"input_tokens":1,"output_tokens":2}}`,
		`{bad-json}`,
	)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	// 混合好坏消息应把坏消息也计入本轮取出的 inbox 行数。
	if result == nil || result.Status != "completed_with_warnings" || result.InsertedEvents != 1 || result.ProcessedRows != 2 || result.BatchFull {
		t.Fatalf("expected warning result with valid event persisted, got %+v", result)
	}

	var event entities.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-valid" {
		t.Fatalf("unexpected usage event: %+v", event)
	}

	var inboxRows []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&inboxRows).Error; err != nil {
		t.Fatalf("load inbox rows: %v", err)
	}
	if len(inboxRows) != 2 {
		t.Fatalf("expected 2 inbox rows, got %d", len(inboxRows))
	}
	if inboxRows[0].Status != repository.RedisUsageInboxStatusProcessed || inboxRows[0].UsageEventKey != "redis-valid" {
		t.Fatalf("expected first row processed, got %+v", inboxRows[0])
	}
	if inboxRows[1].Status != repository.RedisUsageInboxStatusDecodeFailed || inboxRows[1].LastError == "" {
		t.Fatalf("expected second row decode_failed, got %+v", inboxRows[1])
	}
}

func TestProcessRedisUsageInboxMarksMalformedOnlyBatchWithoutSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db, `{bad-json}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	// 全坏消息也应报告本轮取出的 1 行，避免插入数为 0 时丢失批次信号。
	if result == nil || result.Status != "completed_with_warnings" || result.ProcessedRows != 1 || result.BatchFull {
		t.Fatalf("expected warning result, got %+v", result)
	}

	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusDecodeFailed || inbox.RawMessage != `{bad-json}` {
		t.Fatalf("expected decode_failed raw inbox row, got %+v", inbox)
	}
}

func TestProcessRedisUsageInboxMarksFullMalformedBatch(t *testing.T) {
	db := openSyncTestDatabase(t)
	messages := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		// 每条坏消息内容不同，便于插入 inbox 时保持独立行。
		messages = append(messages, fmt.Sprintf("{bad-json-%d}", i))
	}
	seedRedisInboxMessagesForTest(t, db, messages...)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	// 即使没有事件写入，满批坏消息也应报告 BatchFull，供 runner 继续 drain。
	if result == nil || result.Status != "completed_with_warnings" || result.ProcessedRows != 1000 || !result.BatchFull {
		t.Fatalf("expected warning result with full malformed batch, got %+v", result)
	}
}

func TestProcessRedisUsageInboxLogsErrorAndMarksDecodeFailedWhenRequestIDMissing(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","tokens":{"input_tokens":1,"output_tokens":2}}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "request_id is required") {
		t.Fatalf("expected missing request_id warning, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" {
		t.Fatalf("expected warning result, got %+v", result)
	}

	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusDecodeFailed || !strings.Contains(inbox.LastError, "request_id is required") {
		t.Fatalf("expected missing request_id decode_failed row, got %+v", inbox)
	}
	output := logs.String()
	if !strings.Contains(output, "level=error") || !strings.Contains(output, "redis usage message decode failed") || !strings.Contains(output, "request_id is required") {
		t.Fatalf("expected missing request_id error log, got:\n%s", output)
	}
}

func TestProcessRedisUsageInboxDoesNotWatermarkFilterRedisInboxEvents(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "future-watermark",
		APIGroupKey: "claude",
		Model:       "sonnet",
		Timestamp:   time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed future event: %v", err)
	}
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-26T07:00:00Z","provider":"claude","model":"sonnet","request_id":"old-but-unique","tokens":{"input_tokens":1,"output_tokens":2}}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected old unique Redis event to insert despite watermark, got %+v", result)
	}

	var event entities.UsageEvent
	if err := db.Where("event_key = ?", "old-but-unique").First(&event).Error; err != nil {
		t.Fatalf("load old unique Redis event: %v", err)
	}
}

func TestProcessRedisUsageInboxRetriesProcessFailedInbox(t *testing.T) {
	db := openSyncTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"retry-process-failed","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   poppedAt,
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	if err := repository.MarkRedisUsageInboxProcessFailed(db, rows[0].ID, errors.New("temporary insert failure")); err != nil {
		t.Fatalf("mark process failed: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected process_failed row retry to insert, got %+v", result)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.LastError != "" {
		t.Fatalf("expected retried row processed and error cleared, got %+v", inbox)
	}
}

func TestProcessRedisUsageInboxKeepsDistinctRedisRequestIDsWithSameEventFields(t *testing.T) {
	db := openSyncTestDatabase(t)
	message := `{"timestamp":"2026-04-27T08:00:00Z","latency_ms":123,"source":"codex-a","auth_index":"1","failed":false,"api_key":"external-api-key","model":"claude-sonnet","request_id":"redis-request-1","tokens":{"input_tokens":10,"output_tokens":20,"reasoning_tokens":5,"cached_tokens":4,"total_tokens":39}}`
	seedRedisInboxMessagesForTest(t, db, message, strings.Replace(message, "redis-request-1", "redis-request-2", 1))
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	if result.InsertedEvents != 2 || result.DedupedEvents != 0 {
		t.Fatalf("expected distinct Redis request IDs to insert separately, got %+v", result)
	}
	assertUsageEventCount(t, db, 2)
}

func TestProcessRedisUsageInboxWritesDebugLogsWithoutRawPayload(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)

	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-log","api_key":"raw-secret-key","tokens":{"input_tokens":1,"output_tokens":2}}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	_, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	output := logs.String()
	if !strings.Contains(output, "redis usage inbox rows processed") {
		t.Fatalf("expected process debug log in output:\n%s", output)
	}
	if strings.Contains(output, "raw-secret-key") || strings.Contains(output, "redis-log") {
		t.Fatalf("debug logs should not include raw payload fields, got:\n%s", output)
	}
}

func TestNewSyncServiceBuildsClientFromConfig(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncService(db, config.Config{
		CPABaseURL:       " https://cpa.example.com ",
		CPAManagementKey: "secret",
		RequestTimeout:   5 * time.Second,
	})
	if service == nil || reflect.ValueOf(service).Elem().FieldByName("client").IsNil() {
		t.Fatal("expected sync service client to be initialized")
	}
	if reflect.ValueOf(service).Elem().FieldByName("baseURL").String() != "https://cpa.example.com" {
		t.Fatalf("expected trimmed base url, got %q", reflect.ValueOf(service).Elem().FieldByName("baseURL").String())
	}
}

func assertUsageEventCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d usage events, got %d", expected, count)
	}
}

func openSyncTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "sync.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}

func closeTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
}

func loadUsageEventByKey(t *testing.T, db *gorm.DB, eventKey string) entities.UsageEvent {
	t.Helper()
	var event entities.UsageEvent
	if err := db.Where("event_key = ?", eventKey).First(&event).Error; err != nil {
		t.Fatalf("load usage event %q: %v", eventKey, err)
	}
	return event
}

func captureSyncDebugLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	logs := &bytes.Buffer{}
	previousOutput := logrus.StandardLogger().Out
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(logs)
	logrus.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetLevel(previousLevel)
	})
	return logs
}
