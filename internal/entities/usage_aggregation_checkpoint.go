package entities

import "time"

// UsageAggregationCheckpointName 限定共享表中允许存在的三类全局聚合水位。
type UsageAggregationCheckpointName string

const (
	// UsageAggregationCheckpointOverview 对应 Overview hourly/daily 的已提交水位。
	UsageAggregationCheckpointOverview UsageAggregationCheckpointName = "overview"
	// UsageAggregationCheckpointActivity 对应 Activity 四种 grain 的已提交水位。
	UsageAggregationCheckpointActivity UsageAggregationCheckpointName = "activity"
	// UsageAggregationCheckpointLatency 对应 Latency hour/day 的已提交水位。
	UsageAggregationCheckpointLatency UsageAggregationCheckpointName = "latency"
	// UsageAggregationEventProjectionColumns 是运行时和 Latency migration 共用的事件读取契约。
	UsageAggregationEventProjectionColumns = "id, api_group_key, model, model_alias, auth_index, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, timestamp, failed, generate, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens"
)

// UsageAggregationCheckpoint 用一张表保存三个独立 cursor，避免同构 checkpoint 表继续增长。
type UsageAggregationCheckpoint struct {
	// Name 同时是业务类型和主键，每个聚合只能推进自己的行。
	Name UsageAggregationCheckpointName `gorm:"type:text;primaryKey;check:chk_usage_aggregation_checkpoints_name,name IN ('overview','activity','latency')"`
	// LastAggregatedUsageEventID 记录该类已经完整提交的最大 usage event ID。
	LastAggregatedUsageEventID int64 `gorm:"not null;default:0"`
	// StatsUpdatedAt 只在真正推进 cursor 时更新，空表读取不会写入该字段。
	StatsUpdatedAt *time.Time `gorm:"serializer:storageTime"`
	// CreatedAt 保留 checkpoint 行第一次建立的项目时区时间。
	CreatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// UpdatedAt 记录该行最近一次成功推进的项目时区时间。
	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
}
