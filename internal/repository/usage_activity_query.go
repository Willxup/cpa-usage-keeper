package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// usageActivityGridRows 固定热力图纵向 7 行，不允许查询层减少格子数。
	usageActivityGridRows = 7
	// usageActivityGridColumns 固定热力图横向 52 列。
	usageActivityGridColumns = 52
)

// QueryUsageActivityGrid 按 grain 和参考结束时间返回补齐后的固定 364 个 Activity blocks。
func QueryUsageActivityGrid(ctx context.Context, db *gorm.DB, grain entities.UsageActivityGrain, referenceEnd time.Time, apiGroupKey string) (dto.UsageActivityGridRecord, error) {
	// result 先记录请求 grain，错误路径也能保留调用上下文。
	result := dto.UsageActivityGridRecord{Grain: grain}
	// nil 数据库无法读取 Activity rows。
	if db == nil {
		return result, fmt.Errorf("database is nil")
	}
	// nil context 统一降级为 Background，避免 GORM WithContext panic。
	if ctx == nil {
		ctx = context.Background()
	}
	// 查询窗口必须调用存储和 migration 共用的唯一边界 helper。
	buckets, err := UsageActivityWindowEndingAt(grain, timeutil.NormalizeStorageTime(referenceEnd))
	// 非法 grain 直接返回，不生成伪造网格。
	if err != nil {
		return result, err
	}
	// 固定窗口必须完整包含 364 个 bucket，避免后端变化导致前端格子缩水。
	if len(buckets) != UsageActivityHeatmapBlocks {
		return result, fmt.Errorf("usage activity window returned %d buckets, want %d", len(buckets), UsageActivityHeatmapBlocks)
	}
	// 填入固定 7×52 响应形状。
	result.Rows = usageActivityGridRows
	result.Columns = usageActivityGridColumns
	// 第一个 bucket 的起点是实际对齐窗口左边界。
	result.WindowStart = buckets[0].Start
	// 最后一个 bucket 的终点是实际对齐窗口右边界。
	result.WindowEnd = buckets[len(buckets)-1].End
	// blocks 先按唯一边界补齐全部空桶。
	result.Blocks = make([]dto.UsageActivityBlockRecord, len(buckets))
	// bucketIndex 用 Unix 秒把数据库稀疏行映射回固定槽位。
	bucketIndex := make(map[int64]int, len(buckets))
	// bucketStarts 保存本次允许读取的精确边界文本，避免 DST 回拨时按本地 RFC3339 文本范围漏行。
	bucketStarts := make([]string, len(buckets))
	// 每个 helper bucket 都写入一个连续响应块。
	for index, bucket := range buckets {
		// 空桶也必须返回真实 start/end，不能只返回有数据的行。
		result.Blocks[index] = dto.UsageActivityBlockRecord{StartTime: bucket.Start, EndTime: bucket.End}
		// 固定边界精度为秒，Unix key 与 SQLite storageTime 解析稳定对应。
		bucketIndex[bucket.Start.Unix()] = index
		// 查询值复用 Activity 可排序 UTC 格式，只匹配 helper 生成的 364 个真实起点。
		bucketStarts[index] = timeutil.FormatSortableStorageTime(bucket.Start)
		// 兼容 bucket_seconds 取当前窗口实际块宽的最大秒数，覆盖交替宽度。
		spanSeconds := int64(bucket.End.Sub(bucket.Start) / time.Second)
		// 更宽块更新兼容元数据值。
		if spanSeconds > result.BucketSeconds {
			result.BucketSeconds = spanSeconds
		}
	}

	// 查询只读取当前 grain 和 364 个精确起点；IN 不依赖带 offset 文本的字典序。
	query := db.WithContext(ctx).
		Where("grain = ? AND bucket_start IN ?", grain, bucketStarts)
	// API group 非空时保持现有 Overview/API Key 精确 scope。
	if normalizedAPIGroupKey := strings.TrimSpace(apiGroupKey); normalizedAPIGroupKey != "" {
		query = query.Where("api_group_key = ?", normalizedAPIGroupKey)
	}
	// 使用完整 entity 读取数据库保存的 bucket_end 和全部 canonical Activity 字段。
	var rows []entities.UsageActivityStat
	// 查询失败必须阻止返回半完整 Activity 网格。
	if err := query.Find(&rows).Error; err != nil {
		return result, fmt.Errorf("load usage activity grid rows: %w", err)
	}
	// 多 API group 无 scope 查询时在同一固定块内做内存求和，数据库返回顺序不影响结果。
	for _, row := range rows {
		// 行起点归一化后查找对应固定槽位。
		index, ok := bucketIndex[timeutil.NormalizeStorageTime(row.BucketStart).Unix()]
		// 不属于本次窗口的异常行不写入其它槽位。
		if !ok {
			continue
		}
		// block 指针用于逐字段累计同一 bucket 的多个 API group。
		block := &result.Blocks[index]
		// 有数据行时使用表中真实 bucket_end，满足查询端不反推边界的契约。
		block.EndTime = timeutil.NormalizeStorageTime(row.BucketEnd)
		// 成功请求数按 API group 行求和。
		block.SuccessCount += row.SuccessCount
		// 失败请求数按 API group 行求和。
		block.FailureCount += row.FailureCount
		// canonical input tokens 按行求和。
		block.InputTokens += row.InputTokens
		// canonical output tokens 按行求和。
		block.OutputTokens += row.OutputTokens
		// canonical reasoning tokens 按行求和。
		block.ReasoningTokens += row.ReasoningTokens
		// 只累计 cache_read_tokens，不读取或推导 cached_tokens。
		block.CacheReadTokens += row.CacheReadTokens
		// canonical cache creation tokens 按行求和。
		block.CacheCreationTokens += row.CacheCreationTokens
		// canonical total tokens 按行求和。
		block.TotalTokens += row.TotalTokens
	}
	// 返回固定形状和已经补零的 Activity 网格。
	return result, nil
}
