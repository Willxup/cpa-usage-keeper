package migration

import "time"

// legacyUsageOverviewHealthStat 只供历史 migration 重放旧 Health schema。
type legacyUsageOverviewHealthStat struct {
	// ID 是旧 Health 行的自增主键。
	ID int64 `gorm:"primaryKey"`
	// BucketStart 是旧固定 span bucket 的起点。
	BucketStart time.Time `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_overview_health_stats_bucket_span_api,priority:1;index:idx_usage_overview_health_stats_bucket_start;index:idx_usage_overview_health_stats_api_bucket_span,priority:2"`
	// SpanSeconds 是旧表通过固定秒数表达粒度的字段。
	SpanSeconds int64 `gorm:"not null;uniqueIndex:uniq_usage_overview_health_stats_bucket_span_api,priority:2;index:idx_usage_overview_health_stats_api_bucket_span,priority:3"`
	// APIGroupKey 是旧 Health 表唯一保留的业务维度。
	APIGroupKey string `gorm:"not null;uniqueIndex:uniq_usage_overview_health_stats_bucket_span_api,priority:3;index:idx_usage_overview_health_stats_api_bucket_span,priority:1"`
	// SuccessCount 累计旧 bucket 的成功请求数。
	SuccessCount int64 `gorm:"not null;default:0"`
	// FailureCount 累计旧 bucket 的失败请求数。
	FailureCount int64 `gorm:"not null;default:0"`
	// CreatedAt 记录旧 Health 行创建时间。
	CreatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// UpdatedAt 记录旧 Health 行更新时间。
	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
}

// TableName 固定映射历史表名，避免结构体重命名改变 migration 行为。
func (legacyUsageOverviewHealthStat) TableName() string {
	// 历史 migration 和最终删除步骤都必须指向原表。
	return "usage_overview_health_stats"
}
