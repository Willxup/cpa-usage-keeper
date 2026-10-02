package migration

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// legacyUsageOverviewEvent 只映射已发布五维迁移读取的原始列。
type legacyUsageOverviewEvent struct {
	ID                  int64
	APIGroupKey         string
	Model               string
	ModelAlias          *string
	AuthIndex           string
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
	Timestamp           time.Time `gorm:"serializer:storageTime"`
	Failed              bool
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

func (legacyUsageOverviewEvent) TableName() string { return "usage_events" }

type legacyUsageOverviewKey struct {
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

type legacyUsageOverviewCounts struct {
	RequestCount        int64
	SuccessCount        int64
	FailureCount        int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

type legacyUsageOverviewRow struct {
	Key    legacyUsageOverviewKey
	Counts legacyUsageOverviewCounts
}

// 旧回放只累计请求与 Token；未来运行时费用计算不能改变这个迁移的写列。
func buildLegacyUsageOverviewRows(events []legacyUsageOverviewEvent) ([]legacyUsageOverviewRow, []legacyUsageOverviewRow, int64) {
	hourly := make(map[legacyUsageOverviewKey]*legacyUsageOverviewCounts)
	daily := make(map[legacyUsageOverviewKey]*legacyUsageOverviewCounts)
	var maxEventID int64
	for _, event := range events {
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
		key := legacyUsageOverviewKey{
			APIGroupKey:         legacyRequiredOverviewDimension(event.APIGroupKey),
			Model:               legacyRequiredOverviewDimension(event.Model),
			AuthIndex:           strings.TrimSpace(event.AuthIndex),
			ServiceTier:         strings.TrimSpace(event.ServiceTier),
			ResponseServiceTier: strings.TrimSpace(event.ResponseServiceTier),
			ReasoningEffort:     strings.TrimSpace(event.ReasoningEffort),
			Endpoint:            strings.TrimSpace(event.Endpoint),
			ExecutorType:        strings.TrimSpace(event.ExecutorType),
		}
		if event.ModelAlias != nil {
			key.ModelAlias = strings.TrimSpace(*event.ModelAlias)
		}
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		hourKey := key
		hourKey.BucketStart = timestamp.Truncate(time.Hour)
		dayKey := key
		dayKey.BucketStart = time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, timestamp.Location())
		addLegacyUsageOverviewEvent(hourly, hourKey, event)
		addLegacyUsageOverviewEvent(daily, dayKey, event)
	}
	return sortedLegacyUsageOverviewRows(hourly), sortedLegacyUsageOverviewRows(daily), maxEventID
}

func legacyRequiredOverviewDimension(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "unknown"
}

func addLegacyUsageOverviewEvent(rows map[legacyUsageOverviewKey]*legacyUsageOverviewCounts, key legacyUsageOverviewKey, event legacyUsageOverviewEvent) {
	counts := rows[key]
	if counts == nil {
		counts = &legacyUsageOverviewCounts{}
		rows[key] = counts
	}
	counts.RequestCount++
	if event.Failed {
		counts.FailureCount++
	} else {
		counts.SuccessCount++
	}
	counts.InputTokens += event.InputTokens
	counts.OutputTokens += event.OutputTokens
	counts.ReasoningTokens += event.ReasoningTokens
	counts.CachedTokens += event.CachedTokens
	counts.CacheReadTokens += event.CacheReadTokens
	counts.CacheCreationTokens += event.CacheCreationTokens
	counts.TotalTokens += event.TotalTokens
}

func sortedLegacyUsageOverviewRows(rows map[legacyUsageOverviewKey]*legacyUsageOverviewCounts) []legacyUsageOverviewRow {
	result := make([]legacyUsageOverviewRow, 0, len(rows))
	for key, counts := range rows {
		result = append(result, legacyUsageOverviewRow{Key: key, Counts: *counts})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i].Key, result[j].Key
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
	})
	return result
}

