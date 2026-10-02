package repository

import (
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// analysisOverviewStatProjection 同时承接 hourly 与 daily 聚合表的 Analysis 窄投影。
type analysisOverviewStatProjection struct {
	BucketStart          time.Time
	APIGroupKey          string
	Model                string
	AuthIndex            string
	RequestCount         int64
	InputTokens          int64
	OutputTokens         int64
	ReasoningTokens      int64
	CacheReadTokens      int64
	CacheCreationTokens  int64
	TotalTokens          int64
	CostUSD              *float64
	UnavailableCostCount *int64
}

// Analysis 仅投影图表、构成和身份需要的旧计数／Token 字段，以及已存费用两列。
const analysisOverviewProjectionColumns = "bucket_start, api_group_key, model, auth_index, request_count, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, cost_usd, unavailable_cost_count"

// loadAnalysisOverviewHourlyStatsWithFilter 只读 active Key 的完整小时汇总，不拼原始事件。
func loadAnalysisOverviewHourlyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]analysisOverviewStatProjection, error) {
	query := db.Model(&entities.UsageOverviewHourlyStat{}).
		Joins("INNER JOIN cpa_api_keys ON cpa_api_keys.api_key = usage_overview_hourly_stats.api_group_key AND cpa_api_keys.is_deleted = ?", false)
	return loadAnalysisOverviewStatProjection(query, filter, start, end, "hourly")
}

// loadAnalysisOverviewDailyStatsWithFilter 保留自然日范围与 active Key JOIN，费用来自日汇总。
func loadAnalysisOverviewDailyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]analysisOverviewStatProjection, error) {
	query := db.Model(&entities.UsageOverviewDailyStat{}).
		Joins("INNER JOIN cpa_api_keys ON cpa_api_keys.api_key = usage_overview_daily_stats.api_group_key AND cpa_api_keys.is_deleted = ?", false)
	return loadAnalysisOverviewStatProjection(query, filter, start, end, "daily")
}

// loadAnalysisOverviewStatProjection 保留原时间／Key筛选，并拒绝未回填或非法金额。
func loadAnalysisOverviewStatProjection(query *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string) ([]analysisOverviewStatProjection, error) {
	rows := make([]analysisOverviewStatProjection, 0)
	query = query.
		Select(analysisOverviewProjectionColumns).
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
		Order("bucket_start asc")
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview %s stats: %w", grain, err)
	}
	for _, row := range rows {
		if row.CostUSD == nil || row.UnavailableCostCount == nil {
			return nil, fmt.Errorf("analysis %s bucket %s has unbackfilled cost", grain, timeutil.FormatStorageTime(row.BucketStart))
		}
		if math.IsNaN(*row.CostUSD) || math.IsInf(*row.CostUSD, 0) {
			return nil, fmt.Errorf("analysis %s bucket %s has non-finite cost", grain, timeutil.FormatStorageTime(row.BucketStart))
		}
	}
	return rows, nil
}
