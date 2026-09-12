package repository

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

type usageOverviewStatProjection struct {
	ComparisonKind int
	// 比较行没有时间桶；只有主序列行需要读取非空 bucket_start。
	BucketStart             *time.Time
	APIGroupKey             string
	Model                   string
	AuthIndex               string
	ModelAlias              string
	ServiceTier             string
	ResponseServiceTier     string
	ReasoningEffort         string
	Endpoint                string
	ExecutorType            string
	RequestCount            int64
	SuccessCount            int64
	FailureCount            int64
	InputTokens             int64
	OutputTokens            int64
	ReasoningTokens         int64
	CacheReadTokens         int64
	CacheCreationTokens     int64
	TotalTokens             int64
	CostUncachedInputTokens int64
	CostOutputTokens        int64
	CostCacheReadTokens     int64
	CostCacheCreationTokens int64
}

// 标准 SQL CASE 先逐行完成计费 Token 的非负与普通输入归一化，再按启用规则所需维度合并。
const usageOverviewStatProjectionAggregateColumns = `
	SUM(request_count) AS request_count,
	SUM(success_count) AS success_count,
	SUM(failure_count) AS failure_count,
	SUM(input_tokens) AS input_tokens,
	SUM(output_tokens) AS output_tokens,
	SUM(reasoning_tokens) AS reasoning_tokens,
	SUM(cache_read_tokens) AS cache_read_tokens,
	SUM(cache_creation_tokens) AS cache_creation_tokens,
	SUM(total_tokens) AS total_tokens,
	SUM(CASE
		WHEN (CASE WHEN input_tokens > 0 THEN input_tokens ELSE 0 END) -
			(CASE WHEN cache_read_tokens > 0 THEN cache_read_tokens ELSE 0 END) -
			(CASE WHEN cache_creation_tokens > 0 THEN cache_creation_tokens ELSE 0 END) > 0
		THEN (CASE WHEN input_tokens > 0 THEN input_tokens ELSE 0 END) -
			(CASE WHEN cache_read_tokens > 0 THEN cache_read_tokens ELSE 0 END) -
			(CASE WHEN cache_creation_tokens > 0 THEN cache_creation_tokens ELSE 0 END)
		ELSE 0
	END) AS cost_uncached_input_tokens,
	SUM(CASE WHEN output_tokens > 0 THEN output_tokens ELSE 0 END) AS cost_output_tokens,
	SUM(CASE WHEN cache_read_tokens > 0 THEN cache_read_tokens ELSE 0 END) AS cost_cache_read_tokens,
	SUM(CASE WHEN cache_creation_tokens > 0 THEN cache_creation_tokens ELSE 0 END) AS cost_cache_creation_tokens`

const usageOverviewStatProjectionOuterAggregateColumns = `
	SUM(request_count) AS request_count,
	SUM(success_count) AS success_count,
	SUM(failure_count) AS failure_count,
	SUM(input_tokens) AS input_tokens,
	SUM(output_tokens) AS output_tokens,
	SUM(reasoning_tokens) AS reasoning_tokens,
	SUM(cache_read_tokens) AS cache_read_tokens,
	SUM(cache_creation_tokens) AS cache_creation_tokens,
	SUM(total_tokens) AS total_tokens,
	SUM(cost_uncached_input_tokens) AS cost_uncached_input_tokens,
	SUM(cost_output_tokens) AS cost_output_tokens,
	SUM(cost_cache_read_tokens) AS cost_cache_read_tokens,
	SUM(cost_cache_creation_tokens) AS cost_cache_creation_tokens`

