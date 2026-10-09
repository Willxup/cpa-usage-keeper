package overview

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
)

// BucketKey 是 Overview 小时和自然日共用的最终十维唯一键。
type BucketKey struct {
	BucketStart         time.Time
	APIGroupKey         string
	Model               string
	AuthIndex           string
	ModelAlias          string
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
}

// BuildRows 使用最终唯一键把已计价事件聚合成 hourly 和 daily rows；费用未回填时拒绝推进普通水位。
// 费用、请求数和 Token 按已存事实累加，不再计价或归一化；两种速度逐请求计算后分别累计。
func BuildRows(events []entities.UsageEvent) ([]entities.UsageOverviewHourlyStat, []entities.UsageOverviewDailyStat, int64, error) {
	// 两个 map 都直接使用数据库最终唯一键，迁移与运行时不会产生不同分组。
	hourly := make(map[BucketKey]*entities.UsageOverviewHourlyStat)
	daily := make(map[BucketKey]*entities.UsageOverviewDailyStat)
	maxEventID := int64(0)

	for _, event := range events {
		if event.CostUSD == nil || event.CostAvailable == nil {
			return nil, nil, 0, fmt.Errorf("usage event %d cost_usd/cost_available is not backfilled", event.ID)
		}
		if math.IsNaN(*event.CostUSD) || math.IsInf(*event.CostUSD, 0) {
			return nil, nil, 0, fmt.Errorf("usage event %d cost_usd is not finite", event.ID)
		}
		// checkpoint 只推进到当前 batch 实际构建完成的最大事件 ID。
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
		hourKey, dayKey := BucketKeysForEvent(event)

		// 第一次遇到最终唯一键时创建维度完整的稀疏行。
		if hourly[hourKey] == nil {
			zeroCost, zeroUnavailable := 0.0, int64(0)
			hourly[hourKey] = &entities.UsageOverviewHourlyStat{
				BucketStart: hourKey.BucketStart, APIGroupKey: hourKey.APIGroupKey, Model: hourKey.Model,
				AuthIndex: hourKey.AuthIndex, ModelAlias: hourKey.ModelAlias, ServiceTier: hourKey.ServiceTier,
				ResponseServiceTier: hourKey.ResponseServiceTier, ReasoningEffort: hourKey.ReasoningEffort,
				Endpoint: hourKey.Endpoint, ExecutorType: hourKey.ExecutorType,
				CostUSD: &zeroCost, UnavailableCostCount: &zeroUnavailable,
			}
		}
		if daily[dayKey] == nil {
			zeroCost, zeroUnavailable := 0.0, int64(0)
			daily[dayKey] = &entities.UsageOverviewDailyStat{
				BucketStart: dayKey.BucketStart, APIGroupKey: dayKey.APIGroupKey, Model: dayKey.Model,
				AuthIndex: dayKey.AuthIndex, ModelAlias: dayKey.ModelAlias, ServiceTier: dayKey.ServiceTier,
				ResponseServiceTier: dayKey.ResponseServiceTier, ReasoningEffort: dayKey.ReasoningEffort,
				Endpoint: dayKey.Endpoint, ExecutorType: dayKey.ExecutorType,
				CostUSD: &zeroCost, UnavailableCostCount: &zeroUnavailable,
			}
		}
		addEventToHourlyRow(hourly[hourKey], event)
		addEventToDailyRow(daily[dayKey], event)
		// 与请求日志相同：失败、非流式或缺价不影响速度资格；旧 TTFT 缺失只排除解码速度。
		// 先转 float64 再乘 1000，避免大 Token 数整数乘法溢出；同一事件只计算一次。
		if event.OutputTokens > 0 && event.LatencyMS > 0 {
			speed := float64(event.OutputTokens) * 1000 / float64(event.LatencyMS)
			hourly[hourKey].SpeedTPSSum += speed
			hourly[hourKey].SpeedSampleCount++
			daily[dayKey].SpeedTPSSum += speed
			daily[dayKey].SpeedSampleCount++
			if event.TTFTMS != nil && *event.TTFTMS > 0 && *event.TTFTMS < event.LatencyMS {
				// 保留完整输出 Token；这里不是 (N-1)/(T-TTFT) 的 TPOT 倒数。
				decodeSpeed := float64(event.OutputTokens) * 1000 / float64(event.LatencyMS-*event.TTFTMS)
				hourly[hourKey].DecodeSpeedTPSSum += decodeSpeed
				hourly[hourKey].DecodeSpeedSampleCount++
				daily[dayKey].DecodeSpeedTPSSum += decodeSpeed
				daily[dayKey].DecodeSpeedSampleCount++
			}
		}
	}

	// map 转切片后固定写入顺序，让迁移重跑和故障定位保持稳定。
	hourlyRows := make([]entities.UsageOverviewHourlyStat, 0, len(hourly))
	for _, row := range hourly {
		hourlyRows = append(hourlyRows, *row)
	}
	dailyRows := make([]entities.UsageOverviewDailyStat, 0, len(daily))
	for _, row := range daily {
		dailyRows = append(dailyRows, *row)
	}
	sort.Slice(hourlyRows, func(left, right int) bool {
		return hourlyRowLess(hourlyRows[left], hourlyRows[right])
	})
	sort.Slice(dailyRows, func(left, right int) bool {
		return dailyRowLess(dailyRows[left], dailyRows[right])
	})
	return hourlyRows, dailyRows, maxEventID, nil
}

