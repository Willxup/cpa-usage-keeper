package migration

import (
	"database/sql"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemoveUsageIdentityQuotaFieldsMigrationDropsUnusedColumnsAndPreservesRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_identities (
		id integer PRIMARY KEY AUTOINCREMENT,
		name text,
		auth_type integer,
		auth_type_name text,
		identity text,
		type text,
		provider text,
		lookup_key text,
		prefix text,
		account_id text,
		project_id text,
		active_start datetime,
		active_until datetime,
		plan_type text,
		limit_reached numeric,
		primary_window_used_percent integer,
		primary_window_limit_seconds integer,
		primary_window_reset_seconds integer,
		primary_window_reset_at datetime,
		secondary_window_used_percent integer,
		secondary_window_limit_seconds integer,
		secondary_window_reset_seconds integer,
		secondary_window_reset_at datetime,
		total_requests integer,
		success_count integer,
		failure_count integer,
		input_tokens integer,
		output_tokens integer,
		reasoning_tokens integer,
		cached_tokens integer,
		total_tokens integer,
		last_aggregated_usage_event_id integer,
		first_used_at datetime,
		last_used_at datetime,
		stats_updated_at datetime,
		is_deleted numeric,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_identities table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_identities (
		name, auth_type, auth_type_name, identity, type, provider, lookup_key,
		prefix, account_id, project_id, active_start, active_until, plan_type,
		limit_reached, primary_window_used_percent, primary_window_limit_seconds, primary_window_reset_seconds, primary_window_reset_at,
		secondary_window_used_percent, secondary_window_limit_seconds, secondary_window_reset_seconds, secondary_window_reset_at,
		total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens,
		last_aggregated_usage_event_id, first_used_at, last_used_at, stats_updated_at, is_deleted, created_at, updated_at, deleted_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Codex Account", 1, "oauth", "codex-auth", "codex", "Codex", "lookup-key",
		"codex-prefix", "acct_123", "project_123", "2026-05-01T00:00:00Z", "2026-06-01T00:00:00Z", "team",
		true, 80, 18000, 3600, "2026-05-07T12:00:00Z",
		20, 604800, 86400, "2026-05-08T12:00:00Z",
		10, 8, 2, 100, 200, 30, 40, 370,
		99, "2026-05-04T08:00:00Z", "2026-05-04T09:00:00Z", "2026-05-04T10:00:00Z", false, "2026-05-03T08:00:00Z", "2026-05-04T10:30:00Z", nil,
	).Error; err != nil {
		t.Fatalf("seed legacy usage identity: %v", err)
	}

	if err := removeUsageIdentityQuotaFieldsMigration(db); err != nil {
		t.Fatalf("remove usage identity quota fields: %v", err)
	}
	if err := removeUsageIdentityQuotaFieldsMigration(db); err != nil {
		t.Fatalf("remove usage identity quota fields idempotently: %v", err)
	}

	removedColumns := []string{
		"limit_reached",
		"primary_window_used_percent",
		"primary_window_limit_seconds",
		"primary_window_reset_seconds",
		"primary_window_reset_at",
		"secondary_window_used_percent",
		"secondary_window_limit_seconds",
		"secondary_window_reset_seconds",
		"secondary_window_reset_at",
	}
	for _, column := range removedColumns {
		if db.Migrator().HasColumn("usage_identities", column) {
			t.Fatalf("expected usage_identities.%s column to be removed", column)
		}
	}

	preservedColumns := []string{
		"prefix",
		"account_id",
		"project_id",
		"active_start",
		"active_until",
		"plan_type",
		"total_requests",
		"success_count",
		"failure_count",
		"is_deleted",
	}
	for _, column := range preservedColumns {
		if !db.Migrator().HasColumn("usage_identities", column) {
			t.Fatalf("expected usage_identities.%s column to be preserved", column)
		}
	}

	var row struct {
		Prefix        sql.NullString
		AccountID     sql.NullString
		ProjectID     sql.NullString
		PlanType      sql.NullString
		TotalRequests int64
		SuccessCount  int64
		FailureCount  int64
	}
	if err := db.Raw(`SELECT prefix, account_id, project_id, plan_type, total_requests, success_count, failure_count FROM usage_identities WHERE identity = ?`, "codex-auth").Scan(&row).Error; err != nil {
		t.Fatalf("load preserved usage identity row: %v", err)
	}
	if row.Prefix.String != "codex-prefix" || row.AccountID.String != "acct_123" || row.ProjectID.String != "project_123" || row.PlanType.String != "team" {
		t.Fatalf("expected metadata to be preserved, got %+v", row)
	}
	if row.TotalRequests != 10 || row.SuccessCount != 8 || row.FailureCount != 2 {
		t.Fatalf("expected stats to be preserved, got %+v", row)
	}
}
