package repository

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	migrationAddUsageEventRedisFields         = "20260503_add_usage_event_redis_fields"
	migrationBackfillUsageEventRedisFields    = "20260503_backfill_usage_event_redis_fields"
	migrationDropSnapshotRuns                 = "20260503_drop_snapshot_runs"
	migrationDropLegacySnapshotRunColumns     = "20260504_drop_legacy_snapshot_run_columns"
	migrationCreateUsageIdentities            = "20260504_create_usage_identities"
	migrationMigrateUsageIdentitiesMetadata   = "20260504_migrate_usage_identities_metadata"
	migrationBackfillUsageEventIdentityFields = "20260504_backfill_usage_event_identity_fields"
	migrationBackfillUsageIdentityStats       = "20260504_backfill_usage_identity_stats"
	migrationDropLegacyMetadataTables         = "20260504_drop_legacy_metadata_tables"
)

type schemaMigration struct {
	Version   string    `gorm:"primaryKey;column:version"`
	AppliedAt time.Time `gorm:"not null;column:applied_at"`
}

func (schemaMigration) TableName() string {
	return "schema_migrations"
}

type databaseMigration struct {
	version string
	run     func(*gorm.DB) error
}

func runSchemaMigrations(db *gorm.DB) error {
	if err := db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	migrations := []databaseMigration{
		{version: migrationAddUsageEventRedisFields, run: addUsageEventRedisFieldsMigration},
		{version: migrationBackfillUsageEventRedisFields, run: backfillUsageEventRedisFieldsMigration},
		{version: migrationDropSnapshotRuns, run: dropSnapshotRunsMigration},
		{version: migrationDropLegacySnapshotRunColumns, run: dropLegacySnapshotRunColumnsMigration},
		{version: migrationCreateUsageIdentities, run: createUsageIdentitiesMigration},
		{version: migrationMigrateUsageIdentitiesMetadata, run: migrateUsageIdentitiesMetadataMigration},
		{version: migrationBackfillUsageEventIdentityFields, run: backfillUsageEventIdentityFieldsMigration},
		{version: migrationBackfillUsageIdentityStats, run: backfillUsageIdentityStatsMigration},
		{version: migrationDropLegacyMetadataTables, run: dropLegacyMetadataTablesMigration},
	}
	for _, migration := range migrations {
		if err := runSchemaMigration(db, migration); err != nil {
			return err
		}
	}
	return nil
}

func runSchemaMigration(db *gorm.DB, migration databaseMigration) error {
	return db.Transaction(func(tx *gorm.DB) error {
		logger := logrus.WithField("version", migration.version)
		var count int64
		if err := tx.Table("schema_migrations").Where("version = ?", migration.version).Count(&count).Error; err != nil {
			logger.WithError(err).Error("schema migration failed")
			return fmt.Errorf("check schema migration %s: %w", migration.version, err)
		}
		if count > 0 {
			logger.Info("schema migration skipped")
			return nil
		}
		logger.Info("schema migration started")
		if err := migration.run(tx); err != nil {
			logger.WithError(err).Error("schema migration failed")
			return fmt.Errorf("run schema migration %s: %w", migration.version, err)
		}
		if err := tx.Create(&schemaMigration{Version: migration.version, AppliedAt: time.Now().UTC()}).Error; err != nil {
			logger.WithError(err).Error("schema migration failed")
			return fmt.Errorf("record schema migration %s: %w", migration.version, err)
		}
		logger.Info("schema migration applied")
		return nil
	})
}

func addUsageEventRedisFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageEvent{}) {
		return nil
	}
	columns := []struct {
		name string
		sql  string
	}{
		{name: "provider", sql: "ALTER TABLE usage_events ADD COLUMN provider TEXT"},
		{name: "endpoint", sql: "ALTER TABLE usage_events ADD COLUMN endpoint TEXT"},
		{name: "auth_type", sql: "ALTER TABLE usage_events ADD COLUMN auth_type TEXT"},
		{name: "request_id", sql: "ALTER TABLE usage_events ADD COLUMN request_id TEXT"},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(&models.UsageEvent{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add usage_events.%s column: %w", column.name, err)
		}
	}
	return nil
}

type redisUsageBackfillPayload struct {
	Provider  string `json:"provider"`
	Endpoint  string `json:"endpoint"`
	AuthType  string `json:"auth_type"`
	RequestID string `json:"request_id"`
}

func backfillUsageEventRedisFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageEvent{}) || !tx.Migrator().HasTable(&models.RedisUsageInbox{}) {
		return nil
	}
	for _, column := range []string{"provider", "endpoint", "auth_type", "request_id"} {
		if !tx.Migrator().HasColumn(&models.UsageEvent{}, column) {
			return nil
		}
	}

	var inboxRows []models.RedisUsageInbox
	return tx.Where("status = ?", RedisUsageInboxStatusProcessed).
		Order("id asc").
		FindInBatches(&inboxRows, 500, func(_ *gorm.DB, _ int) error {
			for _, inbox := range inboxRows {
				var payload redisUsageBackfillPayload
				if err := json.Unmarshal([]byte(inbox.RawMessage), &payload); err != nil {
					continue
				}
				payload.Provider = strings.TrimSpace(payload.Provider)
				payload.Endpoint = strings.TrimSpace(payload.Endpoint)
				payload.AuthType = normalizeUsageEventRedisAuthType(payload.AuthType)
				payload.RequestID = strings.TrimSpace(payload.RequestID)
				if payload.Provider == "" && payload.Endpoint == "" && payload.AuthType == "" && payload.RequestID == "" {
					continue
				}
				if err := backfillUsageEventRedisFields(tx, strings.TrimSpace(inbox.UsageEventKey), payload, true); err != nil {
					return err
				}
			}
			return nil
		}).Error
}

func backfillUsageEventRedisFields(tx *gorm.DB, usageEventKey string, payload redisUsageBackfillPayload, allowRequestIDFallback bool) error {
	if usageEventKey == "" {
		if allowRequestIDFallback && payload.RequestID != "" {
			return backfillUsageEventRedisFields(tx, payload.RequestID, payload, false)
		}
		return nil
	}

	var event models.UsageEvent
	result := tx.Where("event_key = ?", usageEventKey).Limit(1).Find(&event)
	if result.Error != nil {
		return fmt.Errorf("load usage event %q for redis backfill: %w", usageEventKey, result.Error)
	}
	if result.RowsAffected == 0 {
		if allowRequestIDFallback && payload.RequestID != "" && payload.RequestID != usageEventKey {
			return backfillUsageEventRedisFields(tx, payload.RequestID, payload, false)
		}
		return nil
	}

	updates := map[string]any{}
	if strings.TrimSpace(event.Provider) == "" && payload.Provider != "" {
		updates["provider"] = payload.Provider
	}
	if strings.TrimSpace(event.Endpoint) == "" && payload.Endpoint != "" {
		updates["endpoint"] = payload.Endpoint
	}
	if strings.TrimSpace(event.AuthType) == "" && payload.AuthType != "" {
		updates["auth_type"] = payload.AuthType
	}
	if strings.TrimSpace(event.RequestID) == "" && payload.RequestID != "" {
		updates["request_id"] = payload.RequestID
	}
	if len(updates) == 0 {
		return nil
	}
	if err := tx.Model(&models.UsageEvent{}).Where("id = ?", event.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("backfill usage event %q redis fields: %w", event.EventKey, err)
	}
	return nil
}

func normalizeUsageEventRedisAuthType(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "api_key" {
		return "apikey"
	}
	return trimmed
}

func dropSnapshotRunsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("snapshot_runs") {
		return nil
	}
	if err := tx.Exec("DROP TABLE IF EXISTS snapshot_runs").Error; err != nil {
		return fmt.Errorf("drop snapshot_runs table: %w", err)
	}
	return nil
}

func dropLegacySnapshotRunColumnsMigration(tx *gorm.DB) error {
	for _, indexName := range []string{"idx_usage_events_snapshot_run_id", "idx_redis_usage_inboxes_snapshot_run_id"} {
		if err := tx.Exec("DROP INDEX IF EXISTS " + indexName).Error; err != nil {
			return fmt.Errorf("drop legacy snapshot_run_id index %s: %w", indexName, err)
		}
	}
	if err := dropColumnIfExists(tx, &models.UsageEvent{}, "snapshot_run_id", "usage_events"); err != nil {
		return err
	}
	if err := dropColumnIfExists(tx, &models.RedisUsageInbox{}, "snapshot_run_id", "redis_usage_inboxes"); err != nil {
		return err
	}
	return nil
}

func dropColumnIfExists(tx *gorm.DB, model any, columnName string, tableName string) error {
	if !tx.Migrator().HasTable(model) || !tx.Migrator().HasColumn(model, columnName) {
		return nil
	}
	if err := tx.Exec("ALTER TABLE " + tableName + " DROP COLUMN " + columnName).Error; err != nil {
		return fmt.Errorf("drop %s.%s column: %w", tableName, columnName, err)
	}
	return nil
}

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
			total_requests integer DEFAULT 0,
			success_count integer DEFAULT 0,
			failure_count integer DEFAULT 0,
			input_tokens integer DEFAULT 0,
			output_tokens integer DEFAULT 0,
			reasoning_tokens integer DEFAULT 0,
			cached_tokens integer DEFAULT 0,
			total_tokens integer DEFAULT 0,
			last_aggregated_usage_event_id integer DEFAULT 0,
			first_used_at datetime,
			last_used_at datetime,
			stats_updated_at datetime,
			is_deleted numeric DEFAULT false,
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