// BucketKeysForEvent 供正常聚合和显式费用重算共用维度规范化与项目时区分桶。
// 小时按绝对整小时截断，自然日按部署时区零点生成；不会修改原事件。
func BucketKeysForEvent(event entities.UsageEvent) (BucketKey, BucketKey) {
	dimensions := BucketKey{
		APIGroupKey:         normalizeRequiredDimension(event.APIGroupKey),
		Model:               normalizeRequiredDimension(event.Model),
		AuthIndex:           normalizeOptionalDimension(event.AuthIndex),
		ServiceTier:         normalizeOptionalDimension(event.ServiceTier),
		ResponseServiceTier: normalizeOptionalDimension(event.ResponseServiceTier),
		ReasoningEffort:     normalizeOptionalDimension(event.ReasoningEffort),
		Endpoint:            normalizeOptionalDimension(event.Endpoint),
		ExecutorType:        normalizeOptionalDimension(event.ExecutorType),
	}
	if event.ModelAlias != nil {
		dimensions.ModelAlias = normalizeOptionalDimension(*event.ModelAlias)
	}
	timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
	hourKey := dimensions
	hourKey.BucketStart = timestamp.Truncate(time.Hour)
	dayKey := dimensions
	dayKey.BucketStart = time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, timestamp.Location())
	return hourKey, dayKey
}

func normalizeRequiredDimension(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func normalizeOptionalDimension(value string) string {
	return strings.TrimSpace(value)
}

// addEventToHourlyRow 将一条事件的已存费用和原事实同时计入对应小时桶。
func addEventToHourlyRow(row *entities.UsageOverviewHourlyStat, event entities.UsageEvent) {
	addEventFee(row.CostUSD, row.UnavailableCostCount, event)
	row.RequestCount++
	if event.Failed {
		row.FailureCount++
	} else {
		row.SuccessCount++
	}
	row.InputTokens += event.InputTokens
	row.OutputTokens += event.OutputTokens
	row.ReasoningTokens += event.ReasoningTokens
	row.CachedTokens += event.CachedTokens
	row.CacheReadTokens += event.CacheReadTokens
	row.CacheCreationTokens += event.CacheCreationTokens
	row.TotalTokens += event.TotalTokens
}

// addEventToDailyRow 与小时桶使用相同的已存费用和原 Token 口径，不再次归一化。
func addEventToDailyRow(row *entities.UsageOverviewDailyStat, event entities.UsageEvent) {
	addEventFee(row.CostUSD, row.UnavailableCostCount, event)
	row.RequestCount++
	if event.Failed {
		row.FailureCount++
	} else {
		row.SuccessCount++
	}
	row.InputTokens += event.InputTokens
	row.OutputTokens += event.OutputTokens
	row.ReasoningTokens += event.ReasoningTokens
	row.CachedTokens += event.CachedTokens
	row.CacheReadTokens += event.CacheReadTokens
	row.CacheCreationTokens += event.CacheCreationTokens
	row.TotalTokens += event.TotalTokens
}

// addEventFee 只累加事件已存金额；明确缺价事件贡献零金额和一个不可用计数。
func addEventFee(cost *float64, unavailable *int64, event entities.UsageEvent) {
	*cost += *event.CostUSD
	if !*event.CostAvailable {
		*unavailable += 1
	}
}

func hourlyRowLess(left, right entities.UsageOverviewHourlyStat) bool {
	return dimensionsLess(
		BucketKey{left.BucketStart, left.APIGroupKey, left.Model, left.AuthIndex, left.ModelAlias, left.ServiceTier, left.ResponseServiceTier, left.ReasoningEffort, left.Endpoint, left.ExecutorType},
		BucketKey{right.BucketStart, right.APIGroupKey, right.Model, right.AuthIndex, right.ModelAlias, right.ServiceTier, right.ResponseServiceTier, right.ReasoningEffort, right.Endpoint, right.ExecutorType},
	)
}

func dailyRowLess(left, right entities.UsageOverviewDailyStat) bool {
	return dimensionsLess(
		BucketKey{left.BucketStart, left.APIGroupKey, left.Model, left.AuthIndex, left.ModelAlias, left.ServiceTier, left.ResponseServiceTier, left.ReasoningEffort, left.Endpoint, left.ExecutorType},
		BucketKey{right.BucketStart, right.APIGroupKey, right.Model, right.AuthIndex, right.ModelAlias, right.ServiceTier, right.ResponseServiceTier, right.ReasoningEffort, right.Endpoint, right.ExecutorType},
	)
}

func dimensionsLess(left, right BucketKey) bool {
	if !left.BucketStart.Equal(right.BucketStart) {
		return left.BucketStart.Before(right.BucketStart)
	}
	leftValues := [...]string{left.APIGroupKey, left.Model, left.AuthIndex, left.ModelAlias, left.ServiceTier, left.ResponseServiceTier, left.ReasoningEffort, left.Endpoint, left.ExecutorType}
	rightValues := [...]string{right.APIGroupKey, right.Model, right.AuthIndex, right.ModelAlias, right.ServiceTier, right.ResponseServiceTier, right.ReasoningEffort, right.Endpoint, right.ExecutorType}
	for index := range leftValues {
		if leftValues[index] != rightValues[index] {
			return leftValues[index] < rightValues[index]
		}
	}
	return false
}
