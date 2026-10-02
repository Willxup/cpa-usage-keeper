package repository

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const quotaWindowRawOnlyThreshold = 5 * time.Hour

type UsageWindowStats struct {
	Tokens        int64
	Cost          float64
	CostAvailable bool
}

// UsageWindowStatsGrouper 把真实模型映射到额度组；false 表示模型归属未知。
type UsageWindowStatsGrouper func(model string) (groupKey string, ok bool)

// UsageWindowGroupedStats 保留每个额度组的统计，并标记窗口内模型是否都能可靠归组。
type UsageWindowGroupedStats struct {
	Groups   map[string]UsageWindowStats
	Complete bool
}

type UsageWindowStatsCalculator struct {
	db *gorm.DB
}

type usageWindowStoredStats struct {
	Model                string  `gorm:"column:model"`
	TotalTokens          int64   `gorm:"column:total_tokens"`
	CostUSD              float64 `gorm:"column:cost_usd"`
	UnavailableCostCount int64   `gorm:"column:unavailable_cost_count"`
	UnbackfilledCount    int64   `gorm:"column:unbackfilled_count"`
	HasUsage             int64   `gorm:"column:has_usage"`
}

// 未知模型只在所有 canonical Token 字段均不为正时才可忽略；该证据不参与费用计算。
const usageWindowHasUsageProjection = "MAX(CASE WHEN total_tokens > 0 OR input_tokens > 0 OR output_tokens > 0 OR cache_read_tokens > 0 OR cache_creation_tokens > 0 THEN 1 ELSE 0 END) AS has_usage"

const usageWindowRawProjection = "model, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(cost_usd), 0) AS cost_usd, " +
	"COALESCE(SUM(CASE WHEN cost_usd IS NULL OR cost_available IS NULL THEN 1 ELSE 0 END), 0) AS unbackfilled_count, " +
	"COALESCE(SUM(CASE WHEN cost_available = 0 THEN 1 ELSE 0 END), 0) AS unavailable_cost_count, " + usageWindowHasUsageProjection

const usageWindowHourlyProjection = "model, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(cost_usd), 0) AS cost_usd, " +
	"COALESCE(SUM(CASE WHEN cost_usd IS NULL OR unavailable_cost_count IS NULL THEN 1 ELSE 0 END), 0) AS unbackfilled_count, " +
	"COALESCE(SUM(unavailable_cost_count), 0) AS unavailable_cost_count, " + usageWindowHasUsageProjection

// NewUsageWindowStatsCalculator 保存可克隆句柄；每次查询独立切到 Reader。
func NewUsageWindowStatsCalculator(db *gorm.DB) (*UsageWindowStatsCalculator, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	return &UsageWindowStatsCalculator{db: db}, nil
}