func migrateUsageIdentitiesMetadataMigration(tx *gorm.DB) error {
	now := time.Now().UTC()
	if tx.Migrator().HasTable("auth_files") {
		isDeletedSelect, deletedAtSelect := legacyDeletedStateSelect(tx, "auth_files")
		if err := tx.Exec(`
			INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, is_deleted, created_at, updated_at, deleted_at)
			SELECT COALESCE(NULLIF(TRIM(email), ''), NULLIF(TRIM(label), ''), NULLIF(TRIM(name), ''), auth_index),
				?, ?, auth_index, type, provider, `+isDeletedSelect+`, COALESCE(created_at, ?), ?, `+deletedAtSelect+`
			FROM auth_files
			WHERE auth_index IS NOT NULL AND TRIM(auth_index) != ''
			ON CONFLICT(auth_type, identity) DO UPDATE SET
				name = excluded.name,
				auth_type_name = excluded.auth_type_name,
				type = excluded.type,
				provider = excluded.provider,
				is_deleted = excluded.is_deleted,
				deleted_at = excluded.deleted_at,
				updated_at = excluded.updated_at`, models.UsageIdentityAuthTypeAuthFile, "oauth", now, now).Error; err != nil {
			return fmt.Errorf("migrate auth_files to usage_identities: %w", err)
		}
	}
	if tx.Migrator().HasTable("provider_metadata") {
		isDeletedSelect, deletedAtSelect := legacyDeletedStateSelect(tx, "provider_metadata")
		if err := tx.Exec(`
			INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, is_deleted, created_at, updated_at, deleted_at)
			SELECT display_name, ?, ?, lookup_key, provider_type, display_name, `+isDeletedSelect+`, COALESCE(created_at, ?), ?, `+deletedAtSelect+`
			FROM provider_metadata
			WHERE lookup_key IS NOT NULL AND TRIM(lookup_key) != ''
			ON CONFLICT(auth_type, identity) DO UPDATE SET
				name = excluded.name,
				auth_type_name = excluded.auth_type_name,
				type = excluded.type,
				provider = excluded.provider,
				is_deleted = excluded.is_deleted,
				deleted_at = excluded.deleted_at,
				updated_at = excluded.updated_at`, models.UsageIdentityAuthTypeAIProvider, "apikey", now, now).Error; err != nil {
			return fmt.Errorf("migrate provider_metadata to usage_identities: %w", err)
		}
	}
	return nil
}

func legacyDeletedStateSelect(tx *gorm.DB, table string) (string, string) {
	if tx.Migrator().HasColumn(table, "deleted_at") {
		return "deleted_at IS NOT NULL", "deleted_at"
	}
	return "false", "NULL"
}

func backfillUsageEventIdentityFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageIdentity{}) || !tx.Migrator().HasTable(&models.UsageEvent{}) {
		return nil
	}
	for _, column := range []string{"auth_type", "provider", "source", "auth_index"} {
		if !tx.Migrator().HasColumn(&models.UsageEvent{}, column) {
			return nil
		}
	}

	if err := tx.Exec(`
		UPDATE usage_events
		SET auth_type = CASE
				WHEN TRIM(COALESCE(auth_type, '')) = '' THEN ?
				ELSE auth_type
			END,
			provider = CASE
				WHEN TRIM(COALESCE(provider, '')) = '' THEN COALESCE((
					SELECT NULLIF(TRIM(usage_identities.provider), '')
					FROM usage_identities
					WHERE usage_identities.auth_type = ?
						AND usage_identities.identity = usage_events.source
					LIMIT 1
				), provider)
				ELSE provider
			END
		WHERE EXISTS (
			SELECT 1
			FROM usage_identities
			WHERE usage_identities.auth_type = ?
				AND usage_identities.identity = usage_events.source
		)
		AND (TRIM(COALESCE(auth_type, '')) = '' OR TRIM(COALESCE(provider, '')) = '')`, "apikey", models.UsageIdentityAuthTypeAIProvider, models.UsageIdentityAuthTypeAIProvider).Error; err != nil {
		return fmt.Errorf("backfill AI provider usage event identity fields: %w", err)
	}

	if err := tx.Exec(`
		UPDATE usage_events
		SET auth_type = ?
		WHERE TRIM(COALESCE(auth_type, '')) = ''
		AND EXISTS (
			SELECT 1
			FROM usage_identities
			WHERE usage_identities.auth_type = ?
				AND usage_identities.identity = usage_events.auth_index
		)`, "oauth", models.UsageIdentityAuthTypeAuthFile).Error; err != nil {
		return fmt.Errorf("backfill auth file usage event identity fields: %w", err)
	}
	return nil
}

func backfillUsageIdentityStatsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageIdentity{}) || !tx.Migrator().HasTable(&models.UsageEvent{}) {
		return nil
	}
	for _, column := range []string{"auth_type", "source", "auth_index"} {
		if !tx.Migrator().HasColumn(&models.UsageEvent{}, column) {
			return nil
		}
	}

	var identities []models.UsageIdentity
	if err := tx.Find(&identities).Error; err != nil {
		return fmt.Errorf("list usage identities for stats backfill: %w", err)
	}
	for _, identity := range identities {
		stats, err := aggregateUsageIdentityFullStats(tx, identity)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"total_requests":                 stats.TotalRequests,
			"success_count":                  stats.SuccessCount,
			"failure_count":                  stats.FailureCount,
			"input_tokens":                   stats.InputTokens,
			"output_tokens":                  stats.OutputTokens,
			"reasoning_tokens":               stats.ReasoningTokens,
			"cached_tokens":                  stats.CachedTokens,
			"total_tokens":                   stats.TotalTokens,
			"first_used_at":                  stats.FirstUsedAt,
			"last_used_at":                   stats.LastUsedAt,
			"stats_updated_at":               nil,
			"last_aggregated_usage_event_id": stats.MaxUsageEventID,
		}
		if stats.TotalRequests > 0 {
			now := time.Now().UTC()
			updates["stats_updated_at"] = now
		}
		if err := tx.Model(&models.UsageIdentity{}).Where("id = ?", identity.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("backfill usage identity stats for %q: %w", identity.Identity, err)
		}
	}
	return nil
}

func aggregateUsageIdentityFullStats(tx *gorm.DB, identity models.UsageIdentity) (usageIdentityStatsDelta, error) {
	var stats usageIdentityStatsDelta
	query, ok := usageIdentityBackfillEventsQuery(tx.Model(&models.UsageEvent{}), identity)
	if !ok {
		return stats, nil
	}
	if err := query.Select(`
		COUNT(*) AS total_requests,
		COALESCE(SUM(CASE WHEN failed THEN 0 ELSE 1 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN failed THEN 1 ELSE 0 END), 0) AS failure_count,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
		COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
		COALESCE(SUM(total_tokens), 0) AS total_tokens,
		COALESCE(MAX(id), 0) AS max_usage_event_id`).
		Scan(&stats).Error; err != nil {
		return stats, fmt.Errorf("aggregate full usage identity stats for %q: %w", identity.Identity, err)
	}
	if stats.TotalRequests == 0 {
		return stats, nil
	}

	var firstEvent models.UsageEvent
	firstQuery, _ := usageIdentityBackfillEventsQuery(tx.Model(&models.UsageEvent{}), identity)
	if err := firstQuery.Order("timestamp asc, id asc").First(&firstEvent).Error; err != nil {
		return stats, fmt.Errorf("find first usage identity event for %q: %w", identity.Identity, err)
	}
	firstUsedAt := firstEvent.Timestamp
	stats.FirstUsedAt = &firstUsedAt

	var lastEvent models.UsageEvent
	lastQuery, _ := usageIdentityBackfillEventsQuery(tx.Model(&models.UsageEvent{}), identity)
	if err := lastQuery.Order("timestamp desc, id desc").First(&lastEvent).Error; err != nil {
		return stats, fmt.Errorf("find last usage identity event for %q: %w", identity.Identity, err)
	}
	lastUsedAt := lastEvent.Timestamp
	stats.LastUsedAt = &lastUsedAt
	return stats, nil
}

func usageIdentityBackfillEventsQuery(query *gorm.DB, identity models.UsageIdentity) (*gorm.DB, bool) {
	switch identity.AuthType {
	case models.UsageIdentityAuthTypeAuthFile:
		return query.Where("auth_index = ? AND (auth_type = ? OR TRIM(COALESCE(auth_type, '')) = '')", identity.Identity, "oauth"), true
	case models.UsageIdentityAuthTypeAIProvider:
		return query.Where("source = ? AND (auth_type = ? OR TRIM(COALESCE(auth_type, '')) = '')", identity.Identity, "apikey"), true
	default:
		return query, false
	}
}

func dropLegacyMetadataTablesMigration(tx *gorm.DB) error {
	if err := tx.Exec("DROP TABLE IF EXISTS auth_files").Error; err != nil {
		return fmt.Errorf("drop auth_files table: %w", err)
	}
	if err := tx.Exec("DROP TABLE IF EXISTS provider_metadata").Error; err != nil {
		return fmt.Errorf("drop provider_metadata table: %w", err)
	}
	return nil
}
