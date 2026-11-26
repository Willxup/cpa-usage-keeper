package migration

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// LegacyPricingOverviewBucketStarts 是 20261009 价格回填与验证共用的旧时区分桶合同。
func LegacyPricingOverviewBucketStarts(timestamp time.Time) (hour, day time.Time) {
	timestamp = timeutil.NormalizeStorageTime(timestamp)
	return timestamp.Truncate(time.Hour), time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, timestamp.Location())
}

type legacyPricingOverviewTotals struct {
	legacyUsageOverviewCounts
	cost, speed, decodeSpeed                      float64
	unavailable, speedSamples, decodeSpeedSamples int64
}

type legacyPricingOverviewGroup struct {
	key    legacyUsageOverviewKey
	totals legacyPricingOverviewTotals
}

// LegacyPricingOverviewPage 只用于旧库首次费用升级的十维阶段，不使用运行时汇总实体或写入器。
type LegacyPricingOverviewPage struct {
	hourly, daily []legacyPricingOverviewGroup
}

// BuildLegacyPricingOverviewPage 在取得写连接前构建旧版十维行；只累加已回填的事实和费用。
func BuildLegacyPricingOverviewPage(events []entities.UsageEvent) (LegacyPricingOverviewPage, error) {
	hourly := make(map[legacyUsageOverviewKey]legacyPricingOverviewTotals)
	daily := make(map[legacyUsageOverviewKey]legacyPricingOverviewTotals)
	for _, event := range events {
		if event.CostUSD == nil || event.CostAvailable == nil {
			return LegacyPricingOverviewPage{}, fmt.Errorf("usage event %d cost_usd/cost_available is not backfilled", event.ID)
		}
		if math.IsNaN(*event.CostUSD) || math.IsInf(*event.CostUSD, 0) {
			return LegacyPricingOverviewPage{}, fmt.Errorf("usage event %d cost_usd is not finite", event.ID)
		}
		key := legacyUsageOverviewKey{
			APIGroupKey: legacyRequiredOverviewDimension(event.APIGroupKey), Model: legacyRequiredOverviewDimension(event.Model),
			AuthIndex: strings.TrimSpace(event.AuthIndex), ServiceTier: strings.TrimSpace(event.ServiceTier),
			ResponseServiceTier: strings.TrimSpace(event.ResponseServiceTier), ReasoningEffort: strings.TrimSpace(event.ReasoningEffort),
			Endpoint: strings.TrimSpace(event.Endpoint), ExecutorType: strings.TrimSpace(event.ExecutorType),
		}
		if event.ModelAlias != nil {
			key.ModelAlias = strings.TrimSpace(*event.ModelAlias)
		}
		hour, day := LegacyPricingOverviewBucketStarts(event.Timestamp)
		key.BucketStart = hour
		addLegacyPricingOverviewEvent(hourly, key, event)
		key.BucketStart = day
		addLegacyPricingOverviewEvent(daily, key, event)
	}
	return LegacyPricingOverviewPage{sortedLegacyPricingOverviewGroups(hourly), sortedLegacyPricingOverviewGroups(daily)}, nil
}

func addLegacyPricingOverviewEvent(rows map[legacyUsageOverviewKey]legacyPricingOverviewTotals, key legacyUsageOverviewKey, event entities.UsageEvent) {
	row := rows[key]
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
	row.cost += *event.CostUSD
	if !*event.CostAvailable {
		row.unavailable++
	}
	if event.OutputTokens > 0 && event.LatencyMS > 0 {
		row.speed += float64(event.OutputTokens) * 1000 / float64(event.LatencyMS)
		row.speedSamples++
		if event.TTFTMS != nil && *event.TTFTMS > 0 && *event.TTFTMS < event.LatencyMS {
			row.decodeSpeed += float64(event.OutputTokens) * 1000 / float64(event.LatencyMS-*event.TTFTMS)
			row.decodeSpeedSamples++
		}
	}
	rows[key] = row
}

