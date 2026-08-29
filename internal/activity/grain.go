package activity

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
)

// NormalizeAPIGroupKey 复用现有 Overview 对空 API group 维度的稳定命名。
func NormalizeAPIGroupKey(value string) string {
	// 先移除 CPA 数据中可能存在的首尾空白。
	trimmed := strings.TrimSpace(value)
	// 空维度统一归入 unknown，避免同一含义生成多个聚合 key。
	if trimmed == "" {
		return "unknown"
	}
	// 非空维度保留原始大小写，只返回清理后的 key。
	return trimmed
}

// HeatmapBlocks 固定两张 Activity 热力图都返回 7×52 个格子。
const HeatmapBlocks = 364

const (
	// mediumWindowSeconds 固定 medium 的完整窗口为 7 天。
	mediumWindowSeconds int64 = 7 * 24 * 60 * 60
	// longWindowSeconds 固定 long 的完整窗口为 30 天。
	longWindowSeconds int64 = 30 * 24 * 60 * 60
)

// Bucket 是 Activity 表、migration 和响应共同使用的真实半开时间边界。
type Bucket struct {
	// Start 是半开区间包含的起点。
	Start time.Time
	// End 是半开区间不包含的终点。
	End time.Time
}

// BucketForTimestamp 返回 timestamp 所属的唯一 Activity bucket。
func BucketForTimestamp(grain entities.UsageActivityGrain, timestamp time.Time) (Bucket, error) {
	// short 必须与自然日展示共用从本地零点开始的 364 个边界。
	if grain == entities.UsageActivityGrainShort {
		return shortBucketForTimestamp(timestamp), nil
	}
	// daily 必须按项目时区的自然日处理，不能套用固定秒数公式。
	if grain == entities.UsageActivityGrainDaily {
		// 先把输入时间归一化到项目存储时区，确保日期维度一致。
		normalized := timeutil.NormalizeStorageTime(timestamp)
		// 当前本地零点是 daily bucket 的真实起点。
		start := time.Date(normalized.Year(), normalized.Month(), normalized.Day(), 0, 0, 0, 0, normalized.Location())
		// 使用 AddDate 找下一本地零点，以保留 DST 的 23h/25h 自然日。
		return Bucket{Start: start, End: start.AddDate(0, 0, 1)}, nil
	}

	// 固定窗口 grain 先解析出完整 364 格对应的总秒数。
	windowSeconds, err := fixedWindowSeconds(grain)
	// 非法 grain 立即返回，避免写出无法查询的 Activity 行。
	if err != nil {
		return Bucket{}, err
	}
	// 通过全局稳定公式定位 timestamp 所属的唯一整数 bucket index。
	index := fixedBucketIndex(timestamp, windowSeconds)
	// 根据同一 index 公式返回真实 start/end，供存储和查询共同使用。
	return fixedBucket(windowSeconds, index), nil
}

// WindowEndingAt 返回覆盖 referenceEnd 的最后 364 个连续 bucket。
func WindowEndingAt(grain entities.UsageActivityGrain, referenceEnd time.Time) ([]Bucket, error) {
	// short 从包含 referenceEnd 的自然日格向前读取连续 364 个真实存储桶。
	if grain == entities.UsageActivityGrainShort {
		return shortWindowEndingAt(referenceEnd), nil
	}
	// daily 窗口由连续本地自然日组成，单独保留 DST 语义。
	if grain == entities.UsageActivityGrainDaily {
		// 把查询参考时间归一化到项目时区。
		normalized := timeutil.NormalizeStorageTime(referenceEnd)
		// 先得到 referenceEnd 当天的本地零点候选边界。
		end := time.Date(normalized.Year(), normalized.Month(), normalized.Day(), 0, 0, 0, 0, normalized.Location())
		// referenceEnd 不在零点时向上对齐到下一本地零点。
		if !normalized.Equal(end) {
			end = end.AddDate(0, 0, 1)
		}
		// 固定分配 364 个返回槽位，禁止调用方减少热力图格子。
		buckets := make([]Bucket, HeatmapBlocks)
		// 从对齐终点向前退 364 个自然日，得到第一个 bucket 起点。
		start := end.AddDate(0, 0, -HeatmapBlocks)
		// 逐日生成真实边界，避免用固定 24 小时累加造成 DST 漂移。
		for index := range buckets {
			// 当前 offset 对应的本地零点是该格起点。
			bucketStart := start.AddDate(0, 0, index)
			// 下一本地零点是该格终点。
			buckets[index] = Bucket{Start: bucketStart, End: bucketStart.AddDate(0, 0, 1)}
		}
		// 返回按时间升序排列的 364 个 daily buckets。
		return buckets, nil
	}

	// 固定窗口 grain 先解析完整窗口秒数。
	windowSeconds, err := fixedWindowSeconds(grain)
	// 非法 grain 不允许继续生成伪边界。
	if err != nil {
		return nil, err
	}
	// 先定位 referenceEnd 当前落入的 bucket index。
	endIndex := fixedBucketIndex(referenceEnd, windowSeconds)
	// 读取包含 referenceEnd 的 bucket，以判断 referenceEnd 是否恰好位于边界。
	containing := fixedBucket(windowSeconds, endIndex)
	// 非精确边界需要向上对齐；精确边界直接作为窗口终点。
	if !referenceEnd.Equal(containing.Start) {
		endIndex++
	}

	// 固定分配 364 个返回槽位，保持所有范围的格子数一致。
	buckets := make([]Bucket, HeatmapBlocks)
	// 终点 index 不属于半开窗口，因此第一个 index 正好向前移动 364。
	startIndex := endIndex - HeatmapBlocks
	// 复用存储边界公式逐格生成，保证查询与增量聚合没有独立切桶逻辑。
	for offset := range buckets {
		// offset 始终按时间升序映射到连续 bucket index。
		buckets[offset] = fixedBucket(windowSeconds, startIndex+int64(offset))
	}
	// 返回连续、无重叠、无空隙的 364 个固定窗口 buckets。
	return buckets, nil
}

