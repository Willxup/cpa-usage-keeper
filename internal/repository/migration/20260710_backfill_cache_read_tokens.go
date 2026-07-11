package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const cacheReadTokenBackfillBucketBatchSize = 200

// backfillCacheReadTokensMigration 在一个事务内完成缓存读取 schema 统一和历史聚合修复。
// raw event 先固化 canonical read；已聚合数据使用 cached 基线加 retained event 差量，
// 从而保留已经被 cleanup 删除的历史明细贡献。
func backfillCacheReadTokensMigration(tx *gorm.DB) error {
	if err := addUsageIdentityCacheReadTokensColumn(tx); err != nil {
		return err
	}
	if err := renameModelPriceCacheReadColumn(tx); err != nil {
		return err
	}
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}

	if err := createCacheReadTokenBackfillSnapshot(tx); err != nil {
		return err
	}
	if cacheReadTokenOverviewTablesExist(tx) {
		if err := populateCacheReadTokenBackfillBuckets(tx); err != nil {
			return err
		}
		if err := repairCacheReadTokenOverviewRollups(tx); err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&entities.UsageIdentity{}) {
		if err := repairUsageIdentityCacheReadTokens(tx); err != nil {
			return err
		}
	}
	if err := updateUsageEventCacheReadTokens(tx); err != nil {
		return err
	}
	if err := dropCacheReadTokenBackfillTempTables(tx); err != nil {
		return err
	}
	return nil
}

func addUsageIdentityCacheReadTokensColumn(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) || tx.Migrator().HasColumn(&entities.UsageIdentity{}, "cache_read_tokens") {
		return nil
	}
	if err := tx.Migrator().AddColumn(&entities.UsageIdentity{}, "CacheReadTokens"); err != nil {
		return fmt.Errorf("add usage_identities.cache_read_tokens: %w", err)
	}
	return nil
}

func renameModelPriceCacheReadColumn(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.ModelPriceSetting{}) {
		return nil
	}
	oldExists := tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, "cache_price_per1_m")
	newExists := tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, "cache_read_price_per1_m")
	if !oldExists || newExists {
		return nil
	}
	if err := tx.Migrator().RenameColumn(&entities.ModelPriceSetting{}, "cache_price_per1_m", "cache_read_price_per1_m"); err != nil {
		return fmt.Errorf("rename model_price_settings cache read price column: %w", err)
	}
	return nil
}

func createCacheReadTokenBackfillSnapshot(tx *gorm.DB) error {
	identityColumns := "NULL AS identity_id, 0 AS identity_last_aggregated_usage_event_id"
	identityJoin := ""
	if tx.Migrator().HasTable(&entities.UsageIdentity{}) {
		identityColumns = "ui.id AS identity_id, COALESCE(ui.last_aggregated_usage_event_id, 0) AS identity_last_aggregated_usage_event_id"
		identityJoin = `LEFT JOIN usage_identities ui
		ON ui.auth_type = CASE COALESCE(e.auth_type, '')
			WHEN 'oauth' THEN 1
			WHEN 'apikey' THEN 2
			ELSE 0
		END
		AND ui.identity = COALESCE(e.auth_index, '')`
	}

	overviewCursor := "0"
	if tx.Migrator().HasTable(&entities.UsageOverviewAggregationCheckpoint{}) {
		overviewCursor = `COALESCE((
		SELECT last_aggregated_usage_event_id
		FROM usage_overview_aggregation_checkpoints
		WHERE name = 'overview'
		LIMIT 1
	), 0)`
	}

	statements := []string{
		`DROP TABLE IF EXISTS temp_cache_read_token_backfill`,
		fmt.Sprintf(`CREATE TEMP TABLE temp_cache_read_token_backfill AS
SELECT
	e.id,
	%s,
	CASE WHEN trim(COALESCE(e.api_group_key, '')) = '' THEN 'unknown' ELSE trim(e.api_group_key) END AS api_group_key,
	CASE WHEN trim(COALESCE(e.model, '')) = '' THEN 'unknown' ELSE trim(e.model) END AS model,
	trim(COALESCE(e.auth_index, '')) AS auth_index,
	trim(COALESCE(e.model_alias, '')) AS model_alias,
	COALESCE(e.timestamp, '') AS timestamp_value,
	COALESCE(e.cached_tokens, 0) AS old_cached_tokens,
	COALESCE(e.cache_read_tokens, 0) AS old_cache_read_tokens,
	CASE
		WHEN COALESCE(e.cache_read_tokens, 0) > 0 THEN COALESCE(e.cache_read_tokens, 0)
		ELSE COALESCE(e.cached_tokens, 0)
	END AS canonical_cache_read_tokens,
	%s AS overview_last_aggregated_usage_event_id
FROM usage_events e
%s`, identityColumns, overviewCursor, identityJoin),
		`CREATE INDEX temp_cache_read_token_backfill_id ON temp_cache_read_token_backfill (id)`,
		`DROP TABLE IF EXISTS temp_cache_read_token_buckets`,
		`CREATE TEMP TABLE temp_cache_read_token_buckets (
	id INTEGER PRIMARY KEY,
	hourly_bucket_start TEXT NOT NULL,
	daily_bucket_start TEXT NOT NULL
)`,
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("create cache read token backfill snapshot: %w", err)
		}
	}
	return nil
}

