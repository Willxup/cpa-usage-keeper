package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const redisUsageInboxTestSource = "redis_pull:usage"

type recordingRecentUsageAppender struct {
	calls   int
	events  []entities.UsageEvent
	allowed bool
}

type recordingUsageHeaderQuotaAppender struct {
	calls     int
	snapshots []quota.UsageHeaderSnapshot
	allowed   bool
}

type aggregationAwareUsageHeaderQuotaAppender struct {
	db                  *gorm.DB
	calls               int
	snapshots           []quota.UsageHeaderSnapshot
	hourlyStatsAtAppend int64
	countErr            error
}

func (r *recordingRecentUsageAppender) TryAppend(events []entities.UsageEvent) bool {
	r.calls++
	r.events = append(r.events, events...)
	return r.allowed
}

func (r *recordingUsageHeaderQuotaAppender) TryAppendUsageHeaderSnapshots(snapshots []quota.UsageHeaderSnapshot) bool {
	r.calls++
	r.snapshots = append(r.snapshots, snapshots...)
	return r.allowed
}

func (r *aggregationAwareUsageHeaderQuotaAppender) TryAppendUsageHeaderSnapshots(snapshots []quota.UsageHeaderSnapshot) bool {
	r.calls++
	r.snapshots = append(r.snapshots, snapshots...)
	r.countErr = r.db.Model(&entities.UsageOverviewHourlyStat{}).Where("auth_index = ?", "codex-auth").Count(&r.hourlyStatsAtAppend).Error
	return true
}

func TestProcessRedisUsageInboxPersistsEventsWithoutSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","endpoint":"/v1/messages","auth_type":"api_key","model":"sonnet","request_id":"process-only","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
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
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", "overview").First(&checkpoint).Error; err != nil {
		t.Fatalf("expected overview aggregation checkpoint after processing inbox: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != event.ID {
		t.Fatalf("expected overview checkpoint to aggregate through event %d, got %+v", event.ID, checkpoint)
	}
}

func TestProcessRedisUsageInboxNotifiesRecentCacheAfterTransactionCommit(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","auth_type":"oauth","source":"auth-user@example.com","auth_index":"auth-1","model":"sonnet","request_id":"notify-cache","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	cache := &recordingRecentUsageAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:           "https://cpa.example.com",
		RecentUsageEvents: cache,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if cache.calls != 1 || len(cache.events) != 1 {
		t.Fatalf("expected one recent cache notification, got calls=%d events=%+v", cache.calls, cache.events)
	}
	if cache.events[0].EventKey != "notify-cache" || cache.events[0].AuthIndex != "auth-1" {
		t.Fatalf("unexpected notified event: %+v", cache.events[0])
	}
}

func TestProcessRedisUsageInboxDoesNotNotifyRecentCacheOnRollback(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"rollback-cache-notify","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_recent_cache_mark BEFORE UPDATE OF status ON redis_usage_inboxes WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'processed mark failed'); END;`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	cache := &recordingRecentUsageAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:           "https://cpa.example.com",
		RecentUsageEvents: cache,
	})

	// 执行本地 inbox 处理，触发事务回滚路径。
	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "processed mark failed") {
		t.Fatalf("expected transaction failure, got %v", err)
	}
	// 事务失败也应返回本轮取出的 inbox 行数，避免 runner 日志丢失批次信号。
	if result == nil || result.Status != "failed" || result.ProcessedRows != 1 || result.BatchFull {
		t.Fatalf("expected failed result with one processed row, got %+v", result)
	}
	if cache.calls != 0 || len(cache.events) != 0 {
		t.Fatalf("expected no cache notification on rollback, got calls=%d events=%+v", cache.calls, cache.events)
	}
}

