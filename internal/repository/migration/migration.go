package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	migrationAddUsageEventRedisFields               = "20260503_add_usage_event_redis_fields"
	migrationBackfillUsageEventRedisFields          = "20260503_backfill_usage_event_redis_fields"
	migrationDropSnapshotRuns                       = "20260503_drop_snapshot_runs"
	migrationDropLegacySnapshotRunColumns           = "20260504_drop_legacy_snapshot_run_columns"
	migrationCreateUsageIdentities                  = "20260504_create_usage_identities"
	migrationMigrateUsageIdentitiesMetadata         = "20260504_migrate_usage_identities_metadata"
	migrationBackfillUsageEventIdentityFields       = "20260504_backfill_usage_event_identity_fields"
	migrationBackfillUsageIdentityStats             = "20260504_backfill_usage_identity_stats"
	migrationDropLegacyMetadataTables               = "20260504_drop_legacy_metadata_tables"
	migrationRemovePrefixUsageIdentities            = "20260504_remove_prefix_usage_identities"
	migrationAddUsageIdentityLookupKey              = "20260505_add_usage_identity_lookup_key"
	migrationMigrateAIProviderIdentitiesToAuthIndex = "20260505_migrate_ai_provider_identities_to_auth_index"
	migrationAddUsagePerformanceIndexes             = "20260506_add_usage_performance_indexes"
	migrationAddUsageIdentityMetadataFields         = "20260507_add_usage_identity_metadata_fields"
	migrationAddUsageEventModelAlias                = "20260508_add_usage_event_model_alias"
	migrationUpdateUsageIdentityQuotaFields         = "20260509_update_usage_identity_quota_fields"
	migrationRemoveUsageIdentityQuotaFields         = "20260510_remove_usage_identity_quota_fields"
	migrationAddUsageIdentityBaseURL                = "20260511_add_usage_identity_base_url"
	migrationNormalizeStorageTimesToProjectTZ       = "20260512_normalize_storage_times_to_project_tz"
	migrationUseInt64PrimaryKeys                    = "20260513_use_int64_primary_keys"
	migrationCreateCPAAPIKeys                       = "20260513_create_cpa_api_keys"
	migrationAddUsageEventCacheTokenFields          = "20260514_add_usage_event_cache_token_fields"
	migrationAddUsageEventPlainDimensionIndexes     = "20260514_add_usage_event_plain_dimension_indexes"
	migrationCreateUsageOverviewStats               = "20260514_create_usage_overview_stats"
	migrationRemoveUsageEventEventKeyUniqueIndex    = "20260514_remove_usage_event_event_key_unique_index"
	migrationAddUsageIdentitySyncMetadataFields     = "20260517_add_usage_identity_sync_metadata_fields"
	migrationUsageOverviewRollupDimensions          = "20260518_usage_overview_rollup_dimensions"
	migrationAddUsageEventReasoningEffort           = "20260519_add_usage_event_reasoning_effort"
	migrationAddUsageEventQuotaWindowIndexes        = "20260525_add_usage_event_quota_window_indexes"
	migrationAddUsageEventCPAResponseFields         = "20260528_add_usage_event_cpa_response_fields"
	migrationModelPricePricingStyle                 = "20260531_model_price_pricing_style"
	migrationBackfillClaudeUsageTokens              = "20260601_backfill_claude_usage_tokens"
	migrationAddUsageEventExecutorType              = "20260602_add_usage_event_executor_type"
	migrationAddUsageIdentityFileFields             = "20260603_add_usage_identity_file_fields"
	migrationBackfillGeminiCodexTokenFormat         = "20260605_backfill_gemini_codex_token_format"
	migrationRemoveUsageEventWriteHeavyIndexes      = "20260610_remove_usage_event_write_heavy_indexes"
	migrationRemoveUsageEventLowValueIndexes        = "20260611_remove_usage_event_low_value_indexes"
	migrationReplaceRedisInboxQueueKeyWithSource    = "20260612_replace_redis_inbox_queue_key_with_source"
	migrationCreateAuthSessions                     = "20260620_create_auth_sessions"
	migrationAddUsageIdentityAlias                  = "20260629_add_usage_identity_alias"
	migrationAddAuthSessionSource                   = "20260701_add_auth_session_source"
	migrationModelPriceMultiplier                   = "20260702_model_price_multiplier"
	migrationCreateAppSettings                      = "20260702_create_app_settings"
	migrationBackfillCacheReadTokens                = "20260710_backfill_cache_read_tokens"
	migrationAddUsageIdentityXAIUserID              = "20260711_add_usage_identity_xai_user_id"
	migrationAddUsageEventResponseServiceTier       = "20260715_add_usage_event_response_service_tier"
	migrationAddUsageEventGenerate                  = "20260715_add_usage_event_generate"
	// migrationUsageActivityStats 创建统一 Activity 并在回填完成后删除旧 Health 表。
	migrationUsageActivityStats = "20260719_usage_activity_stats"
	// migrationAlignUsageActivityShort 把已部署 short 行切换到本地自然日边界。
	migrationAlignUsageActivityShort = "20260722_align_usage_activity_short"
	// migrationUsageOverviewFiveDimensions 从现存 raw events 重建五维 hourly/daily rollup。
	migrationUsageOverviewFiveDimensions = "20260723_usage_overview_five_dimensions"
	// migrationModelPriceRules 创建每模型精确字段倍率规则表。
	migrationModelPriceRules = "20260723_model_price_rules"
	// migrationUsageAggregationCheckpoints 原子合并 Overview/Activity 两张旧水位表。
	migrationUsageAggregationCheckpoints = "20260726_usage_aggregation_checkpoints"
	// migrationUsageLatencyStats 用可恢复短事务回填 Latency hour/day 单表。
	migrationUsageLatencyStats = "20260726_usage_latency_stats"
	// migrationAddUsageEventClientMetadata 保存 CPA 新增的客户端请求元数据，历史行保持 NULL。
	migrationAddUsageEventClientMetadata = "20260729_add_usage_event_client_metadata"
	// migrationCreateUsageEventArchive 创建永久冷表；运行期归档在 schema 完成后才会启动。
	migrationCreateUsageEventArchive = "20260730_create_usage_event_archive"
	// migrationLocalRankingStats 创建固定四周期的本地排行累计。
	migrationLocalRankingStats = "20260731_local_ranking_stats"
	// migrationAddCPAAPIKeyLocalRankingAvatar 保存可空的本地排行头像覆盖值。
	migrationAddCPAAPIKeyLocalRankingAvatar = "20260803_add_cpa_api_key_local_ranking_avatar"
	// migrationAddAuthSessionClientMetadata 保存会话客户端与最近活动信息，旧会话只回填活动时间。
	migrationAddAuthSessionClientMetadata = "20260813_add_auth_session_client_metadata"
	// migrationCreateErrorEvents 创建 CPA errors 订阅的独立最终事件表。
	migrationCreateErrorEvents = "20260820_create_error_events"
	// migrationCodexQuotaHistory 创建 Codex 主额度周期父表和整数百分比状态子表。
	migrationCodexQuotaHistory = "20260820_codex_quota_history"
	// migrationAddCPAAPIKeySource 区分 CPA 核心 key 与 key-policy 插件 key，存量行回填为 cpa。
	migrationAddCPAAPIKeySource = "20260822_add_cpa_api_key_source"
)

