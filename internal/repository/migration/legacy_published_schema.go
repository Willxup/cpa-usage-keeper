package migration

import "time"

// 历史回放读取已发布列，费用字段只由后续新结构迁移追加。
const legacyUsageEventReplayColumns = "id, event_key, api_group_key, provider, endpoint, auth_type, request_id, session_id, parent_session_id, client_ip, x_forwarded_for, user_agent, model, model_alias, response_model, reasoning_effort, service_tier, response_service_tier, executor_type, timestamp, source, auth_index, failed, status_code, generate, stream, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, created_at"

// Latency 旧迁移只读取其已发布事件投影，不能跟随运行时费用投影扩展。
const legacyUsageLatencyEventProjectionColumns = "id, api_group_key, model, model_alias, auth_index, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, timestamp, failed, generate, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens"

// 以下结构冻结费用持久化改版前已发布迁移使用的 GORM schema；新增费用或价格分支列不得进入旧版本。

// legacyUsageOverviewHourlyStat 保持已发布旧迁移的物理列、索引及关联合同。
type legacyUsageOverviewHourlyStat struct {
	ID                  int64     `gorm:"primaryKey"`
	BucketStart         time.Time `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:1;index:idx_usage_overview_hourly_stats_bucket_start;index:idx_usage_overview_hourly_stats_api_bucket,priority:2;index:idx_usage_overview_hourly_stats_api_model_bucket,priority:3;index:idx_usage_overview_hourly_stats_auth_bucket,priority:2;index:idx_usage_overview_hourly_stats_model_alias_bucket,priority:2"`
	APIGroupKey         string    `gorm:"not null;uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:2;index:idx_usage_overview_hourly_stats_api_bucket,priority:1;index:idx_usage_overview_hourly_stats_api_model_bucket,priority:1"`
	Model               string    `gorm:"not null;uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:3;index:idx_usage_overview_hourly_stats_api_model_bucket,priority:2"`
	AuthIndex           string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:4;index:idx_usage_overview_hourly_stats_auth_bucket,priority:1"`
	ModelAlias          string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:5;index:idx_usage_overview_hourly_stats_model_alias_bucket,priority:1"`
	ServiceTier         string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:6"`
	ResponseServiceTier string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:7"`
	ReasoningEffort     string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:8"`
	Endpoint            string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:9"`
	ExecutorType        string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:10"`
	RequestCount        int64     `gorm:"not null;default:0"`
	SuccessCount        int64     `gorm:"not null;default:0"`
	FailureCount        int64     `gorm:"not null;default:0"`
	InputTokens         int64     `gorm:"not null;default:0"`
	OutputTokens        int64     `gorm:"not null;default:0"`
	ReasoningTokens     int64     `gorm:"not null;default:0"`
	CachedTokens        int64     `gorm:"not null;default:0"`
	CacheReadTokens     int64     `gorm:"not null;default:0"`
	CacheCreationTokens int64     `gorm:"not null;default:0"`
	TotalTokens         int64     `gorm:"not null;default:0"`
	CreatedAt           time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt           time.Time `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageOverviewHourlyStat) TableName() string { return "usage_overview_hourly_stats" }

// legacyUsageOverviewDailyStat 保持已发布旧迁移的物理列、索引及关联合同。
type legacyUsageOverviewDailyStat struct {
	ID                  int64     `gorm:"primaryKey"`
	BucketStart         time.Time `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:1;index:idx_usage_overview_daily_stats_bucket_start;index:idx_usage_overview_daily_stats_api_bucket,priority:2;index:idx_usage_overview_daily_stats_api_model_bucket,priority:3;index:idx_usage_overview_daily_stats_auth_bucket,priority:2;index:idx_usage_overview_daily_stats_model_alias_bucket,priority:2"`
	APIGroupKey         string    `gorm:"not null;uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:2;index:idx_usage_overview_daily_stats_api_bucket,priority:1;index:idx_usage_overview_daily_stats_api_model_bucket,priority:1"`
	Model               string    `gorm:"not null;uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:3;index:idx_usage_overview_daily_stats_api_model_bucket,priority:2"`
	AuthIndex           string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:4;index:idx_usage_overview_daily_stats_auth_bucket,priority:1"`
	ModelAlias          string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:5;index:idx_usage_overview_daily_stats_model_alias_bucket,priority:1"`
	ServiceTier         string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:6"`
	ResponseServiceTier string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:7"`
	ReasoningEffort     string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:8"`
	Endpoint            string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:9"`
	ExecutorType        string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:10"`
	RequestCount        int64     `gorm:"not null;default:0"`
	SuccessCount        int64     `gorm:"not null;default:0"`
	FailureCount        int64     `gorm:"not null;default:0"`
	InputTokens         int64     `gorm:"not null;default:0"`
	OutputTokens        int64     `gorm:"not null;default:0"`
	ReasoningTokens     int64     `gorm:"not null;default:0"`
	CachedTokens        int64     `gorm:"not null;default:0"`
	CacheReadTokens     int64     `gorm:"not null;default:0"`
	CacheCreationTokens int64     `gorm:"not null;default:0"`
	TotalTokens         int64     `gorm:"not null;default:0"`
	CreatedAt           time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt           time.Time `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageOverviewDailyStat) TableName() string { return "usage_overview_daily_stats" }

// legacyUsageEventArchive 保持已发布旧迁移的物理列、索引及关联合同。
type legacyUsageEventArchive struct {
	ID                  int64 `gorm:"primaryKey;autoIncrement:false"`
	EventKey            string
	APIGroupKey         string
	Provider            string  `gorm:"column:provider"`
	Endpoint            string  `gorm:"column:endpoint"`
	AuthType            string  `gorm:"column:auth_type"`
	RequestID           string  `gorm:"column:request_id"`
	SessionID           string  `gorm:"column:session_id"`
	ParentSessionID     string  `gorm:"column:parent_session_id"`
	ClientIP            *string `gorm:"column:client_ip"`
	XForwardedFor       *string `gorm:"column:x_forwarded_for"`
	UserAgent           *string `gorm:"column:user_agent"`
	Model               string
	ModelAlias          *string   `gorm:"column:model_alias"`
	ResponseModel       string    `gorm:"column:response_model;not null;default:''"`
	ReasoningEffort     string    `gorm:"column:reasoning_effort;not null;default:''"`
	ServiceTier         string    `gorm:"column:service_tier;not null;default:''"`
	ResponseServiceTier string    `gorm:"column:response_service_tier;not null;default:''"`
	ExecutorType        string    `gorm:"column:executor_type;not null;default:''"`
	Timestamp           time.Time `gorm:"serializer:storageTime"`
	Source              string
	AuthIndex           string
	Failed              bool
	StatusCode          *int  `gorm:"column:status_code"`
	Generate            *bool `gorm:"column:generate;not null;default:true"`
	Stream              *bool `gorm:"column:stream"`
	LatencyMS           int64
	TTFTMS              *int64 `gorm:"column:ttft_ms"`
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64 `gorm:"not null;default:0"`
	CacheCreationTokens int64 `gorm:"not null;default:0"`
	TotalTokens         int64
	CreatedAt           time.Time `gorm:"serializer:storageTime"`
}

func (legacyUsageEventArchive) TableName() string { return "usage_events_archive" }

// legacyModelPriceSetting 保持已发布旧迁移的物理列、索引及关联合同。
type legacyModelPriceSetting struct {
	ID                   int64  `gorm:"primaryKey"`
	Model                string `gorm:"uniqueIndex:uniq_model_price_settings_model"`
	PricingStyle         string `gorm:"not null;default:openai"`
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CacheReadPricePer1M  float64
	CacheWritePricePer1M float64   `gorm:"column:cache_creation_price_per1_m;not null;default:0"`
	PriceMultiplier      *float64  `gorm:"not null;default:1"`
	CreatedAt            time.Time `gorm:"serializer:storageTime"`
	UpdatedAt            time.Time `gorm:"serializer:storageTime"`
}

func (legacyModelPriceSetting) TableName() string { return "model_price_settings" }

// legacyModelPriceRule 保持已发布旧迁移的物理列、索引及关联合同。
type legacyModelPriceRule struct {
	ID                  int64                   `gorm:"primaryKey"`
	ModelPriceSettingID int64                   `gorm:"not null;uniqueIndex:uniq_model_price_rules_identity,priority:1"`
	ModelPriceSetting   legacyModelPriceSetting `gorm:"foreignKey:ModelPriceSettingID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Key                 string                  `gorm:"not null;uniqueIndex:uniq_model_price_rules_identity,priority:2"`
	Value               string                  `gorm:"not null;uniqueIndex:uniq_model_price_rules_identity,priority:3"`
	Multiplier          float64                 `gorm:"not null;default:1"`
	CreatedAt           time.Time               `gorm:"serializer:storageTime"`
	UpdatedAt           time.Time               `gorm:"serializer:storageTime"`
}

func (legacyModelPriceRule) TableName() string { return "model_price_rules" }

// legacyUsageOverviewAggregationCheckpoint 保持旧 Overview 水位表结构。
type legacyUsageOverviewAggregationCheckpoint struct {
	ID                         int64      `gorm:"primaryKey"`
	Name                       string     `gorm:"not null;uniqueIndex:uniq_usage_overview_aggregation_checkpoints_name"`
	LastAggregatedUsageEventID int64      `gorm:"not null;default:0"`
	StatsUpdatedAt             *time.Time `gorm:"serializer:storageTime"`
	CreatedAt                  time.Time  `gorm:"serializer:storageTime;not null"`
	UpdatedAt                  time.Time  `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageOverviewAggregationCheckpoint) TableName() string {
	return "usage_overview_aggregation_checkpoints"
}