func fixedWindowSeconds(grain entities.UsageActivityGrain) (int64, error) {
	// 每个合法 grain 只映射到一套固定窗口秒数。
	switch grain {
	case entities.UsageActivityGrainMedium:
		// medium 的 364 个 buckets 合计必须正好覆盖 7 天。
		return mediumWindowSeconds, nil
	case entities.UsageActivityGrainLong:
		// long 的 364 个 buckets 合计必须正好覆盖 30 天。
		return longWindowSeconds, nil
	default:
		// short 与 daily 已在调用入口单独处理，其余值一律视为非法。
		return 0, fmt.Errorf("unsupported usage activity grain %q", grain)
	}
}

func shortBucketForTimestamp(timestamp time.Time) Bucket {
	// 每个 short bucket 都从 timestamp 所在本地自然日的零点重新编号。
	normalized := timeutil.NormalizeStorageTime(timestamp)
	dayStart := time.Date(normalized.Year(), normalized.Month(), normalized.Day(), 0, 0, 0, 0, normalized.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)
	windowSeconds := dayEnd.Unix() - dayStart.Unix()
	index := floorDiv((normalized.Unix()-dayStart.Unix())*HeatmapBlocks, windowSeconds)
	// floor 边界并非等宽，边界时刻需要用真实 start/end 修正一次估算。
	for index+1 < HeatmapBlocks && !normalized.Before(calendarBoundary(dayStart, windowSeconds, index+1)) {
		index++
	}
	for index > 0 && normalized.Before(calendarBoundary(dayStart, windowSeconds, index)) {
		index--
	}
	return calendarBucket(dayStart, windowSeconds, index)
}

func shortWindowEndingAt(referenceEnd time.Time) []Bucket {
	// 非边界时刻包含当前格；精确边界则把该边界作为半开窗口终点。
	containing := shortBucketForTimestamp(referenceEnd)
	cursor := containing.Start
	if !referenceEnd.Equal(containing.Start) {
		cursor = containing.End
	}
	// 逐格向前走能自然跨越本地零点和 DST，避免维护第二套全局 short index。
	buckets := make([]Bucket, HeatmapBlocks)
	for index := len(buckets) - 1; index >= 0; index-- {
		bucket := shortBucketForTimestamp(cursor.Add(-time.Nanosecond))
		buckets[index] = bucket
		cursor = bucket.Start
	}
	return buckets
}

func calendarBucket(dayStart time.Time, windowSeconds, index int64) Bucket {
	return Bucket{
		Start: calendarBoundary(dayStart, windowSeconds, index),
		End:   calendarBoundary(dayStart, windowSeconds, index+1),
	}
}

func calendarBoundary(dayStart time.Time, windowSeconds, index int64) time.Time {
	seconds := floorDiv(index*windowSeconds, HeatmapBlocks)
	return time.Unix(dayStart.Unix()+seconds, 0).In(dayStart.Location())
}

// fixedBucketIndex 先用反向比例估算，再按真实整数边界修正舍入误差。
func fixedBucketIndex(timestamp time.Time, windowSeconds int64) int64 {
	// 用反比例做 O(1) 初始估算，floorDiv 同时覆盖 epoch 之前的负时间。
	index := floorDiv(timestamp.Unix()*HeatmapBlocks, windowSeconds)
	// 若整数取整让估算落在过早的 bucket，就向后修正到真实半开区间。
	for !timestamp.Before(fixedBoundary(windowSeconds, index+1)) {
		index++
	}
	// 若估算越过 timestamp，就向前修正到 start <= timestamp 的 bucket。
	for timestamp.Before(fixedBoundary(windowSeconds, index)) {
		index--
	}
	// 返回唯一满足 start <= timestamp < end 的 index。
	return index
}

func fixedBucket(windowSeconds, index int64) Bucket {
	// start 和 end 必须分别使用相邻整数 index，禁止用近似桶宽相加。
	return Bucket{
		// 当前 index 的全局边界是半开区间起点。
		Start: fixedBoundary(windowSeconds, index),
		// 下一 index 的全局边界是半开区间终点。
		End: fixedBoundary(windowSeconds, index+1),
	}
}

func fixedBoundary(windowSeconds, index int64) time.Time {
	// floor(index×window/364) 让 364 个整数秒 bucket 精确回到完整窗口长度。
	seconds := floorDiv(index*windowSeconds, HeatmapBlocks)
	// 固定窗口以 Unix epoch 为全局锚点，避免项目时区或进程重启改变归桶。
	return time.Unix(seconds, 0).UTC()
}

func floorDiv(value, divisor int64) int64 {
	// Go 的整数除法向零截断，先取得默认商。
	quotient := value / divisor
	// 负余数表示向零截断比数学 floor 大 1，需要再向负方向修正。
	if remainder := value % divisor; remainder < 0 {
		quotient--
	}
	// 返回对正负时间都稳定的数学 floor 除法结果。
	return quotient
}
