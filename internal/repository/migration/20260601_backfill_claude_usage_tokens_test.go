package migration

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBackfillClaudeUsageTokensMigrationNormalizesEventsAndAggregates(t *testing.T) {
	db := openClaudeUsageTokenBackfillTestDatabase(t)
	seedClaudeUsageTokenBackfillRows(t, db)

	if err := backfillClaudeUsageTokensMigration(db); err != nil {
		t.Fatalf("backfill Claude usage tokens: %v", err)
	}
	assertClaudeUsageTokenBackfillRows(t, db)

	// 迁移版本正常只执行一次；函数本身也保持幂等，避免手动重跑或测试复用时重复加 token。
	if err := backfillClaudeUsageTokensMigration(db); err != nil {
		t.Fatalf("backfill Claude usage tokens should be idempotent: %v", err)
	}
	assertClaudeUsageTokenBackfillRows(t, db)
}

func openClaudeUsageTokenBackfillTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "claude-token-backfill.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() {
		closeOpenedDatabase(t, db)
	})

	statements := []string{
		`CREATE TABLE usage_events (
			id integer PRIMARY KEY,
			event_key text,
			api_group_key text,
			provider text,
			auth_type text,
			auth_index text,
			model text,
			model_alias text,
			timestamp datetime,
			input_tokens integer,
			output_tokens integer,
			reasoning_tokens integer,
			cached_tokens integer,
			cache_read_tokens integer NOT NULL DEFAULT 0,
			cache_creation_tokens integer NOT NULL DEFAULT 0,
			total_tokens integer,
			created_at datetime
		)`,
		`CREATE TABLE usage_identities (
			id integer PRIMARY KEY,
			auth_type integer,
			identity text,
			type text,
			input_tokens integer,
			output_tokens integer,
			reasoning_tokens integer,
			cached_tokens integer,
			total_tokens integer,
			last_aggregated_usage_event_id integer NOT NULL DEFAULT 0,
			is_deleted numeric
		)`,
		`CREATE TABLE usage_overview_hourly_stats (
			id integer PRIMARY KEY,
			bucket_start datetime,
			api_group_key text,
			model text,
			auth_index text,
			model_alias text NOT NULL DEFAULT '',
			input_tokens integer NOT NULL DEFAULT 0,
			output_tokens integer NOT NULL DEFAULT 0,
			reasoning_tokens integer NOT NULL DEFAULT 0,
			cached_tokens integer NOT NULL DEFAULT 0,
			cache_read_tokens integer NOT NULL DEFAULT 0,
			cache_creation_tokens integer NOT NULL DEFAULT 0,
			total_tokens integer NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE usage_overview_daily_stats (
			id integer PRIMARY KEY,
			bucket_start datetime,
			api_group_key text,
			model text,
			auth_index text,
			model_alias text NOT NULL DEFAULT '',
			input_tokens integer NOT NULL DEFAULT 0,
			output_tokens integer NOT NULL DEFAULT 0,
			reasoning_tokens integer NOT NULL DEFAULT 0,
			cached_tokens integer NOT NULL DEFAULT 0,
			cache_read_tokens integer NOT NULL DEFAULT 0,
			cache_creation_tokens integer NOT NULL DEFAULT 0,
			total_tokens integer NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE usage_overview_aggregation_checkpoints (
			id integer PRIMARY KEY,
			name text NOT NULL,
			last_aggregated_usage_event_id integer NOT NULL DEFAULT 0,
			stats_updated_at datetime,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test schema: %v", err)
		}
	}
	return db
}

func seedClaudeUsageTokenBackfillRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO usage_identities (id, auth_type, identity, type, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, last_aggregated_usage_event_id, is_deleted) VALUES
			(1, 2, 'claude-auth', 'claude', 330, 90, 5, 80, 325, 3, 0),
			(2, 1, 'oauth-auth', 'account', 200, 50, 0, 15, 265, 4, 0),
			(3, 2, 'openai-auth', 'openai', 100, 30, 0, 30, 160, 5, 0)`,
		`INSERT INTO usage_events (id, event_key, api_group_key, provider, auth_type, auth_index, model, model_alias, timestamp, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, created_at) VALUES
			(1, 'apikey-old-total', 'api-key', 'Team Provider', 'apikey', 'claude-auth', 'claude-sonnet', 'claude-sonnet', '2026-06-01T10:15:30+08:00', 100, 30, 5, 30, 20, 10, 165, '2026-06-01T10:15:31+08:00'),
			(2, 'apikey-already-normalized', 'api-key', 'Team Provider', 'apikey', 'claude-auth', 'claude-sonnet', 'claude-sonnet', '2026-06-01T10:16:30+08:00', 130, 30, 0, 20, 20, 10, 160, '2026-06-01T10:16:31+08:00'),
			(3, 'apikey-zero-total', 'api-key', 'Team Provider', 'apikey', 'claude-auth', 'claude-sonnet', 'claude-sonnet', '2026-06-01T10:17:30+08:00', 100, 30, 0, 30, 20, 10, 0, '2026-06-01T10:17:31+08:00'),
			(4, 'oauth-claude', 'oauth-key', 'claude', 'oauth', 'oauth-auth', 'claude-opus', '', '2026-06-01T11:10:30+08:00', 200, 50, 0, 15, 15, 0, 265, '2026-06-01T11:10:31+08:00'),
			(5, 'openai-untouched', 'api-key', 'OpenAI', 'apikey', 'openai-auth', 'gpt-5', '', '2026-06-01T10:30:30+08:00', 100, 30, 0, 30, 20, 10, 160, '2026-06-01T10:30:31+08:00'),
			(6, 'apikey-zero-total-read-and-creation', 'api-key', 'Team Provider', 'apikey', 'claude-auth', 'claude-sonnet', 'claude-sonnet', '2026-06-01T10:18:30+08:00', 100, 30, 0, 20, 20, 10, 0, '2026-06-01T10:18:31+08:00'),
			(7, 'apikey-zero-total-read-only', 'api-key', 'Team Provider', 'apikey', 'claude-auth', 'claude-sonnet', 'claude-sonnet', '2026-06-01T10:19:30+08:00', 70, 20, 0, 15, 15, 0, 0, '2026-06-01T10:19:31+08:00'),
			(8, 'apikey-zero-total-creation-only', 'api-key', 'Team Provider', 'apikey', 'claude-auth', 'claude-sonnet', 'claude-sonnet', '2026-06-01T10:20:30+08:00', 80, 20, 0, 0, 0, 10, 0, '2026-06-01T10:20:31+08:00')`,
		`INSERT INTO usage_overview_hourly_stats (id, bucket_start, api_group_key, model, auth_index, model_alias, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens) VALUES
			(1, '2026-06-01T10:00:00+08:00', 'api-key', 'claude-sonnet', 'claude-auth', 'claude-sonnet', 330, 90, 5, 80, 60, 30, 325),
			(2, '2026-06-01T11:00:00+08:00', 'oauth-key', 'claude-opus', 'oauth-auth', '', 200, 50, 0, 15, 15, 0, 265),
			(3, '2026-06-01T10:00:00+08:00', 'api-key', 'gpt-5', 'openai-auth', '', 100, 30, 0, 30, 20, 10, 160)`,
		`INSERT INTO usage_overview_daily_stats (id, bucket_start, api_group_key, model, auth_index, model_alias, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens) VALUES
			(1, '2026-06-01T00:00:00+08:00', 'api-key', 'claude-sonnet', 'claude-auth', 'claude-sonnet', 330, 90, 5, 80, 60, 30, 325),
			(2, '2026-06-01T00:00:00+08:00', 'oauth-key', 'claude-opus', 'oauth-auth', '', 200, 50, 0, 15, 15, 0, 265),
			(3, '2026-06-01T00:00:00+08:00', 'api-key', 'gpt-5', 'openai-auth', '', 100, 30, 0, 30, 20, 10, 160)`,
		`INSERT INTO usage_overview_aggregation_checkpoints (id, name, last_aggregated_usage_event_id, stats_updated_at, created_at, updated_at) VALUES
			(1, 'overview', 5, '2026-06-01T12:00:00+08:00', '2026-06-01T12:00:00+08:00', '2026-06-01T12:00:00+08:00')`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed backfill rows: %v", err)
		}
	}
}

func assertClaudeUsageTokenBackfillRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	assertEventTokens(t, db, "apikey-old-total", 130, 30, 5, 20, 20, 10, 165)
	assertEventTokens(t, db, "apikey-already-normalized", 130, 30, 0, 20, 20, 10, 160)
	assertEventTokens(t, db, "apikey-zero-total", 130, 30, 0, 20, 20, 10, 160)
	assertEventTokens(t, db, "oauth-claude", 215, 50, 0, 15, 15, 0, 265)
	assertEventTokens(t, db, "openai-untouched", 100, 30, 0, 30, 20, 10, 160)
	assertEventTokens(t, db, "apikey-zero-total-read-and-creation", 130, 30, 0, 20, 20, 10, 160)
	assertEventTokens(t, db, "apikey-zero-total-read-only", 85, 20, 0, 15, 15, 0, 105)
	assertEventTokens(t, db, "apikey-zero-total-creation-only", 90, 20, 0, 0, 0, 10, 110)

	assertAggregateTokens(t, db, "usage_overview_hourly_stats", 1, 390, 90, 5, 60, 60, 30, 485)
	assertAggregateTokens(t, db, "usage_overview_hourly_stats", 2, 215, 50, 0, 15, 15, 0, 265)
	assertAggregateTokens(t, db, "usage_overview_hourly_stats", 3, 100, 30, 0, 30, 20, 10, 160)
	assertAggregateTokens(t, db, "usage_overview_daily_stats", 1, 390, 90, 5, 60, 60, 30, 485)
	assertAggregateTokens(t, db, "usage_overview_daily_stats", 2, 215, 50, 0, 15, 15, 0, 265)
	assertAggregateTokens(t, db, "usage_overview_daily_stats", 3, 100, 30, 0, 30, 20, 10, 160)

	assertIdentityTokens(t, db, 1, 390, 90, 5, 60, 485)
	assertIdentityTokens(t, db, 2, 215, 50, 0, 15, 265)
	assertIdentityTokens(t, db, 3, 100, 30, 0, 30, 160)
}