func TestProcessRedisUsageInboxReturnsBatchSignalWhenTransactionCannotStart(t *testing.T) {
	// 准备独立测试数据库，避免关闭连接影响其它用例。
	db := openSyncTestDatabase(t)
	// 写入一条不需要 identity 查询的消息，使下一次数据库访问发生在事务开始阶段。
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"transaction-start-fails","tokens":{"input_tokens":1,"output_tokens":2}}`)
	// callbackClosed 保护测试回调只关闭一次底层连接。
	callbackClosed := false
	// callbackName 使用测试专属名称，避免污染同一进程里的其它 GORM 回调。
	callbackName := "test:close_db_after_redis_inbox_list"
	// 注册查询后回调，在取出 redis_usage_inboxes 后关闭底层连接。
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		// 只在目标表查询后触发，避免关闭迁移或 seed 阶段使用的连接。
		if callbackClosed || tx.Statement == nil || tx.Statement.Table != "redis_usage_inboxes" {
			// 非目标查询不做任何处理。
			return
		}
		// 标记已关闭，防止后续回调重复关闭连接。
		callbackClosed = true
		// 取出底层 sql.DB，用来模拟事务开始前连接不可用。
		sqlDB, dbErr := tx.DB()
		// 如果底层连接可取出，就关闭它制造事务启动失败。
		if dbErr == nil {
			// 关闭连接只作用于本测试临时数据库。
			_ = sqlDB.Close()
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	// 测试退出时尽力移除 callback，保持 GORM 回调链干净。
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	// 构造 sync service，走真实 ProcessRedisUsageInbox 链路。
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	// 执行处理，事务启动应因连接关闭而失败。
	result, err := service.ProcessRedisUsageInbox(context.Background())
	// 失败必须暴露给调用方，避免静默丢消息。
	if err == nil {
		t.Fatalf("expected transaction start failure, got nil")
	}
	// 即使事务未能开始，也应返回本轮取出的 inbox 行数。
	if result == nil || result.Status != "failed" || result.ProcessedRows != 1 || result.BatchFull {
		t.Fatalf("expected failed result with one processed row, got %+v", result)
	}
}

func TestProcessRedisUsageInboxIgnoresRecentCacheOverflow(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"cache-overflow","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	cache := &recordingRecentUsageAppender{allowed: false}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:           "https://cpa.example.com",
		RecentUsageEvents: cache,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox should ignore cache overflow, got %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if cache.calls != 1 {
		t.Fatalf("expected cache append attempt, got %d", cache.calls)
	}
}

func TestProcessRedisUsageInboxNotifiesUsageHeaderQuotaAfterTransactionCommit(t *testing.T) {
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
	appender := &recordingUsageHeaderQuotaAppender{allowed: true}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:          "https://cpa.example.com",
		UsageHeaderQuota: appender,
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
	snapshot := appender.snapshots[0]
	if snapshot.AuthType != "oauth" || snapshot.AuthIndex != "codex-auth" || snapshot.Provider != "codex" {
		t.Fatalf("unexpected snapshot identity: %+v", snapshot)
	}
	if snapshot.Headers.Get("X-Codex-Plan-Type") != "pro" {
		t.Fatalf("expected codex header snapshot, got %#v", snapshot.Headers)
	}
}

func TestProcessRedisUsageInboxNotifiesUsageHeaderQuotaAfterOverviewAggregation(t *testing.T) {
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
		BaseURL:          "https://cpa.example.com",
		UsageHeaderQuota: appender,
		Now:              func() time.Time { return time.Date(2026, 6, 22, 11, 15, 0, 0, time.Local) },
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
	if appender.hourlyStatsAtAppend == 0 {
		t.Fatal("expected usage overview hourly stats to be aggregated before header quota notification")
	}
}

func TestProcessRedisUsageInboxCoalescesUsageHeaderQuotaSnapshotsByAuthIndex(t *testing.T) {
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
		BaseURL:          "https://cpa.example.com",
		UsageHeaderQuota: appender,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 3 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if appender.calls != 1 || len(appender.snapshots) != 2 {
		t.Fatalf("expected latest snapshot per auth_index, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
	}
	snapshotsByAuthIndex := map[string]quota.UsageHeaderSnapshot{}
	for _, snapshot := range appender.snapshots {
		snapshotsByAuthIndex[snapshot.AuthIndex] = snapshot
	}
	if snapshotsByAuthIndex["codex-auth"].Headers.Get("X-Codex-Primary-Used-Percent") != "8" {
		t.Fatalf("expected latest codex-auth header snapshot, got %+v", snapshotsByAuthIndex["codex-auth"])
	}
	if snapshotsByAuthIndex["other-codex-auth"].Headers.Get("X-Codex-Primary-Used-Percent") != "20" {
		t.Fatalf("expected other auth header snapshot, got %+v", snapshotsByAuthIndex["other-codex-auth"])
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
		BaseURL:          "https://cpa.example.com",
		UsageHeaderQuota: appender,
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
	if appender.snapshots[0].Headers.Get("X-Codex-Primary-Used-Percent") != "4" {
		t.Fatalf("expected incomplete later header to be filtered before coalesce, got %+v", appender.snapshots[0])
	}
}

func TestProcessRedisUsageInboxDoesNotNotifyUsageHeaderQuotaOnRollback(t *testing.T) {
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
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:          "https://cpa.example.com",
		UsageHeaderQuota: appender,
	})

	_, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "processed mark failed") {
		t.Fatalf("expected transaction failure, got %v", err)
	}
	if appender.calls != 0 || len(appender.snapshots) != 0 {
		t.Fatalf("expected no usage header quota notification on rollback, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
	}
}

func TestProcessRedisUsageInboxIgnoresUsageHeaderQuotaOverflow(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source: redisUsageInboxTestSource,
		RawMessage: `{
			"timestamp":"2026-06-22T11:10:43+08:00",
			"provider":"codex",
			"auth_type":"oauth",
			"auth_index":"codex-auth",
			"model":"gpt-5.5",
			"request_id":"header-quota-overflow",
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
	appender := &recordingUsageHeaderQuotaAppender{allowed: false}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:          "https://cpa.example.com",
		UsageHeaderQuota: appender,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox should ignore usage header quota overflow, got %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if appender.calls != 1 {
		t.Fatalf("expected overflow appender to be attempted once, got %d", appender.calls)
	}
}