func cacheReadTokenOverviewTablesExist(tx *gorm.DB) bool {
	return tx.Migrator().HasTable(&entities.UsageOverviewHourlyStat{}) &&
		tx.Migrator().HasTable(&entities.UsageOverviewDailyStat{}) &&
		tx.Migrator().HasTable(&entities.UsageOverviewAggregationCheckpoint{})
}

func repairCacheReadTokenOverviewRollups(tx *gorm.DB) error {
	statements := []string{
		`UPDATE usage_overview_hourly_stats SET cache_read_tokens = COALESCE(cached_tokens, 0)`,
		`DROP TABLE IF EXISTS temp_cache_read_token_hourly`,
		`CREATE TEMP TABLE temp_cache_read_token_hourly AS
SELECT
	b.hourly_bucket_start AS bucket_start,
	e.api_group_key,
	e.model,
	e.auth_index,
	e.model_alias,
	SUM(e.canonical_cache_read_tokens - e.old_cached_tokens) AS cache_read_delta
FROM temp_cache_read_token_backfill e
JOIN temp_cache_read_token_buckets b ON b.id = e.id
WHERE e.id <= e.overview_last_aggregated_usage_event_id
GROUP BY b.hourly_bucket_start, e.api_group_key, e.model, e.auth_index, e.model_alias`,
		`CREATE INDEX temp_cache_read_token_hourly_key ON temp_cache_read_token_hourly (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`UPDATE usage_overview_hourly_stats
SET cache_read_tokens = CASE
	WHEN COALESCE(cached_tokens, 0) + COALESCE((
		SELECT cache_read_delta FROM temp_cache_read_token_hourly t
		WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
			AND t.api_group_key = usage_overview_hourly_stats.api_group_key
			AND t.model = usage_overview_hourly_stats.model
			AND t.auth_index = usage_overview_hourly_stats.auth_index
			AND t.model_alias = usage_overview_hourly_stats.model_alias
	), 0) < 0 THEN 0
	ELSE COALESCE(cached_tokens, 0) + COALESCE((
		SELECT cache_read_delta FROM temp_cache_read_token_hourly t
		WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
			AND t.api_group_key = usage_overview_hourly_stats.api_group_key
			AND t.model = usage_overview_hourly_stats.model
			AND t.auth_index = usage_overview_hourly_stats.auth_index
			AND t.model_alias = usage_overview_hourly_stats.model_alias
	), 0)
END
WHERE EXISTS (
	SELECT 1 FROM temp_cache_read_token_hourly t
	WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
		AND t.api_group_key = usage_overview_hourly_stats.api_group_key
		AND t.model = usage_overview_hourly_stats.model
		AND t.auth_index = usage_overview_hourly_stats.auth_index
		AND t.model_alias = usage_overview_hourly_stats.model_alias
)`,
		`UPDATE usage_overview_daily_stats SET cache_read_tokens = COALESCE(cached_tokens, 0)`,
		`DROP TABLE IF EXISTS temp_cache_read_token_daily`,
		`CREATE TEMP TABLE temp_cache_read_token_daily AS
SELECT
	b.daily_bucket_start AS bucket_start,
	e.api_group_key,
	e.model,
	e.auth_index,
	e.model_alias,
	SUM(e.canonical_cache_read_tokens - e.old_cached_tokens) AS cache_read_delta
FROM temp_cache_read_token_backfill e
JOIN temp_cache_read_token_buckets b ON b.id = e.id
WHERE e.id <= e.overview_last_aggregated_usage_event_id
GROUP BY b.daily_bucket_start, e.api_group_key, e.model, e.auth_index, e.model_alias`,
		`CREATE INDEX temp_cache_read_token_daily_key ON temp_cache_read_token_daily (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`UPDATE usage_overview_daily_stats
SET cache_read_tokens = CASE
	WHEN COALESCE(cached_tokens, 0) + COALESCE((
		SELECT cache_read_delta FROM temp_cache_read_token_daily t
		WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
			AND t.api_group_key = usage_overview_daily_stats.api_group_key
			AND t.model = usage_overview_daily_stats.model
			AND t.auth_index = usage_overview_daily_stats.auth_index
			AND t.model_alias = usage_overview_daily_stats.model_alias
	), 0) < 0 THEN 0
	ELSE COALESCE(cached_tokens, 0) + COALESCE((
		SELECT cache_read_delta FROM temp_cache_read_token_daily t
		WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
			AND t.api_group_key = usage_overview_daily_stats.api_group_key
			AND t.model = usage_overview_daily_stats.model
			AND t.auth_index = usage_overview_daily_stats.auth_index
			AND t.model_alias = usage_overview_daily_stats.model_alias
	), 0)
END
WHERE EXISTS (
	SELECT 1 FROM temp_cache_read_token_daily t
	WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
		AND t.api_group_key = usage_overview_daily_stats.api_group_key
		AND t.model = usage_overview_daily_stats.model
		AND t.auth_index = usage_overview_daily_stats.auth_index
		AND t.model_alias = usage_overview_daily_stats.model_alias
)`,
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("repair overview cache read tokens: %w", err)
		}
	}
	return nil
}