const legacyUsageOverviewDimensionsPredicate = "bucket_start = ? AND api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ? AND service_tier = ? AND response_service_tier = ? AND reasoning_effort = ? AND endpoint = ? AND executor_type = ?"

// applyLegacyUsageOverviewRows 只写旧计数与 Token 列。调用方持有整页事务，使小时、日和 checkpoint 共同回滚。
// 保留已发布的先 UPDATE、未命中 INSERT、唯一键竞争后重试 UPDATE 的顺序。
func applyLegacyUsageOverviewRows(tx *gorm.DB, table string, rows []legacyUsageOverviewRow, now time.Time) error {
	for _, row := range rows {
		key, counts := row.Key, row.Counts
		stamp := timeutil.FormatStorageTime(now)
		updates := map[string]any{
			"request_count":         gorm.Expr("request_count + ?", counts.RequestCount),
			"success_count":         gorm.Expr("success_count + ?", counts.SuccessCount),
			"failure_count":         gorm.Expr("failure_count + ?", counts.FailureCount),
			"input_tokens":          gorm.Expr("input_tokens + ?", counts.InputTokens),
			"output_tokens":         gorm.Expr("output_tokens + ?", counts.OutputTokens),
			"reasoning_tokens":      gorm.Expr("reasoning_tokens + ?", counts.ReasoningTokens),
			"cached_tokens":         gorm.Expr("cached_tokens + ?", counts.CachedTokens),
			"cache_read_tokens":     gorm.Expr("cache_read_tokens + ?", counts.CacheReadTokens),
			"cache_creation_tokens": gorm.Expr("cache_creation_tokens + ?", counts.CacheCreationTokens),
			"total_tokens":          gorm.Expr("total_tokens + ?", counts.TotalTokens),
			"updated_at":            stamp,
		}
		args := []any{timeutil.FormatStorageTime(key.BucketStart), key.APIGroupKey, key.Model, key.AuthIndex, key.ModelAlias, key.ServiceTier, key.ResponseServiceTier, key.ReasoningEffort, key.Endpoint, key.ExecutorType}
		update := func() *gorm.DB {
			return tx.Table(table).Where(legacyUsageOverviewDimensionsPredicate, args...).Updates(updates)
		}
		result := update()
		if result.Error != nil {
			return fmt.Errorf("update %s: %w", table, result.Error)
		}
		if result.RowsAffected > 0 {
			continue
		}
		// Map 写入固定历史列，不受未来实体追加字段影响。
		insert := map[string]any{
			"bucket_start": args[0], "api_group_key": key.APIGroupKey, "model": key.Model,
			"auth_index": key.AuthIndex, "model_alias": key.ModelAlias,
			"service_tier": key.ServiceTier, "response_service_tier": key.ResponseServiceTier,
			"reasoning_effort": key.ReasoningEffort, "endpoint": key.Endpoint, "executor_type": key.ExecutorType,
			"request_count": counts.RequestCount, "success_count": counts.SuccessCount, "failure_count": counts.FailureCount,
			"input_tokens": counts.InputTokens, "output_tokens": counts.OutputTokens, "reasoning_tokens": counts.ReasoningTokens,
			"cached_tokens": counts.CachedTokens, "cache_read_tokens": counts.CacheReadTokens,
			"cache_creation_tokens": counts.CacheCreationTokens, "total_tokens": counts.TotalTokens,
			"created_at": stamp, "updated_at": stamp,
		}
		if insertErr := tx.Table(table).Create(insert).Error; insertErr != nil {
			retry := update()
			if retry.Error != nil {
				return fmt.Errorf("insert %s: %w; retry update: %v", table, insertErr, retry.Error)
			}
			if retry.RowsAffected == 0 {
				return fmt.Errorf("insert %s: %w; retry update matched no existing row", table, insertErr)
			}
		}
	}
	return nil
}
