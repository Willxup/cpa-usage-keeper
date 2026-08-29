package ranking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

var ErrInvalidMetrics = errors.New("invalid ranking metrics")

type Aggregator struct {
	db *gorm.DB
}

func NewAggregator(db *gorm.DB) *Aggregator {
	return &Aggregator{db: db}
}

// AggregateDay 只处理中心时区的一个自然日，并在 SQLite 内完成固定五分钟分桶与最终汇总。
func (a *Aggregator) AggregateDay(ctx context.Context, start, end time.Time) (Metrics, error) {
	if a == nil || a.db == nil {
		return Metrics{}, fmt.Errorf("ranking aggregator is not configured")
	}
	dayEnd := start.AddDate(0, 0, 1)
	if start.IsZero() || end.IsZero() || start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 || end.Before(start) || end.After(dayEnd) {
		return Metrics{}, fmt.Errorf("ranking aggregation requires one center calendar date range")
	}
	if end.Equal(start) {
		return Metrics{}, nil
	}
	timePredicate, timeArguments := rankingTimeRangePredicate(start, end)

	var metrics Metrics
	query := fmt.Sprintf(`
WITH five_minute_buckets AS (
	SELECT
		CAST(strftime('%%s', timestamp) AS INTEGER) / 300 AS bucket_key,
		COUNT(*) AS request_count,
		SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END) AS success_count,
		SUM(CASE WHEN failed <> 0 THEN 1 ELSE 0 END) AS failure_count,
		SUM(input_tokens) AS input_tokens,
		SUM(output_tokens) AS output_tokens,
		SUM(reasoning_tokens) AS reasoning_tokens,
		SUM(cache_read_tokens) AS cache_read_tokens,
		SUM(cache_creation_tokens) AS cache_creation_tokens,
		SUM(total_tokens) AS total_tokens,
		SUM(CASE WHEN failed = 0 AND ttft_ms IS NOT NULL AND ttft_ms > 0 AND latency_ms > 0 THEN ttft_ms ELSE 0 END) AS ttft_sum_ms,
		SUM(CASE WHEN failed = 0 AND ttft_ms IS NOT NULL AND ttft_ms > 0 AND latency_ms > 0 THEN 1 ELSE 0 END) AS ttft_sample_count,
		SUM(CASE WHEN failed = 0 AND ttft_ms IS NOT NULL AND ttft_ms > 0 AND latency_ms > 0 THEN latency_ms ELSE 0 END) AS latency_sum_ms,
		SUM(CASE WHEN failed = 0 AND ttft_ms IS NOT NULL AND ttft_ms > 0 AND latency_ms > 0 THEN 1 ELSE 0 END) AS latency_sample_count
	FROM usage_events
	WHERE %s
	GROUP BY bucket_key
)
SELECT
	COALESCE(SUM(request_count), 0) AS request_count,
	COALESCE(SUM(success_count), 0) AS success_count,
	COALESCE(SUM(failure_count), 0) AS failure_count,
	COALESCE(SUM(input_tokens), 0) AS input_tokens,
	COALESCE(SUM(output_tokens), 0) AS output_tokens,
	COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
	COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
	COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
	COALESCE(SUM(total_tokens), 0) AS total_tokens,
	COALESCE(SUM(ttft_sum_ms), 0) AS ttft_sum_ms,
	COALESCE(SUM(ttft_sample_count), 0) AS ttft_sample_count,
	COALESCE(SUM(latency_sum_ms), 0) AS latency_sum_ms,
	COALESCE(SUM(latency_sample_count), 0) AS latency_sample_count,
	COALESCE(MAX(request_count), 0) AS peak_5m_request_count,
	COALESCE(MAX(total_tokens), 0) AS peak_5m_total_tokens
FROM five_minute_buckets`, timePredicate)
	// CTE 以 WITH 开头，dbresolver 无法自动识别为 SELECT，必须显式路由到只读池。
	if err := a.db.Clauses(dbresolver.Read).WithContext(ctx).Raw(
		query,
		timeArguments...,
	).Scan(&metrics).Error; err != nil {
		return Metrics{}, fmt.Errorf("aggregate ranking usage day: %w", err)
	}
	if err := ValidateMetrics(metrics); err != nil {
		return Metrics{}, err
	}
	return metrics, nil
}

func (a *Aggregator) LatestEventID(ctx context.Context, start, end time.Time) (int64, error) {
	if a == nil || a.db == nil {
		return 0, fmt.Errorf("ranking aggregator is not configured")
	}
	if start.IsZero() || end.IsZero() || start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 || end.Before(start) || end.After(start.AddDate(0, 0, 1)) {
		return 0, fmt.Errorf("ranking latest event range must use one center calendar date")
	}
	timePredicate, timeArguments := rankingTimeRangePredicate(start, end)
	var latestID int64
	if err := a.db.Clauses(dbresolver.Read).WithContext(ctx).
		Model(&entities.UsageEvent{}).
		Where(timePredicate, timeArguments...).
		Select("COALESCE(MAX(id), 0)").
		Scan(&latestID).Error; err != nil {
		return 0, fmt.Errorf("load latest ranking usage event id: %w", err)
	}
	return latestID, nil
}

func rankingTimeRangePredicate(start, end time.Time) (string, []any) {
	// storageTime 使用 Keeper 本地 offset，DST 回拨时文本顺序不等于 instant 顺序。
	// 外层宽范围继续使用 timestamp 索引，strftime 只负责候选行的真实时间复核。
	coarseStart := timeutil.FormatStorageTime(start.Add(-24 * time.Hour))
	coarseEnd := timeutil.FormatStorageTime(end.Add(24 * time.Hour))
	epochExpression := "CAST(strftime('%s', timestamp) AS INTEGER)"
	if end.Nanosecond() == 0 {
		return "timestamp >= ? AND timestamp < ? AND " + epochExpression + " >= ? AND " + epochExpression + " < ?", []any{
			coarseStart, coarseEnd, start.Unix(), end.Unix(),
		}
	}
	return "timestamp >= ? AND timestamp < ? AND " + epochExpression + " >= ? AND (" + epochExpression + " < ? OR (" + epochExpression + " = ? AND timestamp < ?))", []any{
		coarseStart, coarseEnd, start.Unix(), end.Unix(), end.Unix(), timeutil.FormatStorageTime(end),
	}
}

func ValidateMetrics(metrics Metrics) error {
	values := []int64{
		metrics.RequestCount,
		metrics.SuccessCount,
		metrics.FailureCount,
		metrics.InputTokens,
		metrics.OutputTokens,
		metrics.ReasoningTokens,
		metrics.CacheReadTokens,
		metrics.CacheCreationTokens,
		metrics.TotalTokens,
		metrics.TTFTSumMS,
		metrics.TTFTSampleCount,
		metrics.LatencySumMS,
		metrics.LatencySampleCount,
		metrics.Peak5MRequestCount,
		metrics.Peak5MTotalTokens,
	}
	for _, value := range values {
		if value < 0 {
			return ErrInvalidMetrics
		}
	}
	return nil
}
