package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const usageOverviewAggregationCheckpointName = "overview"

// AggregateUsageOverviewStats 按 usage_events 自增 ID 增量推进 Overview 小时/天/health 统计。
func AggregateUsageOverviewStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	now = timeutil.NormalizeStorageTime(now)
	batchSize := insertBatchSize(entities.UsageEvent{})
	for {
		processed, err := aggregateUsageOverviewStatsBatch(ctx, db, now, batchSize)
		if err != nil {
			return err
		}
		if processed < batchSize {
			return nil
		}
	}
}

// HasPendingUsageOverviewAggregation 用轻量 ID cursor 判断 Overview stats 是否落后，避免空轮次每秒跑完整聚合。
func HasPendingUsageOverviewAggregation(ctx context.Context, db *gorm.DB) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("database is nil")
	}
	var maxEventID int64
	if err := db.WithContext(ctx).Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&maxEventID).Error; err != nil {
		return false, fmt.Errorf("load max usage event id: %w", err)
	}
	if maxEventID == 0 {
		return false, nil
	}

	var checkpoint entities.UsageOverviewAggregationCheckpoint
	err := db.WithContext(ctx).Where("name = ?", usageOverviewAggregationCheckpointName).Take(&checkpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("load usage overview aggregation checkpoint: %w", err)
	}
	return checkpoint.LastAggregatedUsageEventID < maxEventID, nil
}

// CleanupUsageOverviewHealthStats 清理 health 预聚合的保留期外数据，避免短粒度健康表长期膨胀。
func CleanupUsageOverviewHealthStats(db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	cutoff := timeutil.NormalizeStorageTime(now).Add(-8 * 24 * time.Hour)
	if err := db.Where("bucket_start < ?", timeutil.FormatStorageTime(cutoff)).Delete(&entities.UsageOverviewHealthStat{}).Error; err != nil {
		return fmt.Errorf("cleanup usage overview health stats: %w", err)
	}
	return nil
}

// aggregateUsageOverviewStatsBatch 在一个事务里读取新事件、累计 stats，并推进 checkpoint。
func aggregateUsageOverviewStatsBatch(ctx context.Context, db *gorm.DB, now time.Time, limit int) (int, error) {
	processed := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint, err := getOrCreateUsageOverviewAggregationCheckpoint(tx)
		if err != nil {
			return err
		}

		var events []entities.UsageEvent
		if err := tx.Select("id, api_group_key, model, service_tier, model_alias, auth_index, timestamp, failed, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
			Where("id > ?", checkpoint.LastAggregatedUsageEventID).
			Order("id asc").
			Limit(limit).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage overview aggregation events: %w", err)
		}
		if len(events) == 0 {
			processed = 0
			return nil
		}

		hourlyRows, dailyRows, healthRows, maxEventID := buildUsageOverviewStatsRows(events, now)
		if err := applyUsageOverviewHourlyStats(tx, hourlyRows, now); err != nil {
			return err
		}
		if err := applyUsageOverviewDailyStats(tx, dailyRows, now); err != nil {
			return err
		}
		if err := applyUsageOverviewHealthStats(tx, healthRows, now); err != nil {
			return err
		}
		if err := tx.Model(&entities.UsageOverviewAggregationCheckpoint{}).
			Where("id = ?", checkpoint.ID).
			Updates(map[string]any{
				"last_aggregated_usage_event_id": maxEventID,
				"stats_updated_at":               timeutil.FormatStorageTime(now),
			}).Error; err != nil {
			return fmt.Errorf("update usage overview aggregation checkpoint: %w", err)
		}
		processed = len(events)
		return nil
	})
	return processed, err
}

// getOrCreateUsageOverviewAggregationCheckpoint 读取 Overview 的唯一 cursor 行，不存在时初始化为从头聚合。
func getOrCreateUsageOverviewAggregationCheckpoint(tx *gorm.DB) (entities.UsageOverviewAggregationCheckpoint, error) {
	checkpoint := entities.UsageOverviewAggregationCheckpoint{Name: usageOverviewAggregationCheckpointName}
	if err := tx.Where("name = ?", usageOverviewAggregationCheckpointName).FirstOrCreate(&checkpoint).Error; err != nil {
		return checkpoint, fmt.Errorf("get usage overview aggregation checkpoint: %w", err)
	}
	return checkpoint, nil
}

