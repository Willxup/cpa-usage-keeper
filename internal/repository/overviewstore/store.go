package overviewstore

import (
	"fmt"
	"math"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const overviewDimensionsPredicate = "bucket_start = ? AND api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ? AND service_tier = ? AND response_service_tier = ? AND reasoning_effort = ? AND endpoint = ? AND executor_type = ?"

// 旧费用桶必须已回填，且数据库加上本页有限增量后仍须是可保存的有限 REAL。
const overviewReadyFeePredicate = "cost_usd IS NOT NULL AND unavailable_cost_count IS NOT NULL AND cost_usd + ? BETWEEN ? AND ?"

// ApplyRows 用完整维度唯一键累加原统计与已存费用；调用方持有 hourly、daily、checkpoint 的共同事务。
func ApplyRows(tx *gorm.DB, hourlyRows []entities.UsageOverviewHourlyStat, dailyRows []entities.UsageOverviewDailyStat, now time.Time) error {
	// hourly 必须全部成功，调用方才会继续提交 daily 和 checkpoint。
	for _, row := range hourlyRows {
		if err := applyHourlyRow(tx, row, now); err != nil {
			return err
		}
	}
	// daily 与 hourly 共享同一事务和累计公式，禁止出现单表已推进状态。
	for _, row := range dailyRows {
		if err := applyDailyRow(tx, row, now); err != nil {
			return err
		}
	}
	return nil
}

// applyHourlyRow 在同一事务内按完整维度累加；旧 NULL 费用桶只能等迁移回填，不能当零更新。
func applyHourlyRow(tx *gorm.DB, row entities.UsageOverviewHourlyStat, now time.Time) error {
	if err := validateOverviewFeeDelta(row.CostUSD, row.UnavailableCostCount); err != nil {
		return fmt.Errorf("hourly overview fee: %w", err)
	}
	updates := tokenStatUpdates(row.RequestCount, row.SuccessCount, row.FailureCount, row.InputTokens, row.OutputTokens, row.ReasoningTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, row.TotalTokens, *row.CostUSD, *row.UnavailableCostCount, now)
	args := hourlyDimensionArgs(row)
	// update-first 避免正常累计走唯一索引冲突路径并消耗自增 ID。
	result := tx.Model(&entities.UsageOverviewHourlyStat{}).Where(overviewDimensionsPredicate, args...).Where(overviewReadyFeePredicate, *row.CostUSD, -math.MaxFloat64, math.MaxFloat64).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage overview hourly stat: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}

	row.CreatedAt = timeutil.NormalizeStorageTime(now)
	row.UpdatedAt = timeutil.NormalizeStorageTime(now)
	if insertErr := tx.Create(&row).Error; insertErr != nil {
		// 并发创建相同 key 时只重试一次完整五维 UPDATE。
		retryResult := tx.Model(&entities.UsageOverviewHourlyStat{}).Where(overviewDimensionsPredicate, args...).Where(overviewReadyFeePredicate, *row.CostUSD, -math.MaxFloat64, math.MaxFloat64).Updates(updates)
		if retryResult.Error != nil {
			return fmt.Errorf("insert usage overview hourly stat: %w; retry update: %v", insertErr, retryResult.Error)
		}
		if retryResult.RowsAffected == 0 {
			return fmt.Errorf("insert usage overview hourly stat: %w; existing fee row is not backfilled, accumulated amount is not finite, or insert failed", insertErr)
		}
	}
	return nil
}