func loadUsageOverviewStatProjection(query *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, activeFields pricing.ActiveFields) ([]usageOverviewStatProjection, error) {
	rows := make([]usageOverviewStatProjection, 0)
	dimensionColumns := UsagePricingDimensionColumns(activeFields)
	dimensionColumns = append([]string{"bucket_start"}, dimensionColumns...)
	if filter.IncludeComparisons {
		if !containsUsageOverviewDimension(dimensionColumns, "api_group_key") {
			dimensionColumns = append(dimensionColumns, "api_group_key")
		}
		if !containsUsageOverviewDimension(dimensionColumns, "auth_index") {
			dimensionColumns = append(dimensionColumns, "auth_index")
		}
		if filter.ComparisonOnly {
			return loadUsageOverviewStatProjectionComparisonOnly(query, filter, start, end, grain, activeFields, dimensionColumns)
		}
		// 比较图和主曲线共用同一批带时间桶的行；额外维度在内存中汇总，避免再次扫描 rollup。
		// 身份维度用于 Overview 的认证文件/AI 供应商列表，即使当前价格规则未启用 auth_index 也必须保留。
	}
	if filter.IncludeComparisons {
		return loadUsageOverviewStatProjectionWithComparisons(query, filter, start, end, grain, activeFields, dimensionColumns)
	}
	selectColumns := strings.Join(dimensionColumns, ", ") + ", " + usageOverviewStatProjectionAggregateColumns
	query = query.
		Select(selectColumns).
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end))
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	query = query.Group(strings.Join(dimensionColumns, ", "))
	query = query.Order("bucket_start asc")
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview %s projection: %w", grain, err)
	}
	return rows, nil
}

func loadUsageOverviewStatProjectionComparisonOnly(query *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, activeFields pricing.ActiveFields, dimensions []string) ([]usageOverviewStatProjection, error) {
	table := "usage_overview_hourly_stats"
	if grain == "daily" {
		table = "usage_overview_daily_stats"
	}
	where := "bucket_start >= ? AND bucket_start < ?"
	args := []any{timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)}
	if key := strings.TrimSpace(filter.APIGroupKey); key != "" {
		where += " AND api_group_key = ?"
		args = append(args, key)
	}
	group := strings.Join(dimensions[1:], ", ")
	sql := fmt.Sprintf("SELECT 1 AS comparison_kind, %s, %s FROM %s WHERE %s GROUP BY %s ORDER BY model ASC, api_group_key ASC", strings.Join(dimensions[1:], ", "), usageOverviewStatProjectionAggregateColumns, table, where, group)
	rows := make([]usageOverviewStatProjection, 0)
	if err := query.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview %s comparison projection: %w", grain, err)
	}
	return rows, nil
}

