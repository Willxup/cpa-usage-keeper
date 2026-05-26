package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// InsertQuotaCycleSnapshot 持久化一条 quota 窗口快照。调用方传入 captured_at；
// 内部归一化为存储时区。
func InsertQuotaCycleSnapshot(ctx context.Context, db *gorm.DB, snapshot entities.QuotaCycleSnapshot) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	snapshot.Provider = strings.TrimSpace(snapshot.Provider)
	snapshot.AuthIndex = strings.TrimSpace(snapshot.AuthIndex)
	if snapshot.Provider == "" || snapshot.AuthIndex == "" || snapshot.WindowSeconds <= 0 {
		return fmt.Errorf("provider / auth_index / window_seconds are required")
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now()
	}
	snapshot.CapturedAt = timeutil.NormalizeStorageTime(snapshot.CapturedAt)
	snapshot.ResetAt = timeutil.NormalizeStorageTime(snapshot.ResetAt)
	if err := db.WithContext(ctx).Create(&snapshot).Error; err != nil {
		return fmt.Errorf("insert quota cycle snapshot: %w", err)
	}
	return nil
}

// LatestQuotaCycleSnapshot 返回 (provider, auth_index, window_seconds) 下最新一次 snapshot；
// 若无返回 gorm.ErrRecordNotFound。
func LatestQuotaCycleSnapshot(ctx context.Context, db *gorm.DB, provider, authIndex string, windowSeconds int64) (entities.QuotaCycleSnapshot, error) {
	var snapshot entities.QuotaCycleSnapshot
	if db == nil {
		return snapshot, fmt.Errorf("database is nil")
	}
	if err := db.WithContext(ctx).
		Where("provider = ? AND auth_index = ? AND window_seconds = ?", provider, authIndex, windowSeconds).
		Order("reset_at DESC, captured_at DESC, id DESC").
		Limit(1).
		Take(&snapshot).Error; err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

// ListDistinctResetAt 返回 (provider, auth_index, window_seconds) 下按时间序去重的 reset_at 列表，
// 用于推断历史 cycle 边界。结果按 reset_at DESC 排序。
func ListDistinctResetAt(ctx context.Context, db *gorm.DB, provider, authIndex string, windowSeconds int64, limit int) ([]time.Time, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if limit <= 0 {
		limit = 50
	}
	var resetAts []time.Time
	if err := db.WithContext(ctx).
		Model(&entities.QuotaCycleSnapshot{}).
		Where("provider = ? AND auth_index = ? AND window_seconds = ?", provider, authIndex, windowSeconds).
		Distinct("reset_at").
		Order("reset_at DESC").
		Limit(limit).
		Pluck("reset_at", &resetAts).Error; err != nil {
		return nil, fmt.Errorf("list distinct reset_at: %w", err)
	}
	return resetAts, nil
}

// LatestSnapshotForEachAuthIndex 返回每个 auth_index 最新一次 snapshot (按 id DESC, 等价于 captured_at DESC)。
// 用于 "当前 cycle" 概览。
func LatestSnapshotForEachAuthIndex(ctx context.Context, db *gorm.DB, provider string, windowSeconds int64) ([]entities.QuotaCycleSnapshot, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	var snapshots []entities.QuotaCycleSnapshot
	subQuery := db.WithContext(ctx).Model(&entities.QuotaCycleSnapshot{}).
		Select("MAX(id)").
		Where("provider = ? AND window_seconds = ?", provider, windowSeconds).
		Group("auth_index")
	if err := db.WithContext(ctx).
		Where("id IN (?)", subQuery).
		Order("auth_index ASC").
		Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("list latest snapshots per auth_index: %w", err)
	}
	return snapshots, nil
}

// AuthIndexTokenAggregate 是按 model 拆分的 token 用量聚合。
type AuthIndexTokenAggregate struct {
	Model               string
	ModelAlias          string
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	RequestCount        int64
}

// SumUsageTokensByModelForAuthIndex 在给定时间窗内按 model 汇总该 (provider, auth_index) 的 token 使用量。
// failed 事件不计入。结果用于 × model_price_settings 得到 $.
func SumUsageTokensByModelForAuthIndex(ctx context.Context, db *gorm.DB, provider, authIndex string, startInclusive, endExclusive time.Time) ([]AuthIndexTokenAggregate, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	provider = strings.TrimSpace(provider)
	authIndex = strings.TrimSpace(authIndex)
	if provider == "" || authIndex == "" {
		return nil, fmt.Errorf("provider and auth_index are required")
	}
	startStorage := timeutil.NormalizeStorageTime(startInclusive)
	endStorage := timeutil.NormalizeStorageTime(endExclusive)
	type row struct {
		Model               string
		ModelAlias          *string
		InputTokens         int64
		OutputTokens        int64
		ReasoningTokens     int64
		CachedTokens        int64
		CacheReadTokens     int64
		CacheCreationTokens int64
		TotalTokens         int64
		RequestCount        int64
	}
	var rows []row
	if err := db.WithContext(ctx).
		Model(&entities.UsageEvent{}).
		Select(
			"model AS model",
			"MAX(model_alias) AS model_alias",
			"COALESCE(SUM(input_tokens), 0) AS input_tokens",
			"COALESCE(SUM(output_tokens), 0) AS output_tokens",
			"COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens",
			"COALESCE(SUM(cached_tokens), 0) AS cached_tokens",
			"COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens",
			"COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens",
			"COALESCE(SUM(total_tokens), 0) AS total_tokens",
			"COUNT(1) AS request_count",
		).
		Where("provider = ? AND auth_index = ?", provider, authIndex).
		Where("failed = ?", false).
		Where("timestamp >= ? AND timestamp < ?", startStorage, endStorage).
		Group("model").
		Order("model ASC").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("sum usage tokens by model: %w", err)
	}
	results := make([]AuthIndexTokenAggregate, 0, len(rows))
	for _, r := range rows {
		aggregate := AuthIndexTokenAggregate{
			Model:               r.Model,
			InputTokens:         r.InputTokens,
			OutputTokens:        r.OutputTokens,
			ReasoningTokens:     r.ReasoningTokens,
			CachedTokens:        r.CachedTokens,
			CacheReadTokens:     r.CacheReadTokens,
			CacheCreationTokens: r.CacheCreationTokens,
			TotalTokens:         r.TotalTokens,
			RequestCount:        r.RequestCount,
		}
		if r.ModelAlias != nil {
			aggregate.ModelAlias = *r.ModelAlias
		}
		results = append(results, aggregate)
	}
	return results, nil
}