func TestProcessRedisUsageInboxSkipsRowsWithoutUsageHeaderQuotaSnapshot(t *testing.T) {
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
		BaseURL:          "https://cpa.example.com",
		UsageHeaderQuota: appender,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if appender.calls != 0 || len(appender.snapshots) != 0 {
		t.Fatalf("expected rows without response_headers to skip quota notification, got calls=%d snapshots=%+v", appender.calls, appender.snapshots)
	}
}

func TestProcessRedisUsageInboxRollsBackEventsWhenProcessedMarkFails(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"rollback-on-mark-failure","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_processed_mark BEFORE UPDATE OF status ON redis_usage_inboxes WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'processed mark failed'); END;`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	_, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "processed mark failed") {
		t.Fatalf("expected processed mark failure, got %v", err)
	}
	var eventCount int64
	if err := db.Model(&entities.UsageEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected usage event insert to roll back when inbox mark fails, got %d", eventCount)
	}
}

func TestProcessRedisUsageInboxSkipsAggregationWhenInboxAndEventsAreEmpty(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	var checkpointCount int64
	if err := db.Model(&entities.UsageOverviewAggregationCheckpoint{}).Where("name = ?", "overview").Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count overview aggregation checkpoint: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("expected empty process without usage events not to create aggregation checkpoint, got %d", checkpointCount)
	}
}

func TestProcessRedisUsageInboxRetriesOverviewAggregationWhenInboxIsEmpty(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "stale-event", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC), TotalTokens: 10,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", "overview").First(&checkpoint).Error; err != nil {
		t.Fatalf("expected overview aggregation checkpoint after empty process catch-up: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID == 0 {
		t.Fatalf("expected empty process catch-up to aggregate stale usage events, got %+v", checkpoint)
	}
}

func TestProcessRedisUsageInboxDoesNotFetchMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-no-metadata","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	// 不配置 metadata fetcher，证明 Redis usage 核心能够独立完成处理。
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "redis-no-metadata" {
		t.Fatalf("expected inbox row processed, got %+v", inbox)
	}
}

