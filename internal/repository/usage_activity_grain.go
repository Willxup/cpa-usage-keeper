package repository

import (
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
)

// UsageActivityHeatmapBlocks 向 repository 调用方暴露统一的 7×52 格子数量。
const UsageActivityHeatmapBlocks = activity.HeatmapBlocks

// UsageActivityBucket 复用 activity 包的真实半开边界类型，不在 repository 复制字段。
type UsageActivityBucket = activity.Bucket

// UsageActivityBucketForTimestamp 把 repository 调用统一转发给唯一边界实现。
func UsageActivityBucketForTimestamp(grain entities.UsageActivityGrain, timestamp time.Time) (UsageActivityBucket, error) {
	// repository、migration 和后续查询都必须共享 activity.BucketForTimestamp。
	return activity.BucketForTimestamp(grain, timestamp)
}

// UsageActivityWindowEndingAt 把 repository 窗口查询统一转发给唯一边界实现。
func UsageActivityWindowEndingAt(grain entities.UsageActivityGrain, referenceEnd time.Time) ([]UsageActivityBucket, error) {
	// 薄适配层不得加入第二套对齐或切桶规则。
	return activity.WindowEndingAt(grain, referenceEnd)
}
