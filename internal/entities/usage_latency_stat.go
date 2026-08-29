package entities

import "time"

// UsageLatencyBucketType 区分同一张表里的小时桶和自然日桶。
type UsageLatencyBucketType string

const (
	// UsageLatencyBucketHour 保存项目时区整点开始的一小时聚合。
	UsageLatencyBucketHour UsageLatencyBucketType = "hour"
	// UsageLatencyBucketDay 保存项目时区自然日零点开始的一天聚合。
	UsageLatencyBucketDay UsageLatencyBucketType = "day"
)

// UsageLatencyStat 保存可合并分位 Sketch、精确最大值和有界真实散点。
type UsageLatencyStat struct {
	ID            int64                  `gorm:"primaryKey"`
	BucketType    UsageLatencyBucketType `gorm:"type:text;not null;check:chk_usage_latency_stats_bucket_type,bucket_type IN ('hour','day');uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:1;index:idx_usage_latency_stats_type_start,priority:1;index:idx_usage_latency_stats_api_type_start,priority:2"`
	BucketStart   time.Time              `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:2;index:idx_usage_latency_stats_type_start,priority:2;index:idx_usage_latency_stats_api_type_start,priority:3"`
	APIGroupKey   string                 `gorm:"not null;uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:3;index:idx_usage_latency_stats_api_type_start,priority:1"`
	SampleCount   int64                  `gorm:"not null;default:0"`
	MaxTTFTMS     int64                  `gorm:"column:max_ttft_ms;not null;default:0"`
	MaxLatencyMS  int64                  `gorm:"not null;default:0"`
	FormatVersion int                    `gorm:"not null;default:1"`
	TTFTSketch    []byte                 `gorm:"type:blob;not null"`
	LatencySketch []byte                 `gorm:"type:blob;not null"`
	SamplePoints  []byte                 `gorm:"type:blob;not null"`
	CreatedAt     time.Time              `gorm:"serializer:storageTime;not null"`
	UpdatedAt     time.Time              `gorm:"serializer:storageTime;not null"`
}