type schemaMigration struct {
	Version   string    `gorm:"primaryKey;column:version"`
	AppliedAt time.Time `gorm:"serializer:storageTime;not null;column:applied_at"`
}

func (schemaMigration) TableName() string {
	return "schema_migrations"
}

type databaseMigration struct {
	version            string
	run                func(*gorm.DB) error
	disableTransaction bool
}

func Run(db *gorm.DB) error {
	if err := createSchemaMigrationsTable(db); err != nil {
		return err
	}

	for _, migration := range orderedMigrations() {
		if err := runSchemaMigration(db, migration); err != nil {
			return err
		}
	}
	return nil
}

func MarkAllAsApplied(db *gorm.DB) error {
	if err := createSchemaMigrationsTable(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		now := timeutil.NormalizeStorageTime(time.Now())
		for _, migration := range orderedMigrations() {
			if err := tx.Exec("INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, ?)", migration.version, now).Error; err != nil {
				return fmt.Errorf("mark schema migration %s applied: %w", migration.version, err)
			}
		}
		return nil
	})
}

func createSchemaMigrationsTable(db *gorm.DB) error {
	if err := db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

func orderedMigrations() []databaseMigration {
	return []databaseMigration{
		{version: migrationAddUsageEventRedisFields, run: addUsageEventRedisFieldsMigration},
		{version: migrationBackfillUsageEventRedisFields, run: backfillUsageEventRedisFieldsMigration},
		{version: migrationDropSnapshotRuns, run: dropSnapshotRunsMigration},
		{version: migrationDropLegacySnapshotRunColumns, run: dropLegacySnapshotRunColumnsMigration},
		{version: migrationCreateUsageIdentities, run: createUsageIdentitiesMigration},
		{version: migrationMigrateUsageIdentitiesMetadata, run: migrateUsageIdentitiesMetadataMigration},
		{version: migrationBackfillUsageEventIdentityFields, run: backfillUsageEventIdentityFieldsMigration},
		{version: migrationBackfillUsageIdentityStats, run: backfillUsageIdentityStatsMigration},
		{version: migrationDropLegacyMetadataTables, run: dropLegacyMetadataTablesMigration},
		{version: migrationRemovePrefixUsageIdentities, run: removePrefixUsageIdentitiesMigration},
		{version: migrationAddUsageIdentityLookupKey, run: addUsageIdentityLookupKeyMigration},
		{version: migrationMigrateAIProviderIdentitiesToAuthIndex, run: migrateAIProviderIdentitiesToAuthIndexMigration},
		{version: migrationAddUsagePerformanceIndexes, run: addUsagePerformanceIndexesMigration},
		{version: migrationAddUsageIdentityMetadataFields, run: addUsageIdentityMetadataFieldsMigration},
		{version: migrationAddUsageEventModelAlias, run: addUsageEventModelAliasMigration},
		{version: migrationUpdateUsageIdentityQuotaFields, run: updateUsageIdentityQuotaFieldsMigration},
		{version: migrationRemoveUsageIdentityQuotaFields, run: removeUsageIdentityQuotaFieldsMigration},
		{version: migrationAddUsageIdentityBaseURL, run: addUsageIdentityBaseURLMigration},
		{version: migrationNormalizeStorageTimesToProjectTZ, run: normalizeStorageTimesToProjectTZMigration},
		{version: migrationUseInt64PrimaryKeys, run: useInt64PrimaryKeysMigration},
		{version: migrationCreateCPAAPIKeys, run: createCPAAPIKeysMigration},
		{version: migrationAddUsageEventCacheTokenFields, run: addUsageEventCacheTokenFieldsMigration},
		{version: migrationAddUsageEventPlainDimensionIndexes, run: addUsageEventPlainDimensionIndexesMigration},
		{version: migrationCreateUsageOverviewStats, run: createUsageOverviewStatsMigration},
		{version: migrationRemoveUsageEventEventKeyUniqueIndex, run: removeUsageEventEventKeyUniqueIndexMigration},
		{version: migrationAddUsageIdentitySyncMetadataFields, run: addUsageIdentitySyncMetadataFieldsMigration},
		{version: migrationUsageOverviewRollupDimensions, run: usageOverviewRollupDimensionsMigration, disableTransaction: true},
		{version: migrationAddUsageEventReasoningEffort, run: addUsageEventReasoningEffortMigration},
		{version: migrationAddUsageEventQuotaWindowIndexes, run: addUsageEventQuotaWindowIndexesMigration},
		{version: migrationAddUsageEventCPAResponseFields, run: addUsageEventCPAResponseFieldsMigration},
		{version: migrationModelPricePricingStyle, run: addModelPricePricingStyleMigration},
		{version: migrationBackfillClaudeUsageTokens, run: backfillClaudeUsageTokensMigration},
		{version: migrationAddUsageEventExecutorType, run: addUsageEventExecutorTypeMigration},
		{version: migrationAddUsageIdentityFileFields, run: addUsageIdentityFileFieldsMigration},
		{version: migrationBackfillGeminiCodexTokenFormat, run: backfillGeminiCodexTokenFormatMigration},
		{version: migrationRemoveUsageEventWriteHeavyIndexes, run: removeUsageEventWriteHeavyIndexesMigration},
		{version: migrationRemoveUsageEventLowValueIndexes, run: removeUsageEventLowValueIndexesMigration},
		{version: migrationReplaceRedisInboxQueueKeyWithSource, run: replaceRedisInboxQueueKeyWithSourceMigration},
		{version: migrationCreateAuthSessions, run: createAuthSessionsMigration},
		{version: migrationAddUsageIdentityAlias, run: addUsageIdentityAliasMigration},
		{version: migrationAddAuthSessionSource, run: addAuthSessionSourceMigration},
		{version: migrationModelPriceMultiplier, run: addModelPriceMultiplierMigration},
		{version: migrationCreateAppSettings, run: createAppSettingsMigration},
		{version: migrationBackfillCacheReadTokens, run: backfillCacheReadTokensMigration},
		{version: migrationAddUsageIdentityXAIUserID, run: addUsageIdentityXAIUserIDMigration},
		{version: migrationAddUsageEventResponseServiceTier, run: addUsageEventResponseServiceTierMigration},
		{version: migrationAddUsageEventGenerate, run: addUsageEventGenerateMigration},
		// Activity migration 自己管理 1000-event 小事务，外层不能再包一个长事务。
		{version: migrationUsageActivityStats, run: usageActivityStatsMigration, disableTransaction: true},
		// short 重建在默认事务内原子完成，失败时旧行和版本标记一起回滚。
		{version: migrationAlignUsageActivityShort, run: alignUsageActivityShortMigration},
		// 五维重建自己管理 schema/setup 与 1000-event 小事务，外层不能再包长事务。
		{version: migrationUsageOverviewFiveDimensions, run: usageOverviewFiveDimensionsMigration, disableTransaction: true},
		{version: migrationModelPriceRules, run: createModelPriceRulesMigration},
		// 通用水位建表、复制、验证和旧表删除必须由默认外层事务共同保护。
		{version: migrationUsageAggregationCheckpoints, run: usageAggregationCheckpointsMigration},
		// Latency 回填逐页提交，外层长事务会破坏断点续跑语义。
		{version: migrationUsageLatencyStats, run: usageLatencyStatsMigration, disableTransaction: true},
		{version: migrationAddUsageEventClientMetadata, run: addUsageEventClientMetadataMigration},
		{version: migrationCreateUsageEventArchive, run: createUsageEventArchiveMigration},
		{version: migrationLocalRankingStats, run: localRankingStatsMigration},
		{version: migrationAddCPAAPIKeyLocalRankingAvatar, run: addCPAAPIKeyLocalRankingAvatarMigration},
		{version: migrationAddAuthSessionClientMetadata, run: addAuthSessionClientMetadataMigration},
		// 已进入 main 的 Errors 最终表先按原顺序创建，不能因 quota 分支合并而改写已发布迁移序列。
		{version: migrationCreateErrorEvents, run: createErrorEventsMigration},
		// 新表 migration 使用默认单事务，schema 与版本标记必须一起提交或回滚。
		{version: migrationCodexQuotaHistory, run: createCodexQuotaHistoryMigration},
		// source 列只做加列与存量回填，默认单事务即可保证原子性。
		{version: migrationAddCPAAPIKeySource, run: addCPAAPIKeySourceMigration},
	}
}

func runSchemaMigration(db *gorm.DB, migration databaseMigration) error {
	if migration.disableTransaction {
		return runSchemaMigrationWithoutTransaction(db, migration)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return runSchemaMigrationBody(tx, migration)
	})
}

func runSchemaMigrationWithoutTransaction(db *gorm.DB, migration databaseMigration) error {
	return runSchemaMigrationBody(db, migration)
}

func runSchemaMigrationBody(db *gorm.DB, migration databaseMigration) error {
	logger := logrus.WithField("version", migration.version)
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", migration.version).Count(&count).Error; err != nil {
		logger.WithError(err).Error("schema migration failed")
		return fmt.Errorf("check schema migration %s: %w", migration.version, err)
	}
	if count > 0 {
		logger.Debug("schema migration skipped")
		return nil
	}
	logger.Info("schema migration started")
	if err := migration.run(db); err != nil {
		logger.WithError(err).Error("schema migration failed")
		return fmt.Errorf("run schema migration %s: %w", migration.version, err)
	}
	if err := db.Create(&schemaMigration{Version: migration.version, AppliedAt: timeutil.NormalizeStorageTime(time.Now())}).Error; err != nil {
		logger.WithError(err).Error("schema migration failed")
		return fmt.Errorf("record schema migration %s: %w", migration.version, err)
	}
	logger.Info("schema migration applied")
	return nil
}