type usageOverviewStatsKey struct {
	BucketStart time.Time
	APIGroupKey string
	Model       string
	ServiceTier string
	AuthIndex   string
	ModelAlias  string
}

type usageOverviewHealthStatsKey struct {
	BucketStart time.Time
	SpanSeconds int64
	APIGroupKey string
}

// buildUsageOverviewStatsRows 先在内存按聚合 key 合并一批事件，减少 SQLite 写入次数。
func buildUsageOverviewStatsRows(events []entities.UsageEvent, now time.Time) ([]entities.UsageOverviewHourlyStat, []entities.UsageOverviewDailyStat, []entities.UsageOverviewHealthStat, int64) {
	hourly := make(map[usageOverviewStatsKey]*entities.UsageOverviewHourlyStat)
	daily := make(map[usageOverviewStatsKey]*entities.UsageOverviewDailyStat)
	health := make(map[usageOverviewHealthStatsKey]*entities.UsageOverviewHealthStat)
	maxEventID := int64(0)
	for _, event := range events {
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		apiGroupKey := normalizeUsageOverviewDimension(event.APIGroupKey)
		model := normalizeUsageOverviewDimension(event.Model)
		serviceTier := usageOverviewServiceTier(event.ServiceTier)
		authIndex := normalizeUsageOverviewOptionalDimension(event.AuthIndex)
		modelAlias := ""
		if event.ModelAlias != nil {
			modelAlias = normalizeUsageOverviewOptionalDimension(*event.ModelAlias)
		}
		hourBucket := timestamp.Truncate(time.Hour)
		dayBucket := time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, timestamp.Location())

		hourKey := usageOverviewStatsKey{BucketStart: hourBucket, APIGroupKey: apiGroupKey, Model: model, ServiceTier: serviceTier, AuthIndex: authIndex, ModelAlias: modelAlias}
		if _, ok := hourly[hourKey]; !ok {
			hourly[hourKey] = &entities.UsageOverviewHourlyStat{BucketStart: hourBucket, APIGroupKey: apiGroupKey, Model: model, ServiceTier: serviceTier, AuthIndex: authIndex, ModelAlias: modelAlias}
		}
		addUsageOverviewEventToStats(hourly[hourKey], event)

		dayKey := usageOverviewStatsKey{BucketStart: dayBucket, APIGroupKey: apiGroupKey, Model: model, ServiceTier: serviceTier, AuthIndex: authIndex, ModelAlias: modelAlias}
		if _, ok := daily[dayKey]; !ok {
			daily[dayKey] = &entities.UsageOverviewDailyStat{BucketStart: dayBucket, APIGroupKey: apiGroupKey, Model: model, ServiceTier: serviceTier, AuthIndex: authIndex, ModelAlias: modelAlias}
		}
		addUsageOverviewEventToStats(daily[dayKey], event)

		// health 同时保存短窗口和 7d 视图需要的粒度，查询时按 span_seconds 选择。
		for _, span := range []time.Duration{usageOverviewHealthPresetSpan, usageOverviewHealthDefaultSpan} {
			spanSeconds := int64((span + time.Second - 1) / time.Second)
			bucketStart := timestamp.Truncate(time.Duration(spanSeconds) * time.Second)
			key := usageOverviewHealthStatsKey{BucketStart: bucketStart, SpanSeconds: spanSeconds, APIGroupKey: apiGroupKey}
			if _, ok := health[key]; !ok {
				health[key] = &entities.UsageOverviewHealthStat{BucketStart: bucketStart, SpanSeconds: spanSeconds, APIGroupKey: apiGroupKey}
			}
			if event.Failed {
				health[key].FailureCount++
			} else {
				health[key].SuccessCount++
			}
		}
	}

	hourlyRows := make([]entities.UsageOverviewHourlyStat, 0, len(hourly))
	for _, row := range hourly {
		hourlyRows = append(hourlyRows, *row)
	}
	dailyRows := make([]entities.UsageOverviewDailyStat, 0, len(daily))
	for _, row := range daily {
		dailyRows = append(dailyRows, *row)
	}
	healthRows := make([]entities.UsageOverviewHealthStat, 0, len(health))
	for _, row := range health {
		healthRows = append(healthRows, *row)
	}
	return hourlyRows, dailyRows, healthRows, maxEventID
}