func TestProcessRedisUsageInboxNormalizesClaudeTokensForOAuthProvider(t *testing.T) {
	db := openSyncTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source: redisUsageInboxTestSource,
		RawMessage: `{
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
		}`,
		PoppedAt: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
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
	_, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source: redisUsageInboxTestSource,
		RawMessage: `{
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
		}`,
		PoppedAt: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
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
	_, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source: redisUsageInboxTestSource,
		RawMessage: `{
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
		}`,
		PoppedAt: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
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
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"Team Display Name","auth_type":"apikey","auth_index":"provider-auth-index","model":"claude-sonnet","request_id":"type-lookup-error","tokens":{"input_tokens":100,"output_tokens":30,"cache_read_tokens":20,"cache_creation_tokens":10,"total_tokens":160}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
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
	usageIdentityQueries := 0
	callbackName := "test:capture_usage_identity_type_lookup_batches"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.Contains(sql, "FROM `usage_identities`") || strings.Contains(sql, `FROM "usage_identities"`) {
			usageIdentityQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	resolver, err := buildUsageEventTypeResolver(context.Background(), db, events)
	if err != nil {
		t.Fatalf("buildUsageEventTypeResolver returned error: %v", err)
	}
	if len(resolver.byIdentity) != len(events) {
		t.Fatalf("expected resolver to load %d types, got %d", len(events), len(resolver.byIdentity))
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

	resolver, err := buildUsageEventTypeResolver(context.Background(), db, []entities.UsageEvent{{
		AuthType:  "apikey",
		AuthIndex: "blank-active-auth-index",
	}})
	if err != nil {
		t.Fatalf("buildUsageEventTypeResolver returned error: %v", err)
	}
	key := usageEventIdentityKey{authType: entities.UsageIdentityAuthTypeAIProvider, identity: "blank-active-auth-index"}
	if got := resolver.byIdentity[key]; got != "" {
		t.Fatalf("expected blank active type to remain unresolved for default token fallback, got %q", got)
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
	_, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"Deleted Team","auth_type":"apikey","auth_index":"deleted-auth-index","model":"claude-sonnet","request_id":"deleted-identity-claude","tokens":{"input_tokens":100,"output_tokens":30,"cache_read_tokens":20,"cache_creation_tokens":10,"total_tokens":160}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
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

func processRedisUsageInboxForTest(t *testing.T, service *SyncService) (*servicedto.RedisBatchSyncResult, error) {
	t.Helper()
	return service.ProcessRedisUsageInbox(context.Background())
}

func TestProcessRedisUsageInboxSkipsEmptyBatchWithoutSnapshotOrMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	// 空批处理不需要 metadata 依赖。
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := processRedisUsageInboxForTest(t, service)
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	// 空批结果应明确返回 0 行且非满批，避免 runner 误判为 backlog。
	if result == nil || !result.Empty || result.Status != "empty" || result.ProcessedRows != 0 || result.BatchFull {
		t.Fatalf("expected empty redis batch result, got %+v", result)
	}
}

func TestProcessRedisUsageInboxPersistsNonEmptyBatchWithoutMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-1","tokens":{"input_tokens":1,"output_tokens":2}}`)
	// 非空批同样只依赖本地 inbox 和 usage 仓储。
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := processRedisUsageInboxForTest(t, service)
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	// 单条成功消息应报告本轮取出 1 行且非满批，保持原有插入数量语义。
	if result == nil || result.Empty || result.Status != "completed" || result.InsertedEvents != 1 || result.DedupedEvents != 0 || result.ProcessedRows != 1 || result.BatchFull {
		t.Fatalf("unexpected redis batch result: %+v", result)
	}

	var event entities.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-1" {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "redis-1" {
		t.Fatalf("expected processed inbox row without snapshot link, got %+v", inbox)
	}
}

func TestProcessRedisUsageInboxPersistsValidRowsWhenBatchContainsMalformedMessage(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db,
		`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-valid","tokens":{"input_tokens":1,"output_tokens":2}}`,
		`{bad-json}`,
	)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := processRedisUsageInboxForTest(t, service)
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

	result, err := processRedisUsageInboxForTest(t, service)
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
	// 准备独立数据库，用真实 service 路径处理满批坏消息。
	db := openSyncTestDatabase(t)
	// messages 保存一整批无法解码的 Redis 原始消息。
	messages := make([]string, 0, redisInboxProcessLimit)
	// 构造刚好达到 Redis process 批次上限的坏消息集合。
	for i := 0; i < redisInboxProcessLimit; i++ {
		// 每条坏消息内容不同，便于插入 inbox 时保持独立行。
		messages = append(messages, fmt.Sprintf("{bad-json-%d}", i))
	}
	// 将满批坏消息写入 durable inbox，模拟真实 backlog 输入。
	seedRedisInboxMessagesForTest(t, db, messages...)
	// 构造 sync service，后续走真实 ProcessRedisUsageInbox 链路。
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	// 执行本地 inbox 处理，预期全部进入 decode_failed warning 路径。
	result, err := processRedisUsageInboxForTest(t, service)
	// 全坏消息应返回 decode warning，不能被静默吞掉。
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	// 即使没有事件写入，满批坏消息也应报告 BatchFull，供 runner 继续 drain。
	if result == nil || result.Status != "completed_with_warnings" || result.ProcessedRows != redisInboxProcessLimit || !result.BatchFull {
		t.Fatalf("expected warning result with full malformed batch, got %+v", result)
	}
}

