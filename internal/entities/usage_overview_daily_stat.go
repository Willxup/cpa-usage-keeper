package entities

import "time"

// UsageOverviewDailyStat 是 Overview 页面按天预聚合的 usage 统计。
type UsageOverviewDailyStat struct {
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