func repairUsageIdentityCacheReadTokens(tx *gorm.DB) error {
	statements := []string{
		`UPDATE usage_identities SET cache_read_tokens = COALESCE(cached_tokens, 0)`,
		`DROP TABLE IF EXISTS temp_cache_read_token_identity`,
		`CREATE TEMP TABLE temp_cache_read_token_identity AS
SELECT
	identity_id,
	SUM(canonical_cache_read_tokens - old_cached_tokens) AS cache_read_delta
FROM temp_cache_read_token_backfill
WHERE identity_id IS NOT NULL
	AND id <= identity_last_aggregated_usage_event_id
GROUP BY identity_id`,
		`CREATE INDEX temp_cache_read_token_identity_key ON temp_cache_read_token_identity (identity_id)`,
		`UPDATE usage_identities
SET cache_read_tokens = CASE
	WHEN COALESCE(cached_tokens, 0) + COALESCE((
		SELECT cache_read_delta FROM temp_cache_read_token_identity t
		WHERE t.identity_id = usage_identities.id
	), 0) < 0 THEN 0
	ELSE COALESCE(cached_tokens, 0) + COALESCE((
		SELECT cache_read_delta FROM temp_cache_read_token_identity t
		WHERE t.identity_id = usage_identities.id
	), 0)
END
WHERE id IN (SELECT identity_id FROM temp_cache_read_token_identity)`,
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("repair usage identity cache read tokens: %w", err)
		}
	}
	return nil
}

func updateUsageEventCacheReadTokens(tx *gorm.DB) error {
	if err := tx.Exec(`UPDATE usage_events
SET cache_read_tokens = (
	SELECT canonical_cache_read_tokens
	FROM temp_cache_read_token_backfill t
	WHERE t.id = usage_events.id
)
WHERE id IN (
	SELECT id FROM temp_cache_read_token_backfill
	WHERE canonical_cache_read_tokens <> old_cache_read_tokens
)`).Error; err != nil {
		return fmt.Errorf("update usage event cache read tokens: %w", err)
	}
	return nil
}

func dropCacheReadTokenBackfillTempTables(tx *gorm.DB) error {
	for _, table := range []string{
		"temp_cache_read_token_identity",
		"temp_cache_read_token_daily",
		"temp_cache_read_token_hourly",
		"temp_cache_read_token_buckets",
		"temp_cache_read_token_backfill",
	} {
		if err := tx.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			return fmt.Errorf("drop cache read token temp table %s: %w", table, err)
		}
	}
	return nil
}

type cacheReadTokenBackfillTimestamp struct {
	ID             int64
	TimestampValue string `gorm:"column:timestamp_value"`
}

type cacheReadTokenBackfillBucket struct {
	ID                int64
	HourlyBucketStart string
	DailyBucketStart  string
}

// populateCacheReadTokenBackfillBuckets 与正常 Overview 聚合复用同一套项目时区逻辑，
// 避免 DST 切换日把事件时刻的 offset 错当成本地午夜的 offset。
func populateCacheReadTokenBackfillBuckets(tx *gorm.DB) error {
	lastID := int64(0)
	for {
		var rows []cacheReadTokenBackfillTimestamp
		if err := tx.Raw(`SELECT id, timestamp_value
FROM temp_cache_read_token_backfill
WHERE id <= overview_last_aggregated_usage_event_id
	AND id > ?
ORDER BY id ASC
LIMIT ?`, lastID, cacheReadTokenBackfillBucketBatchSize).Scan(&rows).Error; err != nil {
			return fmt.Errorf("backfill cache read tokens: list overview timestamps: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		buckets := make([]cacheReadTokenBackfillBucket, 0, len(rows))
		for _, row := range rows {
			parsed, err := timeutil.ParseStorageTime(row.TimestampValue)
			if err != nil {
				return fmt.Errorf("backfill cache read tokens: parse usage event %d timestamp %q: %w", row.ID, row.TimestampValue, err)
			}
			timestamp := timeutil.NormalizeStorageTime(parsed)
			hourBucket := timestamp.Truncate(time.Hour)
			dayBucket := time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, timestamp.Location())
			buckets = append(buckets, cacheReadTokenBackfillBucket{
				ID:                row.ID,
				HourlyBucketStart: timeutil.FormatStorageTime(hourBucket),
				DailyBucketStart:  timeutil.FormatStorageTime(dayBucket),
			})
		}
		if err := tx.Table("temp_cache_read_token_buckets").CreateInBatches(&buckets, cacheReadTokenBackfillBucketBatchSize).Error; err != nil {
			return fmt.Errorf("backfill cache read tokens: save overview buckets: %w", err)
		}
		lastID = rows[len(rows)-1].ID
	}
}