func assertEventTokens(t *testing.T, db *gorm.DB, eventKey string, input, output, reasoning, cached, cacheRead, cacheCreation, total int64) {
	t.Helper()
	var row struct {
		InputTokens         int64
		OutputTokens        int64
		ReasoningTokens     int64
		CachedTokens        int64
		CacheReadTokens     int64
		CacheCreationTokens int64
		TotalTokens         int64
	}
	if err := db.Raw(`SELECT input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens FROM usage_events WHERE event_key = ?`, eventKey).Scan(&row).Error; err != nil {
		t.Fatalf("load usage event %s: %v", eventKey, err)
	}
	if row.InputTokens != input || row.OutputTokens != output || row.ReasoningTokens != reasoning || row.CachedTokens != cached || row.CacheReadTokens != cacheRead || row.CacheCreationTokens != cacheCreation || row.TotalTokens != total {
		t.Fatalf("unexpected event %s tokens: %+v", eventKey, row)
	}
}

func assertAggregateTokens(t *testing.T, db *gorm.DB, table string, id, input, output, reasoning, cached, cacheRead, cacheCreation, total int64) {
	t.Helper()
	var row struct {
		InputTokens         int64
		OutputTokens        int64
		ReasoningTokens     int64
		CachedTokens        int64
		CacheReadTokens     int64
		CacheCreationTokens int64
		TotalTokens         int64
	}
	if err := db.Raw(`SELECT input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens FROM `+table+` WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("load aggregate %s/%d: %v", table, id, err)
	}
	if row.InputTokens != input || row.OutputTokens != output || row.ReasoningTokens != reasoning || row.CachedTokens != cached || row.CacheReadTokens != cacheRead || row.CacheCreationTokens != cacheCreation || row.TotalTokens != total {
		t.Fatalf("unexpected aggregate %s/%d tokens: %+v", table, id, row)
	}
}

func assertIdentityTokens(t *testing.T, db *gorm.DB, id, input, output, reasoning, cached, total int64) {
	t.Helper()
	var row struct {
		InputTokens     int64
		OutputTokens    int64
		ReasoningTokens int64
		CachedTokens    int64
		TotalTokens     int64
	}
	if err := db.Raw(`SELECT input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens FROM usage_identities WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("load identity %d: %v", id, err)
	}
	if row.InputTokens != input || row.OutputTokens != output || row.ReasoningTokens != reasoning || row.CachedTokens != cached || row.TotalTokens != total {
		t.Fatalf("unexpected identity %d tokens: %+v", id, row)
	}
}
