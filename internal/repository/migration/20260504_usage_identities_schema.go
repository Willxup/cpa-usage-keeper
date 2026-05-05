package migration

import (
	"fmt"

	"gorm.io/gorm"
)

func createUsageIdentitiesMigration(tx *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS usage_identities (
			id integer PRIMARY KEY AUTOINCREMENT,
			name text,
			auth_type integer,
			auth_type_name text,
			identity text,
			type text,
			provider text,
			lookup_key text,
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
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_identities_type_identity ON usage_identities(auth_type, identity)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_auth_type ON usage_identities(auth_type)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_auth_type_name ON usage_identities(auth_type_name)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_identity ON usage_identities(identity)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_is_deleted ON usage_identities(is_deleted)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_last_aggregated_usage_event_id ON usage_identities(last_aggregated_usage_event_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_deleted_at ON usage_identities(deleted_at)`,
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("create usage_identities schema: %w", err)
		}
	}
	return nil
}

func normalizeUsageIdentitiesSchemaMigration(tx *gorm.DB) error {
	statements := []string{
		`DROP INDEX IF EXISTS uniq_usage_identities_type_identity`,
		`DROP INDEX IF EXISTS idx_usage_identities_auth_type`,
		`DROP INDEX IF EXISTS idx_usage_identities_auth_type_name`,
		`DROP INDEX IF EXISTS idx_usage_identities_identity`,
		`DROP INDEX IF EXISTS idx_usage_identities_is_deleted`,
		`DROP INDEX IF EXISTS idx_usage_identities_last_aggregated_usage_event_id`,
		`DROP INDEX IF EXISTS idx_usage_identities_deleted_at`,
		`CREATE TABLE usage_identities_normalized (
			id integer PRIMARY KEY AUTOINCREMENT,
			name text,
			auth_type integer,
			auth_type_name text,
			identity text,
			type text,
			provider text,
			lookup_key text,
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
		)`,
		`INSERT INTO usage_identities_normalized (id, name, auth_type, auth_type_name, identity, type, provider, lookup_key, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at, stats_updated_at, is_deleted, created_at, updated_at, deleted_at)
		SELECT id, name, auth_type, auth_type_name, identity, type, provider, lookup_key, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at, stats_updated_at, is_deleted, created_at, updated_at, deleted_at
		FROM usage_identities`,
		`DROP TABLE usage_identities`,
		`ALTER TABLE usage_identities_normalized RENAME TO usage_identities`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_identities_type_identity ON usage_identities(auth_type, identity)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_auth_type ON usage_identities(auth_type)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_auth_type_name ON usage_identities(auth_type_name)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_identity ON usage_identities(identity)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_is_deleted ON usage_identities(is_deleted)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_last_aggregated_usage_event_id ON usage_identities(last_aggregated_usage_event_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_identities_deleted_at ON usage_identities(deleted_at)`,
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("normalize usage_identities schema: %w", err)
		}
	}
	return nil
}
