package activity

import (
	"fmt"
	"sort"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
)

var grainRetentions = map[entities.UsageActivityGrain]time.Duration{
	// short 只保留最近 3 天的细粒度 Activity。
	entities.UsageActivityGrainShort: 3 * 24 * time.Hour,
	// medium 保留 8 天，以覆盖完整 7 天展示及边界余量。
	entities.UsageActivityGrainMedium: 8 * 24 * time.Hour,
	// long 保留 31 天，以覆盖完整 30 天展示及边界余量。
	entities.UsageActivityGrainLong: 31 * 24 * time.Hour,
}

var allGrains = []entities.UsageActivityGrain{
	// short 必须先于其它 grain 构建，保持调试输出稳定。
	entities.UsageActivityGrainShort,
	// medium 使用同一事件继续构建 7 天窗口。
	entities.UsageActivityGrainMedium,
	// long 使用同一事件继续构建 30 天窗口。
	entities.UsageActivityGrainLong,
	// daily 永久累计所有仍存在的 raw events。
	entities.UsageActivityGrainDaily,
}

type aggregateKey struct {
	// Grain 区分四套互不混用的边界。
	Grain entities.UsageActivityGrain
	// BucketStart 与 grain 一起唯一决定 bucket_end。
	BucketStart time.Time
	// APIGroupKey 保持现有 Overview 的过滤维度。
	APIGroupKey string
}

// Retention 返回短 grain 的固定保留期；daily 返回 limited=false。
func Retention(grain entities.UsageActivityGrain) (duration time.Duration, limited bool) {
	// map 中只有需要 cleanup 的 short、medium 和 long。
	duration, limited = grainRetentions[grain]
	// 调用方根据 limited 决定是否应用 cutoff。
	return duration, limited
}

// BuildRows 用唯一规则把 usage events 聚合成稀疏 Activity rows。
func BuildRows(events []entities.UsageEvent, now time.Time) ([]entities.UsageActivityStat, error) {
	// 统一 now 到项目存储时区，保证 migration 与运行时使用相同 cutoff。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// map key 与数据库唯一索引一致，先在内存合并相同 bucket。
	rowsByKey := make(map[aggregateKey]*entities.UsageActivityStat)
	// 每条事件都可能进入三个短 grain 和永久 daily。
	for _, event := range events {
		// 事件时间统一到项目存储时区后再判断 retention 和 daily。
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		// API group 复用统一规范化，避免迁移与运行时产生不同 key。
		apiGroupKey := NormalizeAPIGroupKey(event.APIGroupKey)
		// 固定顺序逐 grain 处理，禁止调用方各自选择字段或边界。
		for _, grain := range allGrains {
			// daily 不受 retention 限制；短 grain 跳过各自 cutoff 之前的迟到事件。
			if retention, limited := Retention(grain); limited && timestamp.Before(normalizedNow.Add(-retention)) {
				continue
			}
			// 所有调用方共享 BucketForTimestamp，不复制整数边界公式。
			bucket, err := BucketForTimestamp(grain, timestamp)
			if err != nil {
				return nil, fmt.Errorf("build usage activity %s bucket: %w", grain, err)
			}
			// 唯一 key 不包含 bucket_end，因为 grain/start 已唯一决定真实终点。
			key := aggregateKey{Grain: grain, BucketStart: bucket.Start, APIGroupKey: apiGroupKey}
			// 第一次遇到 key 时创建稀疏行并保存真实边界。
			row := rowsByKey[key]
			if row == nil {
				row = &entities.UsageActivityStat{
					Grain: grain, BucketStart: bucket.Start, BucketEnd: bucket.End, APIGroupKey: apiGroupKey,
				}
				rowsByKey[key] = row
			}
			// failed 是 Request Health 成功失败二选一的唯一来源。
			if event.Failed {
				row.FailureCount++
			} else {
				row.SuccessCount++
			}
			// Activity 只逐字段累计 canonical Token，不读取 cached_tokens。
			// input_tokens 只进入独立 InputTokens 列。
			row.InputTokens += event.InputTokens
			// output_tokens 只进入独立 OutputTokens 列。
			row.OutputTokens += event.OutputTokens
			// reasoning_tokens 只进入独立 ReasoningTokens 列。
			row.ReasoningTokens += event.ReasoningTokens
			// cache_read_tokens 是 Activity 唯一缓存读取来源。
			row.CacheReadTokens += event.CacheReadTokens
			// cache_creation_tokens 单独保存缓存创建量。
			row.CacheCreationTokens += event.CacheCreationTokens
			// total_tokens 保留 canonical 最终总量，不在 Activity 内重算。
			row.TotalTokens += event.TotalTokens
		}
	}

	// 把 map 转为值切片，避免写入层修改内存聚合指针。
	rows := make([]entities.UsageActivityStat, 0, len(rowsByKey))
	// 每个唯一 key 只追加一行。
	for _, row := range rowsByKey {
		rows = append(rows, *row)
	}
	// 稳定排序让 migration、运行时、测试和故障注入使用相同写入顺序。
	sort.Slice(rows, func(left, right int) bool {
		// grain 不同时先按固定字符串顺序排列。
		if rows[left].Grain != rows[right].Grain {
			return rows[left].Grain < rows[right].Grain
		}
		// 同 grain 先按真实 bucket_start 排列。
		if !rows[left].BucketStart.Equal(rows[right].BucketStart) {
			return rows[left].BucketStart.Before(rows[right].BucketStart)
		}
		// 同 bucket 最后按 API group 排列。
		return rows[left].APIGroupKey < rows[right].APIGroupKey
	})
	// 返回 migration 与运行时共同使用的最终稀疏 rows。
	return rows, nil
}