// SumByAuthIndex 从已存事件或完整小时汇总计算一个认证身份的窗口 Token/费用。
func (c *UsageWindowStatsCalculator) SumByAuthIndex(ctx context.Context, authIndex string, start time.Time, end *time.Time) (UsageWindowStats, error) {
	if c == nil || c.db == nil {
		return UsageWindowStats{}, fmt.Errorf("usage window stats calculator is nil")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return UsageWindowStats{}, fmt.Errorf("auth_index is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryDB := c.db.Clauses(dbresolver.Read).Session(&gorm.Session{Context: ctx})
	rows, err := loadUsageWindowStoredStats(queryDB, authIndex, start, end)
	if err != nil {
		return UsageWindowStats{}, err
	}
	return usageWindowStatsFromStoredRows(rows)
}

// SumGroupsByAuthIndex 复用同一窗口分段，并只按原始 model 归入上游额度组。
func (c *UsageWindowStatsCalculator) SumGroupsByAuthIndex(ctx context.Context, authIndex string, start time.Time, end *time.Time, grouper UsageWindowStatsGrouper) (UsageWindowGroupedStats, error) {
	if c == nil || c.db == nil {
		return UsageWindowGroupedStats{}, fmt.Errorf("usage window stats calculator is nil")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return UsageWindowGroupedStats{}, fmt.Errorf("auth_index is required")
	}
	if grouper == nil {
		return UsageWindowGroupedStats{}, fmt.Errorf("usage window stats grouper is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryDB := c.db.Clauses(dbresolver.Read).Session(&gorm.Session{Context: ctx})
	rows, err := loadUsageWindowStoredStats(queryDB, authIndex, start, end)
	if err != nil {
		return UsageWindowGroupedStats{}, err
	}
	return usageWindowGroupedStatsFromStoredRows(rows, grouper)
}

// loadUsageWindowStoredStats 保持五小时阈值及原半开边界；未指定终点时继续只读 raw。
func loadUsageWindowStoredStats(db *gorm.DB, authIndex string, start time.Time, end *time.Time) ([]usageWindowStoredStats, error) {
	if start.IsZero() || (end != nil && end.IsZero()) {
		return nil, nil
	}
	if end == nil {
		return sumRawUsageWindowStoredStats(db, authIndex, start, nil)
	}
	windowStart := timeutil.NormalizeStorageTime(start)
	windowEnd := timeutil.NormalizeStorageTime(*end)
	if !windowStart.Before(windowEnd) {
		return nil, nil
	}
	if windowEnd.Sub(windowStart) <= quotaWindowRawOnlyThreshold {
		return sumRawUsageWindowStoredStats(db, authIndex, windowStart, &windowEnd)
	}
	return sumLongUsageWindowStoredStats(db, authIndex, windowStart, windowEnd)
}

// sumLongUsageWindowStoredStats 保留左 raw、完整小时、保守右 raw 的互斥区间，避免迟到聚合与边界双计。
func sumLongUsageWindowStoredStats(db *gorm.DB, authIndex string, start, end time.Time) ([]usageWindowStoredStats, error) {
	leftEnd := ceilUsageWindowHour(start)
	if end.Before(leftEnd) {
		leftEnd = end
	}
	rightStart := end.Truncate(time.Hour)
	// 最近完整小时仍可能尚未进入 Overview，继续把它纳入右 raw 边界。
	safeHourlyEnd := rightStart.Add(-time.Hour)
	if safeHourlyEnd.Before(rightStart) {
		rightStart = safeHourlyEnd
	}
	if rightStart.Before(start) {
		rightStart = start
	}
	if rightStart.Before(leftEnd) {
		rightStart = leftEnd
	}
	rows := make([]usageWindowStoredStats, 0)
	if start.Before(leftEnd) {
		left, err := sumRawUsageWindowStoredStats(db, authIndex, start, &leftEnd)
		if err != nil {
			return nil, fmt.Errorf("sum left raw usage window stats: %w", err)
		}
		rows = append(rows, left...)
	}
	if leftEnd.Before(rightStart) {
		hourly, err := sumHourlyUsageWindowStoredStats(db, authIndex, leftEnd, rightStart)
		if err != nil {
			return nil, fmt.Errorf("sum hourly usage window stats: %w", err)
		}
		rows = append(rows, hourly...)
	}
	if rightStart.Before(end) {
		right, err := sumRawUsageWindowStoredStats(db, authIndex, rightStart, &end)
		if err != nil {
			return nil, fmt.Errorf("sum right raw usage window stats: %w", err)
		}
		rows = append(rows, right...)
	}
	return rows, nil
}

// sumRawUsageWindowStoredStats 在 SQLite 按真实 model 汇总已存事件金额和可用性，不读取 payload。
func sumRawUsageWindowStoredStats(db *gorm.DB, authIndex string, start time.Time, end *time.Time) ([]usageWindowStoredStats, error) {
	query := db.Model(&entities.UsageEvent{}).
		Select(usageWindowRawProjection).
		Where("auth_index = ? AND timestamp >= ?", authIndex, timeutil.FormatStorageTime(start)).
		Group("model")
	if end != nil {
		query = query.Where("timestamp < ?", timeutil.FormatStorageTime(*end))
	}
	var rows []usageWindowStoredStats
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("sum raw usage window stats: %w", err)
	}
	return rows, nil
}

// sumHourlyUsageWindowStoredStats 只扫描完整小时 Overview 行，并在 SQLite 按真实 model 合并。
func sumHourlyUsageWindowStoredStats(db *gorm.DB, authIndex string, start, end time.Time) ([]usageWindowStoredStats, error) {
	var rows []usageWindowStoredStats
	if err := db.Model(&entities.UsageOverviewHourlyStat{}).
		Select(usageWindowHourlyProjection).
		Where("auth_index = ? AND bucket_start >= ? AND bucket_start < ?", authIndex, timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
		Group("model").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("sum hourly usage window stats: %w", err)
	}
	return rows, nil
}

// usageWindowStatsFromStoredRows 汇总所有互斥来源；NULL 或非有限金额不能冒充零费用。
func usageWindowStatsFromStoredRows(rows []usageWindowStoredStats) (UsageWindowStats, error) {
	stats := UsageWindowStats{CostAvailable: true}
	for _, row := range rows {
		if err := validateUsageWindowStoredCost(row); err != nil {
			return UsageWindowStats{}, err
		}
		var err error
		stats, err = addUsageWindowStoredRow(stats, row)
		if err != nil {
			return UsageWindowStats{}, err
		}
	}
	return stats, nil
}

// usageWindowGroupedStatsFromStoredRows 保留未知正 Token 模型使额度组不完整的既有规则。
func usageWindowGroupedStatsFromStoredRows(rows []usageWindowStoredStats, grouper UsageWindowStatsGrouper) (UsageWindowGroupedStats, error) {
	result := UsageWindowGroupedStats{Groups: make(map[string]UsageWindowStats), Complete: true}
	for _, row := range rows {
		if err := validateUsageWindowStoredCost(row); err != nil {
			return UsageWindowGroupedStats{}, err
		}
		groupKey, ok := grouper(row.Model)
		groupKey = strings.TrimSpace(groupKey)
		if !ok || groupKey == "" {
			if row.HasUsage > 0 {
				result.Complete = false
			}
			continue
		}
		stats, exists := result.Groups[groupKey]
		if !exists {
			stats.CostAvailable = true
		}
		var err error
		stats, err = addUsageWindowStoredRow(stats, row)
		if err != nil {
			return UsageWindowGroupedStats{}, err
		}
		result.Groups[groupKey] = stats
	}
	return result, nil
}

// validateUsageWindowStoredCost 检查 SQL SUM 前是否存在未回填行，避免 COALESCE 抹去未知费用。
func validateUsageWindowStoredCost(row usageWindowStoredStats) error {
	if row.UnbackfilledCount > 0 {
		return fmt.Errorf("usage window model %q has unbackfilled cost", row.Model)
	}
	if math.IsNaN(row.CostUSD) || math.IsInf(row.CostUSD, 0) {
		return fmt.Errorf("usage window model %q has non-finite cost", row.Model)
	}
	return nil
}

// addUsageWindowStoredRow 累加原 total_tokens 和已存总费用，同时保留费用不可用状态。
func addUsageWindowStoredRow(stats UsageWindowStats, row usageWindowStoredStats) (UsageWindowStats, error) {
	newCost := stats.Cost + row.CostUSD
	if math.IsNaN(newCost) || math.IsInf(newCost, 0) {
		return UsageWindowStats{}, fmt.Errorf("usage window cost sum is non-finite")
	}
	stats.Tokens += row.TotalTokens
	stats.Cost = newCost
	if row.UnavailableCostCount > 0 {
		stats.CostAvailable = false
	}
	return stats, nil
}

// ceilUsageWindowHour 将非整点起点推进到下一个绝对整小时，供长窗口左边界切分。
func ceilUsageWindowHour(value time.Time) time.Time {
	value = timeutil.NormalizeStorageTime(value)
	truncated := value.Truncate(time.Hour)
	if value.Equal(truncated) {
		return truncated
	}
	return truncated.Add(time.Hour)
}