func sortedLegacyPricingOverviewGroups(rows map[legacyUsageOverviewKey]legacyPricingOverviewTotals) []legacyPricingOverviewGroup {
	groups := make([]legacyPricingOverviewGroup, 0, len(rows))
	for key, row := range rows {
		groups = append(groups, legacyPricingOverviewGroup{key, row})
	}
	slices.SortFunc(groups, func(left, right legacyPricingOverviewGroup) int {
		l, r := left.key, right.key
		if order := l.BucketStart.Compare(r.BucketStart); order != 0 {
			return order
		}
		return slices.Compare(
			[]string{l.APIGroupKey, l.Model, l.AuthIndex, l.ModelAlias, l.ServiceTier, l.ResponseServiceTier, l.ReasoningEffort, l.Endpoint, l.ExecutorType},
			[]string{r.APIGroupKey, r.Model, r.AuthIndex, r.ModelAlias, r.ServiceTier, r.ResponseServiceTier, r.ReasoningEffort, r.Endpoint, r.ExecutorType},
		)
	})
	return groups
}

// Apply 写入冻结十维列；调用方在同一事务提交两表和价格迁移游标，绝不改普通 Overview 水位。
func (page LegacyPricingOverviewPage) Apply(tx *gorm.DB, now time.Time) error {
	stamp := timeutil.FormatStorageTime(now)
	for _, target := range []struct {
		table  string
		groups []legacyPricingOverviewGroup
	}{
		{"usage_overview_hourly_stats", page.hourly}, {"usage_overview_daily_stats", page.daily},
	} {
		for _, group := range target.groups {
			if err := applyLegacyPricingOverviewGroup(tx, target.table, group, stamp); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyLegacyPricingOverviewGroup(tx *gorm.DB, table string, group legacyPricingOverviewGroup, stamp string) error {
	key, totals := group.key, group.totals
	if math.IsNaN(totals.cost) || math.IsInf(totals.cost, 0) {
		return fmt.Errorf("%s legacy pricing overview fee is not finite", table)
	}
	values := map[string]any{
		"request_count": totals.RequestCount, "success_count": totals.SuccessCount, "failure_count": totals.FailureCount,
		"input_tokens": totals.InputTokens, "output_tokens": totals.OutputTokens, "reasoning_tokens": totals.ReasoningTokens,
		"cached_tokens": totals.CachedTokens, "cache_read_tokens": totals.CacheReadTokens, "cache_creation_tokens": totals.CacheCreationTokens,
		"total_tokens": totals.TotalTokens, "cost_usd": totals.cost, "unavailable_cost_count": totals.unavailable,
		"speed_tps_sum": totals.speed, "speed_sample_count": totals.speedSamples,
		"decode_speed_tps_sum": totals.decodeSpeed, "decode_speed_sample_count": totals.decodeSpeedSamples,
	}
	updates := make(map[string]any, len(values)+1)
	for field, value := range values {
		updates[field] = gorm.Expr(field+" + ?", value)
	}
	updates["updated_at"] = stamp
	bucket := timeutil.FormatStorageTime(key.BucketStart)
	args := []any{bucket, key.APIGroupKey, key.Model, key.AuthIndex, key.ModelAlias, key.ServiceTier, key.ResponseServiceTier, key.ReasoningEffort, key.Endpoint, key.ExecutorType}
	update := func() *gorm.DB {
		return tx.Table(table).Where(legacyUsageOverviewDimensionsPredicate, args...).
			Where("cost_usd IS NOT NULL AND unavailable_cost_count IS NOT NULL AND cost_usd + ? BETWEEN ? AND ?", totals.cost, -math.MaxFloat64, math.MaxFloat64).
			Updates(updates)
	}
	// 沿用先 UPDATE、无现有桶再 INSERT 的顺序；未回填或无法保存的已有桶不可被当作零。
	result := update()
	if result.Error != nil {
		return fmt.Errorf("update %s legacy pricing overview: %w", table, result.Error)
	}
	if result.RowsAffected != 0 {
		return nil
	}
	values["bucket_start"], values["api_group_key"], values["model"], values["auth_index"] = bucket, key.APIGroupKey, key.Model, key.AuthIndex
	values["model_alias"], values["service_tier"], values["response_service_tier"] = key.ModelAlias, key.ServiceTier, key.ResponseServiceTier
	values["reasoning_effort"], values["endpoint"], values["executor_type"] = key.ReasoningEffort, key.Endpoint, key.ExecutorType
	values["created_at"], values["updated_at"] = stamp, stamp
	if err := tx.Table(table).Create(values).Error; err != nil {
		retry := update()
		if retry.Error != nil {
			return fmt.Errorf("insert %s legacy pricing overview: %w; retry update: %v", table, err, retry.Error)
		}
		if retry.RowsAffected == 0 {
			return fmt.Errorf("insert %s legacy pricing overview: %w; retry matched no ready fee row", table, err)
		}
	}
	return nil
}
