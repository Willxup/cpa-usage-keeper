package migration

import (
	"fmt"

	"gorm.io/gorm"
)

// backfillClaudeUsageTokensMigration 修复旧版 Claude usage token 口径。
//
// 这次回填只修正已经进入各自聚合 cursor 的历史事件：
// - usage_events 先统一修正成新口径；
// - overview 只补已经聚合过的事件，未聚合事件留给后续 catch-up；
// - usage_identities 也只补已经聚合过的事件，避免和后续增量聚合重复累加。
func backfillClaudeUsageTokensMigration(tx *gorm.DB) error {
	for _, table := range []string{"usage_events", "usage_identities", "usage_overview_hourly_stats", "usage_overview_daily_stats", "usage_overview_aggregation_checkpoints"} {
		if !tx.Migrator().HasTable(table) {
			return nil
		}
	}

	statements := []string{
		`DROP TABLE IF EXISTS temp_claude_usage_token_backfill`,
		`CREATE TEMP TABLE temp_claude_usage_token_backfill AS
WITH candidate_events AS (
	SELECT
		e.id,
		CASE e.auth_type
			WHEN 'oauth' THEN 1
			WHEN 'apikey' THEN 2
			WHEN 'api_key' THEN 2
			ELSE 0
		END AS identity_auth_type,
		trim(COALESCE(e.auth_index, '')) AS auth_index,
		CASE
			WHEN trim(COALESCE(e.api_group_key, '')) = '' THEN 'unknown'
			ELSE trim(COALESCE(e.api_group_key, ''))
		END AS api_group_key,
		CASE
			WHEN trim(COALESCE(e.model, '')) = '' THEN 'unknown'
			ELSE trim(COALESCE(e.model, ''))
		END AS model,
		CASE
			WHEN trim(COALESCE(e.model_alias, '')) = '' THEN ''
			ELSE trim(COALESCE(e.model_alias, ''))
		END AS model_alias,
		COALESCE(e.timestamp, '') AS timestamp_value,
		COALESCE(e.input_tokens, 0) AS input_tokens,
		COALESCE(e.output_tokens, 0) AS output_tokens,
		COALESCE(e.reasoning_tokens, 0) AS reasoning_tokens,
		COALESCE(e.cached_tokens, 0) AS cached_tokens,
		COALESCE(e.cache_read_tokens, 0) AS cache_read_tokens,
		COALESCE(e.cache_creation_tokens, 0) AS cache_creation_tokens,
		COALESCE(e.total_tokens, 0) AS total_tokens,
		COALESCE((SELECT last_aggregated_usage_event_id FROM usage_overview_aggregation_checkpoints WHERE name = 'overview' LIMIT 1), 0) AS overview_last_aggregated_usage_event_id,
		COALESCE(ui.last_aggregated_usage_event_id, 0) AS identity_last_aggregated_usage_event_id
	FROM usage_events e
	LEFT JOIN usage_identities ui
		ON ui.auth_type = CASE e.auth_type
			WHEN 'oauth' THEN 1
			WHEN 'apikey' THEN 2
			WHEN 'api_key' THEN 2
			ELSE 0
		END
		AND ui.identity = trim(COALESCE(e.auth_index, ''))
	WHERE (COALESCE(e.cache_read_tokens, 0) + COALESCE(e.cache_creation_tokens, 0)) > 0
		AND (
			(
				e.auth_type = 'oauth'
				AND lower(trim(COALESCE(e.provider, ''))) IN ('claude', 'anthropic')
			)
			OR (
				e.auth_type IN ('apikey', 'api_key')
				AND EXISTS (
					SELECT 1
					FROM usage_identities ui2
					WHERE ui2.auth_type = 2
						AND ui2.identity = trim(COALESCE(e.auth_index, ''))
						AND lower(trim(COALESCE(ui2.type, ''))) IN ('claude', 'anthropic')
				)
			)
		)
),
proposed_events AS (
	SELECT
		*,
		CASE
			WHEN total_tokens = 0 THEN input_tokens + cache_read_tokens + cache_creation_tokens
			WHEN total_tokens > output_tokens + reasoning_tokens THEN total_tokens - output_tokens - reasoning_tokens
			ELSE input_tokens
		END AS proposed_input_tokens,
		cache_read_tokens AS proposed_cached_tokens,
		CASE
			WHEN total_tokens = 0 THEN input_tokens + cache_read_tokens + cache_creation_tokens + output_tokens + reasoning_tokens
			ELSE total_tokens
		END AS proposed_total_tokens
	FROM candidate_events
),
normalized_events AS (
	SELECT
		*,
		CASE WHEN proposed_input_tokens > input_tokens THEN proposed_input_tokens ELSE input_tokens END AS new_input_tokens,
		proposed_cached_tokens AS new_cached_tokens,
		CASE WHEN proposed_total_tokens > total_tokens THEN proposed_total_tokens ELSE total_tokens END AS new_total_tokens,
		CASE WHEN id <= overview_last_aggregated_usage_event_id THEN 1 ELSE 0 END AS apply_overview_delta,
		CASE WHEN id <= identity_last_aggregated_usage_event_id THEN 1 ELSE 0 END AS apply_identity_delta
	FROM proposed_events
)
SELECT
	id,
	identity_auth_type,
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
	overview_last_aggregated_usage_event_id,
	identity_last_aggregated_usage_event_id,
	apply_overview_delta,
	apply_identity_delta,
	new_input_tokens,
	new_cached_tokens,
	new_total_tokens,
	new_input_tokens - input_tokens AS input_delta,
	new_cached_tokens - cached_tokens AS cached_delta,
	new_total_tokens - total_tokens AS total_delta
FROM normalized_events
WHERE identity_auth_type IN (1, 2)
	AND (
		new_input_tokens > input_tokens
		OR new_cached_tokens <> cached_tokens
		OR new_total_tokens <> total_tokens
	)`,
		`CREATE INDEX temp_claude_usage_token_backfill_id ON temp_claude_usage_token_backfill (id)`,
		`UPDATE usage_events
SET
	input_tokens = (SELECT new_input_tokens FROM temp_claude_usage_token_backfill t WHERE t.id = usage_events.id),
	cached_tokens = (SELECT new_cached_tokens FROM temp_claude_usage_token_backfill t WHERE t.id = usage_events.id),
	total_tokens = (SELECT new_total_tokens FROM temp_claude_usage_token_backfill t WHERE t.id = usage_events.id)
WHERE id IN (SELECT id FROM temp_claude_usage_token_backfill)`,
		`DROP TABLE IF EXISTS temp_claude_usage_token_hourly`,
		`CREATE TEMP TABLE temp_claude_usage_token_hourly AS
SELECT
	hourly_bucket_start AS bucket_start,
	api_group_key,
	model,
	auth_index,
	model_alias,
	SUM(input_delta) AS input_delta,
	SUM(cached_delta) AS cached_delta,
	SUM(total_delta) AS total_delta
FROM temp_claude_usage_token_backfill
WHERE hourly_bucket_start <> ''
	AND apply_overview_delta = 1
GROUP BY hourly_bucket_start, api_group_key, model, auth_index, model_alias`,
		`CREATE INDEX temp_claude_usage_token_hourly_key ON temp_claude_usage_token_hourly (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`UPDATE usage_overview_hourly_stats
SET
	input_tokens = COALESCE(input_tokens, 0) + (
		SELECT input_delta FROM temp_claude_usage_token_hourly t
		WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
			AND t.api_group_key = usage_overview_hourly_stats.api_group_key
			AND t.model = usage_overview_hourly_stats.model
			AND t.auth_index = usage_overview_hourly_stats.auth_index
			AND t.model_alias = usage_overview_hourly_stats.model_alias
	),
	cached_tokens = CASE
		WHEN COALESCE(cached_tokens, 0) + (
			SELECT cached_delta FROM temp_claude_usage_token_hourly t
			WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
				AND t.api_group_key = usage_overview_hourly_stats.api_group_key
				AND t.model = usage_overview_hourly_stats.model
				AND t.auth_index = usage_overview_hourly_stats.auth_index
				AND t.model_alias = usage_overview_hourly_stats.model_alias
		) < 0 THEN 0
		ELSE COALESCE(cached_tokens, 0) + (
			SELECT cached_delta FROM temp_claude_usage_token_hourly t
			WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
				AND t.api_group_key = usage_overview_hourly_stats.api_group_key
				AND t.model = usage_overview_hourly_stats.model
				AND t.auth_index = usage_overview_hourly_stats.auth_index
				AND t.model_alias = usage_overview_hourly_stats.model_alias
		)
	END,
	total_tokens = COALESCE(total_tokens, 0) + (
		SELECT total_delta FROM temp_claude_usage_token_hourly t
		WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
			AND t.api_group_key = usage_overview_hourly_stats.api_group_key
			AND t.model = usage_overview_hourly_stats.model
			AND t.auth_index = usage_overview_hourly_stats.auth_index
			AND t.model_alias = usage_overview_hourly_stats.model_alias
	)
WHERE EXISTS (
	SELECT 1 FROM temp_claude_usage_token_hourly t
	WHERE t.bucket_start = usage_overview_hourly_stats.bucket_start
		AND t.api_group_key = usage_overview_hourly_stats.api_group_key
		AND t.model = usage_overview_hourly_stats.model
		AND t.auth_index = usage_overview_hourly_stats.auth_index
		AND t.model_alias = usage_overview_hourly_stats.model_alias
)`,
		`DROP TABLE IF EXISTS temp_claude_usage_token_daily`,
		`CREATE TEMP TABLE temp_claude_usage_token_daily AS
SELECT
	daily_bucket_start AS bucket_start,
	api_group_key,
	model,
	auth_index,
	model_alias,
	SUM(input_delta) AS input_delta,
	SUM(cached_delta) AS cached_delta,
	SUM(total_delta) AS total_delta
FROM temp_claude_usage_token_backfill
WHERE daily_bucket_start <> ''
	AND apply_overview_delta = 1
GROUP BY daily_bucket_start, api_group_key, model, auth_index, model_alias`,
		`CREATE INDEX temp_claude_usage_token_daily_key ON temp_claude_usage_token_daily (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`UPDATE usage_overview_daily_stats
SET
	input_tokens = COALESCE(input_tokens, 0) + (
		SELECT input_delta FROM temp_claude_usage_token_daily t
		WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
			AND t.api_group_key = usage_overview_daily_stats.api_group_key
			AND t.model = usage_overview_daily_stats.model
			AND t.auth_index = usage_overview_daily_stats.auth_index
			AND t.model_alias = usage_overview_daily_stats.model_alias
	),
	cached_tokens = CASE
		WHEN COALESCE(cached_tokens, 0) + (
			SELECT cached_delta FROM temp_claude_usage_token_daily t
			WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
				AND t.api_group_key = usage_overview_daily_stats.api_group_key
				AND t.model = usage_overview_daily_stats.model
				AND t.auth_index = usage_overview_daily_stats.auth_index
				AND t.model_alias = usage_overview_daily_stats.model_alias
		) < 0 THEN 0
		ELSE COALESCE(cached_tokens, 0) + (
			SELECT cached_delta FROM temp_claude_usage_token_daily t
			WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
				AND t.api_group_key = usage_overview_daily_stats.api_group_key
				AND t.model = usage_overview_daily_stats.model
				AND t.auth_index = usage_overview_daily_stats.auth_index
				AND t.model_alias = usage_overview_daily_stats.model_alias
		)
	END,
	total_tokens = COALESCE(total_tokens, 0) + (
		SELECT total_delta FROM temp_claude_usage_token_daily t
		WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
			AND t.api_group_key = usage_overview_daily_stats.api_group_key
			AND t.model = usage_overview_daily_stats.model
			AND t.auth_index = usage_overview_daily_stats.auth_index
			AND t.model_alias = usage_overview_daily_stats.model_alias
	)
WHERE EXISTS (
	SELECT 1 FROM temp_claude_usage_token_daily t
	WHERE t.bucket_start = usage_overview_daily_stats.bucket_start
		AND t.api_group_key = usage_overview_daily_stats.api_group_key
		AND t.model = usage_overview_daily_stats.model
		AND t.auth_index = usage_overview_daily_stats.auth_index
		AND t.model_alias = usage_overview_daily_stats.model_alias
)`,
		`DROP TABLE IF EXISTS temp_claude_usage_token_identity`,
		`CREATE TEMP TABLE temp_claude_usage_token_identity AS
SELECT
	identity_auth_type AS auth_type,
	auth_index AS identity,
	SUM(input_delta) AS input_delta,
	SUM(cached_delta) AS cached_delta,
	SUM(total_delta) AS total_delta
FROM temp_claude_usage_token_backfill
WHERE auth_index <> ''
	AND apply_identity_delta = 1
GROUP BY identity_auth_type, auth_index`,
		`CREATE INDEX temp_claude_usage_token_identity_key ON temp_claude_usage_token_identity (auth_type, identity)`,
		`UPDATE usage_identities
SET
	input_tokens = COALESCE(input_tokens, 0) + (
		SELECT input_delta FROM temp_claude_usage_token_identity t
		WHERE t.auth_type = usage_identities.auth_type
			AND t.identity = usage_identities.identity
	),
	cached_tokens = CASE
		WHEN COALESCE(cached_tokens, 0) + (
			SELECT cached_delta FROM temp_claude_usage_token_identity t
			WHERE t.auth_type = usage_identities.auth_type
				AND t.identity = usage_identities.identity
		) < 0 THEN 0
		ELSE COALESCE(cached_tokens, 0) + (
			SELECT cached_delta FROM temp_claude_usage_token_identity t
			WHERE t.auth_type = usage_identities.auth_type
				AND t.identity = usage_identities.identity
		)
	END,
	total_tokens = COALESCE(total_tokens, 0) + (
		SELECT total_delta FROM temp_claude_usage_token_identity t
		WHERE t.auth_type = usage_identities.auth_type
			AND t.identity = usage_identities.identity
	)
WHERE EXISTS (
	SELECT 1 FROM temp_claude_usage_token_identity t
	WHERE t.auth_type = usage_identities.auth_type
		AND t.identity = usage_identities.identity
)`,
		`DROP TABLE IF EXISTS temp_claude_usage_token_identity`,
		`DROP TABLE IF EXISTS temp_claude_usage_token_daily`,
		`DROP TABLE IF EXISTS temp_claude_usage_token_hourly`,
		`DROP TABLE IF EXISTS temp_claude_usage_token_backfill`,
	}

	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("backfill Claude usage tokens: %w", err)
		}
	}
	return nil
}
