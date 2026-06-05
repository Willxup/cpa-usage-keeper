package migration

import (
	"fmt"

	"gorm.io/gorm"
)

// backfillGeminiCodexTokenFormatMigration 把 Gemini family 旧事件归一到 Codex token 口径。
//
// 只迁移确认属于 Gemini family 的事件：
// - 能匹配 usage_identities 时，以 identity.type 为准；
// - 找不到 identity 时，才 fallback 到 event.provider；
// - 不匹配 Gemini family 的事件不会进入临时表，也不会更新明细或聚合。
func backfillGeminiCodexTokenFormatMigration(tx *gorm.DB) error {
	for _, table := range []string{"usage_events", "usage_identities", "usage_overview_hourly_stats", "usage_overview_daily_stats", "usage_overview_aggregation_checkpoints"} {
		if !tx.Migrator().HasTable(table) {
			return nil
		}
	}

	statements := []string{
		`DROP TABLE IF EXISTS temp_gemini_codex_token_backfill`,
		`CREATE TEMP TABLE temp_gemini_codex_token_backfill AS
WITH source_events AS (
	SELECT
		e.id,
		e.auth_type AS raw_auth_type,
		e.auth_index AS raw_auth_index,
		COALESCE(e.auth_index, '') AS auth_index,
		CASE
			WHEN COALESCE(e.api_group_key, '') = '' THEN 'unknown'
			ELSE COALESCE(e.api_group_key, '')
		END AS api_group_key,
		CASE
			WHEN COALESCE(e.model, '') = '' THEN 'unknown'
			ELSE COALESCE(e.model, '')
		END AS model,
		CASE
			WHEN COALESCE(e.model_alias, '') = '' THEN ''
			ELSE COALESCE(e.model_alias, '')
		END AS model_alias,
		COALESCE(e.timestamp, '') AS timestamp_value,
		COALESCE(e.input_tokens, 0) AS input_tokens,
		COALESCE(e.output_tokens, 0) AS output_tokens,
		COALESCE(e.reasoning_tokens, 0) AS reasoning_tokens,
		COALESCE(e.total_tokens, 0) AS total_tokens,
		CASE
			WHEN lower(COALESCE(e.provider, '')) IN ('gemini', 'vertex', 'gemini-cli', 'gemini-cli-code-assist', 'antigravity', 'aistudio', 'ai-studio') THEN 1
			ELSE 0
		END AS provider_is_gemini_family
	FROM usage_events e
	WHERE COALESCE(e.reasoning_tokens, 0) > 0
),
candidate_events AS (
	SELECT
		e.id,
		ui.id AS identity_id,
		e.auth_index,
		e.api_group_key,
		e.model,
		e.model_alias,
		e.timestamp_value,
		e.input_tokens,
		e.output_tokens,
		e.reasoning_tokens,
		e.total_tokens,
		e.provider_is_gemini_family,
		CASE
			WHEN lower(COALESCE(ui.type, '')) IN ('gemini', 'vertex', 'gemini-cli', 'gemini-cli-code-assist', 'antigravity', 'aistudio', 'ai-studio') THEN 1
			ELSE 0
		END AS identity_is_gemini_family,
		COALESCE((SELECT last_aggregated_usage_event_id FROM usage_overview_aggregation_checkpoints WHERE name = 'overview' LIMIT 1), 0) AS overview_last_aggregated_usage_event_id,
		COALESCE(ui.last_aggregated_usage_event_id, 0) AS identity_last_aggregated_usage_event_id
	FROM source_events e
	LEFT JOIN usage_identities ui
		ON ui.auth_type = CASE e.raw_auth_type
			WHEN 'oauth' THEN 1
			WHEN 'apikey' THEN 2
			ELSE -1
		END
		AND ui.identity = e.raw_auth_index
),
normalized_events AS (
	SELECT
		*,
		output_tokens + reasoning_tokens AS new_output_tokens,
		reasoning_tokens AS output_delta,
		CASE WHEN id <= overview_last_aggregated_usage_event_id THEN 1 ELSE 0 END AS apply_overview_delta,
		CASE WHEN identity_id IS NOT NULL AND id <= identity_last_aggregated_usage_event_id THEN 1 ELSE 0 END AS apply_identity_delta
	FROM candidate_events
	WHERE total_tokens > 0
		AND input_tokens + output_tokens <> total_tokens
		AND input_tokens + output_tokens + reasoning_tokens = total_tokens
		AND (
			(identity_id IS NOT NULL AND identity_is_gemini_family = 1)
			OR (identity_id IS NULL AND provider_is_gemini_family = 1)
		)
)
SELECT
	id,
	identity_id,
	auth_index,
	api_group_key,
	model,
	model_alias,
	CASE
		WHEN timestamp_value = '' THEN ''
		WHEN substr(timestamp_value, length(timestamp_value), 1) = 'Z' THEN substr(timestamp_value, 1, 13) || ':00:00Z'
		WHEN length(timestamp_value) >= 25 THEN substr(timestamp_value, 1, 13) || ':00:00' || substr(timestamp_value, length(timestamp_value) - 5, 6)
		ELSE substr(timestamp_value, 1, 13) || ':00:00'
	END AS hourly_bucket_start,
	CASE
		WHEN timestamp_value = '' THEN ''
		WHEN substr(timestamp_value, length(timestamp_value), 1) = 'Z' THEN substr(timestamp_value, 1, 10) || 'T00:00:00Z'
		WHEN length(timestamp_value) >= 25 THEN substr(timestamp_value, 1, 10) || 'T00:00:00' || substr(timestamp_value, length(timestamp_value) - 5, 6)
		ELSE substr(timestamp_value, 1, 10) || 'T00:00:00'
	END AS daily_bucket_start,
	apply_overview_delta,
	apply_identity_delta,
	new_output_tokens,
	output_delta
FROM normalized_events`,
		`CREATE INDEX temp_gemini_codex_token_backfill_id ON temp_gemini_codex_token_backfill (id)`,
		`UPDATE usage_events
SET
	output_tokens = (SELECT new_output_tokens FROM temp_gemini_codex_token_backfill t WHERE t.id = usage_events.id)
WHERE id IN (SELECT id FROM temp_gemini_codex_token_backfill)`,
		`DROP TABLE IF EXISTS temp_gemini_codex_token_hourly`,
		`CREATE TEMP TABLE temp_gemini_codex_token_hourly AS
SELECT
	hourly_bucket_start AS bucket_start,
	api_group_key,
	model,
	auth_index,
	model_alias,
	SUM(output_delta) AS output_delta
FROM temp_gemini_codex_token_backfill
WHERE hourly_bucket_start <> ''
	AND apply_overview_delta = 1
GROUP BY hourly_bucket_start, api_group_key, model, auth_index, model_alias`,
		`CREATE INDEX temp_gemini_codex_token_hourly_key ON temp_gemini_codex_token_hourly (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`UPDATE usage_overview_hourly_stats
SET
	output_tokens = COALESCE(output_tokens, 0) + (
		SELECT output_delta FROM temp_gemini_codex_token_hourly t
		WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
			AND t.api_group_key = usage_overview_hourly_stats.api_group_key
			AND t.model = usage_overview_hourly_stats.model
			AND t.auth_index = usage_overview_hourly_stats.auth_index
			AND t.model_alias = usage_overview_hourly_stats.model_alias
	)
WHERE EXISTS (
	SELECT 1 FROM temp_gemini_codex_token_hourly t
	WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
		AND t.api_group_key = usage_overview_hourly_stats.api_group_key
		AND t.model = usage_overview_hourly_stats.model
		AND t.auth_index = usage_overview_hourly_stats.auth_index
		AND t.model_alias = usage_overview_hourly_stats.model_alias
)`,
		`DROP TABLE IF EXISTS temp_gemini_codex_token_daily`,
		`CREATE TEMP TABLE temp_gemini_codex_token_daily AS
SELECT
	daily_bucket_start AS bucket_start,
	api_group_key,
	model,
	auth_index,
	model_alias,
	SUM(output_delta) AS output_delta
FROM temp_gemini_codex_token_backfill
WHERE daily_bucket_start <> ''
	AND apply_overview_delta = 1
GROUP BY daily_bucket_start, api_group_key, model, auth_index, model_alias`,
		`CREATE INDEX temp_gemini_codex_token_daily_key ON temp_gemini_codex_token_daily (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`UPDATE usage_overview_daily_stats
SET
	output_tokens = COALESCE(output_tokens, 0) + (
		SELECT output_delta FROM temp_gemini_codex_token_daily t
		WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
			AND t.api_group_key = usage_overview_daily_stats.api_group_key
			AND t.model = usage_overview_daily_stats.model
			AND t.auth_index = usage_overview_daily_stats.auth_index
			AND t.model_alias = usage_overview_daily_stats.model_alias
	)
WHERE EXISTS (
	SELECT 1 FROM temp_gemini_codex_token_daily t
	WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
		AND t.api_group_key = usage_overview_daily_stats.api_group_key
		AND t.model = usage_overview_daily_stats.model
		AND t.auth_index = usage_overview_daily_stats.auth_index
		AND t.model_alias = usage_overview_daily_stats.model_alias
)`,
		`DROP TABLE IF EXISTS temp_gemini_codex_token_identity`,
		`CREATE TEMP TABLE temp_gemini_codex_token_identity AS
SELECT
	identity_id,
	SUM(output_delta) AS output_delta
FROM temp_gemini_codex_token_backfill
WHERE identity_id IS NOT NULL
	AND apply_identity_delta = 1
GROUP BY identity_id`,
		`CREATE INDEX temp_gemini_codex_token_identity_key ON temp_gemini_codex_token_identity (identity_id)`,
		`UPDATE usage_identities
SET
	output_tokens = COALESCE(output_tokens, 0) + (
		SELECT output_delta FROM temp_gemini_codex_token_identity t
		WHERE t.identity_id = usage_identities.id
	)
WHERE id IN (SELECT identity_id FROM temp_gemini_codex_token_identity)`,
		`DROP TABLE IF EXISTS temp_gemini_codex_token_identity`,
		`DROP TABLE IF EXISTS temp_gemini_codex_token_daily`,
		`DROP TABLE IF EXISTS temp_gemini_codex_token_hourly`,
		`DROP TABLE IF EXISTS temp_gemini_codex_token_backfill`,
	}

	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("backfill Gemini Codex token format: %w", err)
		}
	}
	return nil
}
