package entities

import "time"

// UsageActivityGrain 标识 Activity 热力图使用的稳定时间粒度。
type UsageActivityGrain string

const (
	// UsageActivityGrainShort 对应从本地零点划分的 Day 视图细粒度桶。
	UsageActivityGrainShort UsageActivityGrain = "short"
	// UsageActivityGrainMedium 对应固定 7 天窗口。
	UsageActivityGrainMedium UsageActivityGrain = "medium"
	// UsageActivityGrainLong 对应固定 30 天窗口。
	UsageActivityGrainLong UsageActivityGrain = "long"
	// UsageActivityGrainDaily 对应项目时区内的本地自然日。
	UsageActivityGrainDaily UsageActivityGrain = "daily"
)

// UsageActivityStat 同时保存请求结果与 canonical Token 活动统计。
type UsageActivityStat struct {
	// ID 是 Activity 稀疏聚合行的自增主键。
	ID int64 `gorm:"primaryKey"`
	// Grain 区分 short、medium、long 和 daily 四套互不混用的边界。
	Grain UsageActivityGrain `gorm:"type:text;not null;check:chk_usage_activity_stats_grain,grain IN ('short','medium','long','daily');uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:1;index:idx_usage_activity_stats_api_grain_start,priority:2;index:idx_usage_activity_stats_grain_end,priority:1"`
	// BucketStart 保存半开区间的真实起点，查询端不得重新推算。
	BucketStart time.Time `gorm:"serializer:sortableTime;not null;check:chk_usage_activity_stats_bucket_bounds,bucket_start < bucket_end;uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:2;index:idx_usage_activity_stats_api_grain_start,priority:3"`
	// BucketEnd 保存半开区间的真实终点，并作为 retention cleanup 的时间列。
	BucketEnd time.Time `gorm:"serializer:sortableTime;not null;index:idx_usage_activity_stats_grain_end,priority:2"`
	// APIGroupKey 保持与现有 Overview/API Key 过滤完全相同的维度。
	APIGroupKey string `gorm:"not null;uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:3;index:idx_usage_activity_stats_api_grain_start,priority:1"`
	// SuccessCount 累计当前 bucket 内成功请求数。
	SuccessCount int64 `gorm:"not null;default:0"`
	// FailureCount 累计当前 bucket 内失败请求数。
	FailureCount int64 `gorm:"not null;default:0"`
	// InputTokens 累计 canonical input_tokens。
	InputTokens int64 `gorm:"not null;default:0"`
	// OutputTokens 累计 canonical output_tokens。
	OutputTokens int64 `gorm:"not null;default:0"`
	// ReasoningTokens 累计 canonical reasoning_tokens。
	ReasoningTokens int64 `gorm:"not null;default:0"`
	// CacheReadTokens 累计 canonical cache_read_tokens，不读取 cached_tokens。
	CacheReadTokens int64 `gorm:"not null;default:0"`
	// CacheCreationTokens 累计 canonical cache_creation_tokens。
	CacheCreationTokens int64 `gorm:"not null;default:0"`
	// TotalTokens 直接累计 usage event 的 canonical total_tokens。
	TotalTokens int64 `gorm:"not null;default:0"`
	// CreatedAt 记录该稀疏聚合行首次创建时间。
	CreatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// UpdatedAt 记录该稀疏聚合行最近一次累计时间。
	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
}
