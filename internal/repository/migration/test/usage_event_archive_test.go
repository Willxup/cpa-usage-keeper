package test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const usageEventArchiveMigrationVersion = "20260730_create_usage_event_archive"

func TestUsageEventArchiveMigrationCreatesColdTableOnExistingDatabase(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	// 热表故意使用旧物理 schema；归档迁移不能依赖未来事件实体或提前创建费用列。
	if err := db.Exec("CREATE TABLE usage_events (id integer PRIMARY KEY, event_key text NOT NULL)").Error; err != nil {
		t.Fatalf("create existing usage_events: %v", err)
	}
	if err := db.Exec("INSERT INTO usage_events (id, event_key) VALUES (1, 'legacy-event')").Error; err != nil {
		t.Fatalf("seed existing usage event: %v", err)
	}
	runOnlyMigration(t, db, usageEventArchiveMigrationVersion)

	if !db.Migrator().HasTable("usage_events_archive") {
		t.Fatal("expected archive migration to create usage_events_archive")
	}
	assertNoFuturePricingColumns(t, db, "usage_events_archive", "cost_usd", "cost_available")
	for _, column := range []string{"event_key", "session_id", "response_model", "status_code", "stream", "client_ip"} {
		if !db.Migrator().HasColumn("usage_events_archive", column) {
			t.Fatalf("published archive schema is missing %s", column)
		}
	}
	var eventKey string
	if err := db.Table("usage_events").Select("event_key").Where("id = 1").Scan(&eventKey).Error; err != nil || eventKey != "legacy-event" {
		t.Fatalf("archive schema migration changed old hot row: key=%q err=%v", eventKey, err)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageEventArchiveMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count archive migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected archive migration recorded once, got %d", count)
	}
}

func TestUsageEventReplayPagesMergeArchiveAndHotByGlobalID(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	createPhysicalLegacyUsageReplayTables(t, db)
	now := time.Date(2026, 7, 30, 4, 30, 0, 0, time.Local)
	for _, row := range []struct {
		table, eventKey, authType, authIndex, source string
		id, totalTokens                              int64
		timestamp                                    time.Time
	}{
		{"usage_events_archive", "archive-1", "oauth", "auth-file-1", "archive-source", 1, 1, now.AddDate(0, 0, -120)},
		{"usage_events", "hot-2", "apikey", "provider-2", "hot-source", 2, 2, now.AddDate(0, 0, -10)},
		{"usage_events", "hot-3", "", "", "", 3, 3, now.AddDate(0, 0, -9)},
		{"usage_events_archive", "late-archive-4", "", "", "", 4, 4, now.AddDate(0, 0, -100)},
		{"usage_events", "hot-5", "", "", "", 5, 5, now},
	} {
		if err := db.Exec(fmt.Sprintf("INSERT INTO %s (id, event_key, auth_type, auth_index, source, timestamp, total_tokens) VALUES (?, ?, ?, ?, ?, ?, ?)", row.table),
			row.id, row.eventKey, row.authType, row.authIndex, row.source, timeutil.FormatStorageTime(row.timestamp), row.totalTokens).Error; err != nil {
			t.Fatalf("seed physical old %s replay row %d: %v", row.table, row.id, err)
		}
	}

	targetID, err := migration.LoadUsageAggregationReplayTargetEventID(db)
	if err != nil {
		t.Fatalf("load replay target: %v", err)
	}
	if targetID != 5 {
		t.Fatalf("expected replay target 5, got %d", targetID)
	}
	var gotIDs []int64
	gotEvents := make(map[int64]entities.UsageEvent)
	afterID := int64(0)
	for {
		page, err := migration.LoadUsageAggregationReplayEventPage(db, afterID, targetID, 2)
		if err != nil {
			t.Fatalf("load replay page after %d: %v", afterID, err)
		}
		if len(page) == 0 {
			break
		}
		for _, event := range page {
			gotIDs = append(gotIDs, event.ID)
			gotEvents[event.ID] = event
		}
		afterID = page[len(page)-1].ID
	}
	if !slices.Equal(gotIDs, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("expected globally ordered replay IDs, got %v", gotIDs)
	}
	if event := gotEvents[1]; event.AuthType != "oauth" || event.AuthIndex != "auth-file-1" || event.Source != "archive-source" {
		t.Fatalf("expected archive replay to preserve identity fields, got %+v", event)
	}
	if event := gotEvents[2]; event.AuthType != "apikey" || event.AuthIndex != "provider-2" || event.Source != "hot-source" {
		t.Fatalf("expected hot replay to preserve identity fields, got %+v", event)
	}

	// 回放查询应沿两张表的主键合并有序分页。
	var rows []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := db.Raw(`EXPLAIN QUERY PLAN
		SELECT id FROM (
			SELECT id FROM usage_events_archive WHERE id > ? AND id <= ?
			UNION ALL
			SELECT id FROM usage_events WHERE id > ? AND id <= ?
		) ORDER BY id ASC LIMIT ?`, 0, 100, 0, 100, 10).Scan(&rows).Error; err != nil {
		t.Fatalf("explain replay query: %v", err)
	}
	details := make([]string, 0, len(rows))
	for _, row := range rows {
		details = append(details, row.Detail)
	}
	plan := strings.Join(details, "\n")
	for _, want := range []string{"MERGE (UNION ALL)", "usage_events_archive USING INTEGER PRIMARY KEY", "usage_events USING INTEGER PRIMARY KEY"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("expected replay query plan to contain %q, got:\n%s", want, plan)
		}
	}
}

// 两张表只含费用持久化前的已发布列，让回放查询真实面对缺少费用列的 SQLite schema。
func createPhysicalLegacyUsageReplayTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	const columns = `
		id INTEGER PRIMARY KEY, event_key TEXT, api_group_key TEXT, provider TEXT, endpoint TEXT,
		auth_type TEXT, request_id TEXT, session_id TEXT, parent_session_id TEXT,
		client_ip TEXT, x_forwarded_for TEXT, user_agent TEXT, model TEXT, model_alias TEXT,
		response_model TEXT, reasoning_effort TEXT, service_tier TEXT, response_service_tier TEXT,
		executor_type TEXT, timestamp DATETIME, source TEXT, auth_index TEXT, failed NUMERIC,
		status_code INTEGER, generate NUMERIC, stream NUMERIC, latency_ms INTEGER, ttft_ms INTEGER,
		input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cached_tokens INTEGER,
		cache_read_tokens INTEGER, cache_creation_tokens INTEGER, total_tokens INTEGER, created_at DATETIME`
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Exec("CREATE TABLE " + table + " (" + columns + ")").Error; err != nil {
			t.Fatalf("create physical old %s replay schema: %v", table, err)
		}
		assertNoFuturePricingColumns(t, db, table, "cost_usd", "cost_available")
	}
}
