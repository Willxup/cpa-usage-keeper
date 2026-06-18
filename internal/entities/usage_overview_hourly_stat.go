package entities

import "time"

// UsageOverviewHourlyStat 是 Overview 页面按小时预聚合的 usage 统计。
type UsageOverviewHourlyStat struct {
	ID                  int64     `gorm:"primaryKey"`
	BucketStart         time.Time `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias,priority:1;index:idx_usage_overview_hourly_stats_bucket_start;index:idx_usage_overview_hourly_stats_api_bucket,priority:2;index:idx_usage_overview_hourly_stats_api_model_bucket,priority:3;index:idx_usage_overview_hourly_stats_auth_bucket,priority:2;index:idx_usage_overview_hourly_stats_model_alias_bucket,priority:2"`
	APIGroupKey         string    `gorm:"not null;uniqueIndex:uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias,priority:2;index:idx_usage_overview_hourly_stats_api_bucket,priority:1;index:idx_usage_overview_hourly_stats_api_model_bucket,priority:1"`
	Model               string    `gorm:"not null;uniqueIndex:uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias,priority:3;index:idx_usage_overview_hourly_stats_api_model_bucket,priority:2"`
	ServiceTier         string    `gorm:"not null;default:'default';uniqueIndex:uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias,priority:4"`
	AuthIndex           string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias,priority:5;index:idx_usage_overview_hourly_stats_auth_bucket,priority:1"`
	ModelAlias          string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias,priority:6;index:idx_usage_overview_hourly_stats_model_alias_bucket,priority:1"`
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