// loadUsageOverviewStatProjectionWithComparisons 在一次 CTE rollup 读取中同时产出时间序列和维度汇总。
// CTE 先按最细的时间/价格维度聚合，外层再分别按主曲线与比较维度汇总，避免重复扫描或向 Go 返回笛卡尔展开的中间行。
func loadUsageOverviewStatProjectionWithComparisons(query *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, activeFields pricing.ActiveFields, allDimensions []string) ([]usageOverviewStatProjection, error) {
	rows := make([]usageOverviewStatProjection, 0)
	table := "usage_overview_hourly_stats"
	if grain == "daily" {
		table = "usage_overview_daily_stats"
	}
	baseDimensions := strings.Join(allDimensions, ", ")
	seriesDimensions := append([]string(nil), UsagePricingDimensionColumns(activeFields)...)
	seriesGroupDimensions := append([]string{"bucket_start"}, seriesDimensions...)
	comparisonDimensions := append([]string(nil), seriesDimensions...)
	if !containsUsageOverviewDimension(comparisonDimensions, "api_group_key") {
		comparisonDimensions = append(comparisonDimensions, "api_group_key")
	}
	if !containsUsageOverviewDimension(comparisonDimensions, "auth_index") {
		comparisonDimensions = append(comparisonDimensions, "auth_index")
	}

	selectDimension := func(column string, comparison bool) string {
		if comparison || containsUsageOverviewDimension(seriesGroupDimensions, column) {
			return column
		}
		return "'' AS " + column
	}
	seriesSelectDimensions := make([]string, 0, len(allDimensions)+2)
	for _, column := range allDimensions {
		if column == "bucket_start" {
			seriesSelectDimensions = append(seriesSelectDimensions, column)
			continue
		}
		seriesSelectDimensions = append(seriesSelectDimensions, selectDimension(column, false))
	}
	comparisonSelectDimensions := make([]string, 0, len(allDimensions)+2)
	for _, column := range allDimensions {
		if column == "bucket_start" {
			comparisonSelectDimensions = append(comparisonSelectDimensions, "NULL AS bucket_start")
			continue
		}
		comparisonSelectDimensions = append(comparisonSelectDimensions, selectDimension(column, true))
	}

	where := "bucket_start >= ? AND bucket_start < ?"
	args := []any{timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)}
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		where += " AND api_group_key = ?"
		args = append(args, apiGroupKey)
	}
	outerAggregate := usageOverviewStatProjectionOuterAggregateColumns
	seriesGroup := strings.Join(seriesGroupDimensions, ", ")
	comparisonGroup := strings.Join(comparisonDimensions, ", ")
	sql := fmt.Sprintf(`WITH base AS (
		SELECT %s, %s
		FROM %s
		WHERE %s
		GROUP BY %s
	)
	SELECT 0 AS comparison_kind, %s, %s
	FROM base
	GROUP BY %s
	UNION ALL
	SELECT 1 AS comparison_kind, %s, %s
	FROM base
	GROUP BY %s
	ORDER BY comparison_kind ASC, bucket_start ASC`, baseDimensions, usageOverviewStatProjectionAggregateColumns, table, where, baseDimensions, strings.Join(seriesSelectDimensions, ", "), outerAggregate, seriesGroup, strings.Join(comparisonSelectDimensions, ", "), outerAggregate, comparisonGroup)
	if err := query.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview %s projection with comparisons: %w", grain, err)
	}
	return rows, nil
}

func containsUsageOverviewDimension(columns []string, target string) bool {
	for _, column := range columns {
		if column == target {
			return true
		}
	}
	return false
}

func applyUsageOverviewStatToOverview(overview *dto.UsageOverviewRecord, row usageOverviewStatProjection, bucketByDay bool, costResolver pricing.Resolver) {
	result := calculateUsageOverviewProjectionCost(costResolver, row)
	applyUsageOverviewStatToOverviewWithCost(overview, row, bucketByDay, result)
}

func applyUsageOverviewStatToOverviewWithCost(overview *dto.UsageOverviewRecord, row usageOverviewStatProjection, bucketByDay bool, result pricing.CostResult) {
	applyUsageOverviewStatToSnapshotTotals(overview.Usage, row.RequestCount, row.SuccessCount, row.FailureCount, row.TotalTokens)
	if !result.Available {
		overview.Summary.CostAvailable = false
	}
	rowCost := result.Cost.TotalCostUSD
	applyUsageOverviewStatToSummary(overview, row.InputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, rowCost)

	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(*row.BucketStart), bucketByDay)
	applyUsageOverviewStatToSeries(&overview.Series, row.RequestCount, row.InputTokens, row.CacheReadTokens, row.TotalTokens, rowCost, bucketKey, bucketMinutes)
}

func calculateUsageOverviewProjectionCost(costResolver pricing.Resolver, row usageOverviewStatProjection) pricing.CostResult {
	return costResolver.Calculate(newUsagePricingCostSubject(
		row.APIGroupKey,
		row.Model,
		row.AuthIndex,
		row.ModelAlias,
		row.ServiceTier,
		row.ResponseServiceTier,
		row.ReasoningEffort,
		row.Endpoint,
		row.ExecutorType,
		row.CostUncachedInputTokens+row.CostCacheReadTokens+row.CostCacheCreationTokens,
		row.CostOutputTokens,
		row.CostCacheReadTokens,
		row.CostCacheCreationTokens,
	))
}