func normalizeUsageOverviewOptionalDimension(value string) string {
	return strings.TrimSpace(value)
}

type usageOverviewTokenStat interface {
	*entities.UsageOverviewHourlyStat | *entities.UsageOverviewDailyStat
}

// addUsageOverviewEventToStats 将单条事件累加到 hourly 或 daily token stats 行。
func addUsageOverviewEventToStats[T usageOverviewTokenStat](row T, event entities.UsageEvent) {
	switch stat := any(row).(type) {
	case *entities.UsageOverviewHourlyStat:
		addUsageOverviewEventToHourlyStat(stat, event)
	case *entities.UsageOverviewDailyStat:
		addUsageOverviewEventToDailyStat(stat, event)
	}
}

// addUsageOverviewEventToHourlyStat 累加请求数、成功失败数和各类 token 到小时行。
func addUsageOverviewEventToHourlyStat(row *entities.UsageOverviewHourlyStat, event entities.UsageEvent) {
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

// addUsageOverviewEventToDailyStat 累加请求数、成功失败数和各类 token 到天行。
func addUsageOverviewEventToDailyStat(row *entities.UsageOverviewDailyStat, event entities.UsageEvent) {
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

// applyUsageOverviewHourlyStats 分批写入小时聚合行，复用 SQLite 参数数量保护。
func applyUsageOverviewHourlyStats(tx *gorm.DB, rows []entities.UsageOverviewHourlyStat, now time.Time) error {
	for start := 0; start < len(rows); start += insertBatchSize(entities.UsageOverviewHourlyStat{}) {
		end := min(start+insertBatchSize(entities.UsageOverviewHourlyStat{}), len(rows))
		for index := start; index < end; index++ {
			if err := applyUsageOverviewHourlyStat(tx, rows[index], now); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyUsageOverviewDailyStats 分批写入天聚合行，复用 SQLite 参数数量保护。
func applyUsageOverviewDailyStats(tx *gorm.DB, rows []entities.UsageOverviewDailyStat, now time.Time) error {
	for start := 0; start < len(rows); start += insertBatchSize(entities.UsageOverviewDailyStat{}) {
		end := min(start+insertBatchSize(entities.UsageOverviewDailyStat{}), len(rows))
		for index := start; index < end; index++ {
			if err := applyUsageOverviewDailyStat(tx, rows[index], now); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyUsageOverviewHealthStats 分批写入 health 聚合行，复用 SQLite 参数数量保护。
func applyUsageOverviewHealthStats(tx *gorm.DB, rows []entities.UsageOverviewHealthStat, now time.Time) error {
	for start := 0; start < len(rows); start += insertBatchSize(entities.UsageOverviewHealthStat{}) {
		end := min(start+insertBatchSize(entities.UsageOverviewHealthStat{}), len(rows))
		for index := start; index < end; index++ {
			if err := applyUsageOverviewHealthStat(tx, rows[index], now); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyUsageOverviewHourlyStat 使用 update-first 写入小时 stats，避免 upsert 冲突路径消耗自增 ID。
func applyUsageOverviewHourlyStat(tx *gorm.DB, row entities.UsageOverviewHourlyStat, now time.Time) error {
	updates := usageOverviewTokenStatUpdates(row.RequestCount, row.SuccessCount, row.FailureCount, row.InputTokens, row.OutputTokens, row.ReasoningTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, row.TotalTokens, now)
	result := tx.Model(&entities.UsageOverviewHourlyStat{}).Where("bucket_start = ? AND api_group_key = ? AND model = ? AND service_tier = ? AND auth_index = ? AND model_alias = ?", timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.ServiceTier, row.AuthIndex, row.ModelAlias).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage overview hourly stat: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := tx.Create(&row).Error; err != nil {
		if retryErr := tx.Model(&entities.UsageOverviewHourlyStat{}).Where("bucket_start = ? AND api_group_key = ? AND model = ? AND service_tier = ? AND auth_index = ? AND model_alias = ?", timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.ServiceTier, row.AuthIndex, row.ModelAlias).Updates(updates).Error; retryErr != nil {
			return fmt.Errorf("insert usage overview hourly stat: %w; retry update: %v", err, retryErr)
		}
	}
	return nil
}

// applyUsageOverviewDailyStat 使用 update-first 写入天 stats，支撑长窗口完整天查询。
func applyUsageOverviewDailyStat(tx *gorm.DB, row entities.UsageOverviewDailyStat, now time.Time) error {
	updates := usageOverviewTokenStatUpdates(row.RequestCount, row.SuccessCount, row.FailureCount, row.InputTokens, row.OutputTokens, row.ReasoningTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, row.TotalTokens, now)
	result := tx.Model(&entities.UsageOverviewDailyStat{}).Where("bucket_start = ? AND api_group_key = ? AND model = ? AND service_tier = ? AND auth_index = ? AND model_alias = ?", timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.ServiceTier, row.AuthIndex, row.ModelAlias).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage overview daily stat: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := tx.Create(&row).Error; err != nil {
		if retryErr := tx.Model(&entities.UsageOverviewDailyStat{}).Where("bucket_start = ? AND api_group_key = ? AND model = ? AND service_tier = ? AND auth_index = ? AND model_alias = ?", timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.ServiceTier, row.AuthIndex, row.ModelAlias).Updates(updates).Error; retryErr != nil {
			return fmt.Errorf("insert usage overview daily stat: %w; retry update: %v", err, retryErr)
		}
	}
	return nil
}

// applyUsageOverviewHealthStat 使用 update-first 写入 health stats，按 span/API/bucket 累计成功失败数。
func applyUsageOverviewHealthStat(tx *gorm.DB, row entities.UsageOverviewHealthStat, now time.Time) error {
	updates := map[string]any{
		"success_count": gorm.Expr("success_count + ?", row.SuccessCount),
		"failure_count": gorm.Expr("failure_count + ?", row.FailureCount),
		"updated_at":    timeutil.FormatStorageTime(now),
	}
	result := tx.Model(&entities.UsageOverviewHealthStat{}).Where("bucket_start = ? AND span_seconds = ? AND api_group_key = ?", timeutil.FormatStorageTime(row.BucketStart), row.SpanSeconds, row.APIGroupKey).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage overview health stat: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := tx.Create(&row).Error; err != nil {
		if retryErr := tx.Model(&entities.UsageOverviewHealthStat{}).Where("bucket_start = ? AND span_seconds = ? AND api_group_key = ?", timeutil.FormatStorageTime(row.BucketStart), row.SpanSeconds, row.APIGroupKey).Updates(updates).Error; retryErr != nil {
			return fmt.Errorf("insert usage overview health stat: %w; retry update: %v", err, retryErr)
		}
	}
	return nil
}

// usageOverviewTokenStatUpdates 生成 hourly/daily 共用的累加更新表达式。
func usageOverviewTokenStatUpdates(requestCount, successCount, failureCount, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheReadTokens, cacheCreationTokens, totalTokens int64, now time.Time) map[string]any {
	return map[string]any{
		"request_count":         gorm.Expr("request_count + ?", requestCount),
		"success_count":         gorm.Expr("success_count + ?", successCount),
		"failure_count":         gorm.Expr("failure_count + ?", failureCount),
		"input_tokens":          gorm.Expr("input_tokens + ?", inputTokens),
		"output_tokens":         gorm.Expr("output_tokens + ?", outputTokens),
		"reasoning_tokens":      gorm.Expr("reasoning_tokens + ?", reasoningTokens),
		"cached_tokens":         gorm.Expr("cached_tokens + ?", cachedTokens),
		"cache_read_tokens":     gorm.Expr("cache_read_tokens + ?", cacheReadTokens),
		"cache_creation_tokens": gorm.Expr("cache_creation_tokens + ?", cacheCreationTokens),
		"total_tokens":          gorm.Expr("total_tokens + ?", totalTokens),
		"updated_at":            timeutil.FormatStorageTime(now),
	}
}
