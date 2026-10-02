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

// 时间轴和数据共用项目时区，空桶保留为零；完整天用 AddDate 避免 DST 导致日期漂移。
func newUsageOverviewComparisons(filter dto.UsageQueryFilter, byDay bool) *dto.UsageOverviewComparisonsRecord {
	result := &dto.UsageOverviewComparisonsRecord{
		Models: map[string]*dto.UsageComparisonItemRecord{}, APIKeys: map[string]*dto.UsageComparisonItemRecord{},
		AuthFiles: map[string]*dto.UsageComparisonItemRecord{}, AIProviders: map[string]*dto.UsageComparisonItemRecord{},
		Buckets: usageOverviewComparisonBuckets(filter, byDay), Granularity: "hourly",
	}
	if byDay {
		result.Granularity = "daily"
	}
	return result
}

func usageOverviewComparisonBuckets(filter dto.UsageQueryFilter, byDay bool) []string {
	buckets := []string{}
	start, end := timeutil.NormalizeStorageTime(*filter.StartTime), timeutil.NormalizeStorageTime(*filter.EndTime)
	cursor := start.Truncate(time.Hour)
	if byDay {
		cursor = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	}
	for cursor.Before(end) || (!filter.EndExclusive && cursor.Equal(end)) {
		bucket := usageOverviewBucket(cursor, byDay)
		buckets = append(buckets, bucket)
		if byDay {
			cursor = cursor.AddDate(0, 0, 1)
		} else {
			cursor = cursor.Add(time.Hour)
		}
	}
	return buckets
}

// applyUsageEventToComparisonOnly 把窄边界事件的已存费用同时归到模型、Key 与已知身份。
func applyUsageEventToComparisonOnly(comparisons *dto.UsageOverviewComparisonsRecord, event entities.UsageEvent, identityLookup analysisIdentityLookup) error {
	cost, available, err := usageOverviewStoredEventCost(event)
	if err != nil {
		return err
	}
	failed := int64(0)
	if event.Failed {
		failed = 1
	}
	row := dto.UsageComparisonItemRecord{Requests: 1, Failures: failed, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens, CacheReadTokens: event.CacheReadTokens, CacheCreationTokens: event.CacheCreationTokens, ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens, CostUSD: cost, CostAvailable: available}
	row.Bucket = usageOverviewBucket(event.Timestamp, comparisons.Granularity == "daily")
	applyUsageOverviewComparison(comparisons, event.Model, event.APIGroupKey, row)
	applyUsageOverviewIdentityComparison(comparisons, identityLookup, event.AuthIndex, row)
	return nil
}

// 同一比较行的已存费用同时归入模型和 API Key 两个维度。
func applyUsageOverviewComparison(comparisons *dto.UsageOverviewComparisonsRecord, model, apiKey string, row dto.UsageComparisonItemRecord) {
	addUsageOverviewComparison(comparisons.Models, normalizeUsageOverviewDimension(model), row)
	addUsageOverviewComparison(comparisons.APIKeys, normalizeUsageOverviewDimension(apiKey), row)
}

func applyUsageOverviewIdentityComparison(comparisons *dto.UsageOverviewComparisonsRecord, identityLookup analysisIdentityLookup, authIndex string, row dto.UsageComparisonItemRecord) {
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAuthFile, strings.TrimSpace(authIndex)); ok {
		row.Label = identity.label
		addUsageOverviewComparison(comparisons.AuthFiles, identity.identity, row)
	}
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAIProvider, strings.TrimSpace(authIndex)); ok {
		row.Label = identity.label
		addUsageOverviewComparison(comparisons.AIProviders, identity.identity, row)
	}
}

func addUsageOverviewComparison(items map[string]*dto.UsageComparisonItemRecord, key string, row dto.UsageComparisonItemRecord) {
	item := items[key]
	if item == nil {
		item = &dto.UsageComparisonItemRecord{Key: key, Label: row.Label, CostAvailable: true, TokenBuckets: map[string]int64{}}
		items[key] = item
	}
	if item.Label == "" && row.Label != "" {
		item.Label = row.Label
	}
	item.Requests += row.Requests
	item.Failures += row.Failures
	item.InputTokens += row.InputTokens
	item.OutputTokens += row.OutputTokens
	item.CacheReadTokens += row.CacheReadTokens
	item.CacheCreationTokens += row.CacheCreationTokens
	item.ReasoningTokens += row.ReasoningTokens
	item.TotalTokens += row.TotalTokens
	if row.Bucket != "" {
		item.TokenBuckets[row.Bucket] += row.TotalTokens
	}
	item.CostUSD += row.CostUSD
	item.CostAvailable = item.CostAvailable && row.CostAvailable
}

// 区间汇总直接读取已存费用，Token 趋势单独按时间和分类聚合。
// 比较查询复用范围规划，边界事件由调用方读取一次并补入比较结果。
func loadAndApplyUsageOverviewStats(overview *dto.UsageOverviewRecord, db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, bucketByDay bool) error {
	if filter.ComparisonOnly {
		rows, err := loadUsageOverviewComparisonProjection(db, filter, start, end, grain)
		if err != nil {
			return err
		}
		var identityLookup analysisIdentityLookup
		authIndexes := make([]string, 0)
		seenAuthIndexes := make(map[string]struct{})
		for _, row := range rows {
			if authIndex := strings.TrimSpace(row.AuthIndex); authIndex != "" {
				if _, seen := seenAuthIndexes[authIndex]; !seen {
					authIndexes = append(authIndexes, authIndex)
					seenAuthIndexes[authIndex] = struct{}{}
				}
			}
		}
		identityLookup, err = loadAnalysisIdentityLookup(db, authIndexes)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.MissingCostCount != 0 || row.CostUSD == nil || row.UnavailableCostCount == nil {
				return fmt.Errorf("usage overview %s comparison has unbackfilled cost", grain)
			}
			if math.IsNaN(*row.CostUSD) || math.IsInf(*row.CostUSD, 0) {
				return fmt.Errorf("usage overview %s comparison has non-finite cost", grain)
			}
			comparison := dto.UsageComparisonItemRecord{Requests: row.RequestCount, Failures: row.FailureCount, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, ReasoningTokens: row.ReasoningTokens, TotalTokens: row.TotalTokens, CostUSD: *row.CostUSD, CostAvailable: *row.UnavailableCostCount == 0}
			applyUsageOverviewComparison(overview.Comparisons, row.Model, row.APIGroupKey, comparison)
			applyUsageOverviewIdentityComparison(overview.Comparisons, identityLookup, row.AuthIndex, comparison)
		}
		return loadUsageOverviewComparisonTokenSeries(db, filter, start, end, grain, bucketByDay, overview.Comparisons, identityLookup)
	}
	var model any = &entities.UsageOverviewHourlyStat{}
	if grain == "daily" {
		model = &entities.UsageOverviewDailyStat{}
	}
	rows, err := loadUsageOverviewStatProjection(db.Model(model), filter, start, end, grain)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := applyUsageOverviewStatToOverview(overview, row, bucketByDay); err != nil {
			return err
		}
	}
	return nil
}
