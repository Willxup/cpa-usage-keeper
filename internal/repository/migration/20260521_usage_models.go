package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// createUsageModelsMigration creates the compact pricing model index and backfills historical usage_events.
func createUsageModelsMigration(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&entities.UsageModel{}, &entities.UsageOverviewAggregationCheckpoint{}); err != nil {
		return fmt.Errorf("auto migrate usage models: %w", err)
	}
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_models_model_auth_type_auth_index ON usage_models (model, auth_type, auth_index)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_models_model ON usage_models (model)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_models_auth_type_auth_index ON usage_models (auth_type, auth_index)`,
	} {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create usage models index: %w", err)
		}
	}
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	now := timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(time.Now()))
	if err := tx.Exec(`
		INSERT OR IGNORE INTO usage_models (
			model,
			auth_type,
			auth_index,
			request_count,
			first_used_at,
			last_used_at,
			last_usage_event_id,
			created_at,
			updated_at
		)
		SELECT
			TRIM(COALESCE(model, '')) AS model,
			TRIM(COALESCE(auth_type, '')) AS auth_type,
			TRIM(COALESCE(auth_index, '')) AS auth_index,
			COUNT(*) AS request_count,
			MIN(timestamp) AS first_used_at,
			MAX(timestamp) AS last_used_at,
			COALESCE(MAX(id), 0) AS last_usage_event_id,
			?,
			?
		FROM usage_events
		WHERE TRIM(COALESCE(model, '')) <> ''
		GROUP BY TRIM(COALESCE(model, '')), TRIM(COALESCE(auth_type, '')), TRIM(COALESCE(auth_index, ''))
	`, now, now).Error; err != nil {
		return fmt.Errorf("backfill usage models: %w", err)
	}
	if err := tx.Exec(`
		INSERT OR IGNORE INTO usage_overview_aggregation_checkpoints (
			name,
			last_aggregated_usage_event_id,
			stats_updated_at,
			created_at,
			updated_at
		)
		SELECT
			?,
			COALESCE(MAX(id), 0),
			?,
			?,
			?
		FROM usage_events
	`, "usage_models", now, now, now).Error; err != nil {
		return fmt.Errorf("create usage model aggregation checkpoint: %w", err)
	}
	return nil
}