// applyDailyRow 与小时桶使用同一费用累加和 NULL 门禁，失败由调用方回滚整个聚合页。
func applyDailyRow(tx *gorm.DB, row entities.UsageOverviewDailyStat, now time.Time) error {
	if err := validateOverviewFeeDelta(row.CostUSD, row.UnavailableCostCount); err != nil {
		return fmt.Errorf("daily overview fee: %w", err)
	}
	updates := tokenStatUpdates(row.RequestCount, row.SuccessCount, row.FailureCount, row.InputTokens, row.OutputTokens, row.ReasoningTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, row.TotalTokens, *row.CostUSD, *row.UnavailableCostCount, now)
	args := dailyDimensionArgs(row)
	// daily 使用与 hourly 完全相同的最终唯一键和 update-first 语义。
	result := tx.Model(&entities.UsageOverviewDailyStat{}).Where(overviewDimensionsPredicate, args...).Where(overviewReadyFeePredicate, *row.CostUSD, -math.MaxFloat64, math.MaxFloat64).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage overview daily stat: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}

	row.CreatedAt = timeutil.NormalizeStorageTime(now)
	row.UpdatedAt = timeutil.NormalizeStorageTime(now)
	if insertErr := tx.Create(&row).Error; insertErr != nil {
		retryResult := tx.Model(&entities.UsageOverviewDailyStat{}).Where(overviewDimensionsPredicate, args...).Where(overviewReadyFeePredicate, *row.CostUSD, -math.MaxFloat64, math.MaxFloat64).Updates(updates)
		if retryResult.Error != nil {
			return fmt.Errorf("insert usage overview daily stat: %w; retry update: %v", insertErr, retryResult.Error)
		}
		if retryResult.RowsAffected == 0 {
			return fmt.Errorf("insert usage overview daily stat: %w; existing fee row is not backfilled, accumulated amount is not finite, or insert failed", insertErr)
		}
	}
	return nil
}

func hourlyDimensionArgs(row entities.UsageOverviewHourlyStat) []any {
	return []any{
		timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.AuthIndex, row.ModelAlias,
		row.ServiceTier, row.ResponseServiceTier, row.ReasoningEffort, row.Endpoint, row.ExecutorType,
	}
}

func dailyDimensionArgs(row entities.UsageOverviewDailyStat) []any {
	return []any{
		timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.AuthIndex, row.ModelAlias,
		row.ServiceTier, row.ResponseServiceTier, row.ReasoningEffort, row.Endpoint, row.ExecutorType,
	}
}

// tokenStatUpdates 将原事实列与已存费用组成一次原子增量，避免只更新其中一类。
func tokenStatUpdates(requestCount, successCount, failureCount, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheReadTokens, cacheCreationTokens, totalTokens int64, costUSD float64, unavailableCount int64, now time.Time) map[string]any {
	return map[string]any{
		"request_count":          gorm.Expr("request_count + ?", requestCount),
		"success_count":          gorm.Expr("success_count + ?", successCount),
		"failure_count":          gorm.Expr("failure_count + ?", failureCount),
		"input_tokens":           gorm.Expr("input_tokens + ?", inputTokens),
		"output_tokens":          gorm.Expr("output_tokens + ?", outputTokens),
		"reasoning_tokens":       gorm.Expr("reasoning_tokens + ?", reasoningTokens),
		"cached_tokens":          gorm.Expr("cached_tokens + ?", cachedTokens),
		"cache_read_tokens":      gorm.Expr("cache_read_tokens + ?", cacheReadTokens),
		"cache_creation_tokens":  gorm.Expr("cache_creation_tokens + ?", cacheCreationTokens),
		"total_tokens":           gorm.Expr("total_tokens + ?", totalTokens),
		"cost_usd":               gorm.Expr("cost_usd + ?", costUSD),
		"unavailable_cost_count": gorm.Expr("unavailable_cost_count + ?", unavailableCount),
		"updated_at":             timeutil.FormatStorageTime(now),
	}
}

// validateOverviewFeeDelta 拒绝未回填或非有限值，保留显式零价和缺价计数。
func validateOverviewFeeDelta(cost *float64, unavailable *int64) error {
	if cost == nil || unavailable == nil {
		return fmt.Errorf("cost_usd/unavailable_cost_count is not backfilled")
	}
	if math.IsNaN(*cost) || math.IsInf(*cost, 0) {
		return fmt.Errorf("cost_usd is not finite")
	}
	return nil
}
