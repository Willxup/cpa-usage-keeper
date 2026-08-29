package entities

import "time"

// UsageActivityAggregationCheckpoint 记录 Activity 增量聚合已处理到的 usage_events cursor。
type UsageActivityAggregationCheckpoint struct {
	// ID 是 Activity checkpoint 行的自增主键。
	ID int64 `gorm:"primaryKey"`
	// Name 固定为 activity，并通过唯一索引保证只有一个 cursor。
	Name string `gorm:"not null;uniqueIndex:uniq_usage_activity_aggregation_checkpoints_name"`
	// LastAggregatedUsageEventID 记录已经完整提交到 Activity 的最大 usage event ID。
	LastAggregatedUsageEventID int64 `gorm:"not null;default:0"`
	// StatsUpdatedAt 记录最近一次真正推进 Activity cursor 的时间。
	StatsUpdatedAt *time.Time `gorm:"serializer:storageTime"`
	// CreatedAt 记录 checkpoint 首次创建时间。
	CreatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// UpdatedAt 记录 checkpoint 行最近一次更新时间。
	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
}