func TestProcessRedisUsageInboxLogsErrorAndMarksDecodeFailedWhenRequestIDMissing(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","tokens":{"input_tokens":1,"output_tokens":2}}`)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := processRedisUsageInboxForTest(t, service)
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

func TestProcessRedisUsageInboxProcessesPendingInbox(t *testing.T) {
	db := openSyncTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"pending-1","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   poppedAt,
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := processRedisUsageInboxForTest(t, service)
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("expected pending inbox row to be processed, got %+v", result)
	}

	var event entities.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "pending-1" {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed {
		t.Fatalf("expected pending row processed, got %+v", inbox)
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

	result, err := processRedisUsageInboxForTest(t, service)
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

	result, err := processRedisUsageInboxForTest(t, service)
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

func TestProcessRedisUsageInboxUsesDurableInbox(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"sync-now-redis","tokens":{"input_tokens":1,"output_tokens":2}}`)
	// durable inbox 路径不配置 metadata 依赖。
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := processRedisUsageInboxForTest(t, service)
	if err != nil {
		t.Fatalf("process Redis usage inbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process Redis usage inbox result: %+v", result)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "sync-now-redis" {
		t.Fatalf("expected process Redis usage inbox redis path to use inbox, got %+v", inbox)
	}
}

func TestProcessRedisUsageInboxKeepsDistinctRedisRequestIDsWithSameEventFields(t *testing.T) {
	db := openSyncTestDatabase(t)
	timestamp := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	tokens := dto.TokenStats{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 4, TotalTokens: 39}
	seedRedisInboxMessagesForTest(t, db,
		equivalentRedisMessage("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, 123, tokens, "redis-request-1"),
		equivalentRedisMessage("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, 123, tokens, "redis-request-2"),
	)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := processRedisUsageInboxForTest(t, service)
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

	_, err := processRedisUsageInboxForTest(t, service)
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
	if service == nil || service.client == nil {
		t.Fatal("expected sync service client to be initialized")
	}
	if service.baseURL != "https://cpa.example.com" {
		t.Fatalf("expected trimmed base url, got %q", service.baseURL)
	}
}

func equivalentRedisMessage(apiGroupKey, model string, timestamp time.Time, source, authIndex string, failed bool, latencyMS int64, tokens dto.TokenStats, requestID string) string {
	failedValue := "false"
	if failed {
		failedValue = "true"
	}
	return `{"timestamp":"` + timestamp.UTC().Format(time.RFC3339) + `","latency_ms":` + int64String(latencyMS) + `,"source":"` + source + `","auth_index":"` + authIndex + `","failed":` + failedValue + `,"api_key":"` + apiGroupKey + `","model":"` + model + `","request_id":"` + requestID + `","tokens":{"input_tokens":` + int64String(tokens.InputTokens) + `,"output_tokens":` + int64String(tokens.OutputTokens) + `,"reasoning_tokens":` + int64String(tokens.ReasoningTokens) + `,"cached_tokens":` + int64String(tokens.CachedTokens) + `,"total_tokens":` + int64String(tokens.TotalTokens) + `}}`
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
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

func openSyncTestDatabaseWithLogs(t *testing.T) (*gorm.DB, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	gormLogger := gormlogger.New(
		log.New(logs, "", 0),
		gormlogger.Config{
			LogLevel:                  gormlogger.Info,
			IgnoreRecordNotFoundError: false,
			Colorful:                  false,
		},
	)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sync.db")), &gorm.Config{Logger: gormLogger})
	if err != nil {
		t.Fatalf("gorm.Open returned error: %v", err)
	}
	closeTestDatabase(t, db)
	if err := db.AutoMigrate(entities.All()...); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}
	return db, logs
}
