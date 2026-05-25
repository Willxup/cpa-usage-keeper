package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const usageModelAggregationCheckpointName = "usage_models"

type usageModelAggregateKey struct {
	model     string
	authType  string
	authIndex string
}

type usageModelAggregate struct {
	Model            string
	AuthType         string
	AuthIndex        string
	RequestCount     int64
	FirstUsedAt      time.Time
	LastUsedAt       time.Time
	LastUsageEventID int64
}

// AggregateUsageModels 按 usage_events 自增 ID 增量推进定价模型索引。
func AggregateUsageModels(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	now = timeutil.NormalizeStorageTime(now)
	batchSize := insertBatchSize(entities.UsageEvent{})
	for {
		processed, err := aggregateUsageModelsBatch(ctx, db, now, batchSize)
		if err != nil {
			return err
		}
		if processed < batchSize {
			return nil
		}
	}
}

// HasPendingUsageModelAggregation 用轻量 ID cursor 判断 usage_models 是否落后。
func HasPendingUsageModelAggregation(ctx context.Context, db *gorm.DB) (bool, error) {
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
	err := db.WithContext(ctx).Where("name = ?", usageModelAggregationCheckpointName).Take(&checkpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("load usage model aggregation checkpoint: %w", err)
	}
	return checkpoint.LastAggregatedUsageEventID < maxEventID, nil
}

func aggregateUsageModelsBatch(ctx context.Context, db *gorm.DB, now time.Time, limit int) (int, error) {
	processed := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint, err := getOrCreateUsageModelAggregationCheckpoint(tx, now)
		if err != nil {
			return err
		}

		var events []entities.UsageEvent
		if err := tx.Select("id, model, auth_type, auth_index, timestamp").
			Where("id > ?", checkpoint.LastAggregatedUsageEventID).
			Order("id asc").
			Limit(limit).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage model aggregation events: %w", err)
		}
		if len(events) == 0 {
			processed = 0
			return nil
		}

		for _, aggregate := range buildUsageModelAggregates(events) {
			if err := applyUsageModelAggregate(tx, aggregate, now); err != nil {
				return err
			}
		}
		if err := tx.Model(&entities.UsageOverviewAggregationCheckpoint{}).
			Where("id = ?", checkpoint.ID).
			Updates(map[string]any{
				"last_aggregated_usage_event_id": maxUsageModelEventID(events),
				"stats_updated_at":               timeutil.FormatStorageTime(now),
			}).Error; err != nil {
			return fmt.Errorf("update usage model aggregation checkpoint: %w", err)
		}
		processed = len(events)
		return nil
	})
	return processed, err
}

func getOrCreateUsageModelAggregationCheckpoint(tx *gorm.DB, now time.Time) (entities.UsageOverviewAggregationCheckpoint, error) {
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	err := tx.Where("name = ?", usageModelAggregationCheckpointName).Take(&checkpoint).Error
	if err == nil {
		return checkpoint, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return checkpoint, fmt.Errorf("load usage model aggregation checkpoint: %w", err)
	}

	lastAggregatedID, err := initialUsageModelAggregationCheckpointID(tx)
	if err != nil {
		return checkpoint, err
	}
	updatedAt := now
	checkpoint = entities.UsageOverviewAggregationCheckpoint{
		Name:                       usageModelAggregationCheckpointName,
		LastAggregatedUsageEventID: lastAggregatedID,
		StatsUpdatedAt:             &updatedAt,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	if err := tx.Create(&checkpoint).Error; err != nil {
		if retryErr := tx.Where("name = ?", usageModelAggregationCheckpointName).Take(&checkpoint).Error; retryErr == nil {
			return checkpoint, nil
		}
		return checkpoint, fmt.Errorf("create usage model aggregation checkpoint: %w", err)
	}
	return checkpoint, nil
}

func initialUsageModelAggregationCheckpointID(tx *gorm.DB) (int64, error) {
	var maxUsageModelEventID int64
	if err := tx.Model(&entities.UsageModel{}).Select("COALESCE(MAX(last_usage_event_id), 0)").Scan(&maxUsageModelEventID).Error; err != nil {
		return 0, fmt.Errorf("load existing usage model checkpoint id: %w", err)
	}
	return maxUsageModelEventID, nil
}

func maxUsageModelEventID(events []entities.UsageEvent) int64 {
	maxEventID := int64(0)
	for _, event := range events {
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
	}
	return maxEventID
}

func buildUsageModelAggregates(events []entities.UsageEvent) []usageModelAggregate {
	aggregates := make(map[usageModelAggregateKey]*usageModelAggregate)
	for _, event := range events {
		model := strings.TrimSpace(event.Model)
		if model == "" {
			continue
		}
		authType := strings.TrimSpace(event.AuthType)
		authIndex := strings.TrimSpace(event.AuthIndex)
		key := usageModelAggregateKey{model: model, authType: authType, authIndex: authIndex}
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		aggregate := aggregates[key]
		if aggregate == nil {
			aggregate = &usageModelAggregate{
				Model:            model,
				AuthType:         authType,
				AuthIndex:        authIndex,
				FirstUsedAt:      timestamp,
				LastUsedAt:       timestamp,
				LastUsageEventID: event.ID,
			}
			aggregates[key] = aggregate
		}
		aggregate.RequestCount++
		if timestamp.Before(aggregate.FirstUsedAt) {
			aggregate.FirstUsedAt = timestamp
		}
		if timestamp.After(aggregate.LastUsedAt) {
			aggregate.LastUsedAt = timestamp
		}
		if event.ID > aggregate.LastUsageEventID {
			aggregate.LastUsageEventID = event.ID
		}
	}

	rows := make([]usageModelAggregate, 0, len(aggregates))
	for _, aggregate := range aggregates {
		rows = append(rows, *aggregate)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Model != rows[j].Model {
			return rows[i].Model < rows[j].Model
		}
		if rows[i].AuthType != rows[j].AuthType {
			return rows[i].AuthType < rows[j].AuthType
		}
		return rows[i].AuthIndex < rows[j].AuthIndex
	})
	return rows
}

func applyUsageModelAggregate(tx *gorm.DB, aggregate usageModelAggregate, now time.Time) error {
	firstUsedAt := timeutil.FormatStorageTime(aggregate.FirstUsedAt)
	lastUsedAt := timeutil.FormatStorageTime(aggregate.LastUsedAt)
	updates := map[string]any{
		"request_count":       gorm.Expr("request_count + ?", aggregate.RequestCount),
		"first_used_at":       gorm.Expr("CASE WHEN first_used_at IS NULL OR first_used_at > ? THEN ? ELSE first_used_at END", firstUsedAt, firstUsedAt),
		"last_used_at":        gorm.Expr("CASE WHEN last_used_at IS NULL OR last_used_at < ? THEN ? ELSE last_used_at END", lastUsedAt, lastUsedAt),
		"last_usage_event_id": gorm.Expr("CASE WHEN last_usage_event_id < ? THEN ? ELSE last_usage_event_id END", aggregate.LastUsageEventID, aggregate.LastUsageEventID),
		"updated_at":          timeutil.FormatStorageTime(now),
	}
	result := tx.Model(&entities.UsageModel{}).
		Where("model = ? AND auth_type = ? AND auth_index = ?", aggregate.Model, aggregate.AuthType, aggregate.AuthIndex).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage model: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}

	first := aggregate.FirstUsedAt
	last := aggregate.LastUsedAt
	row := entities.UsageModel{
		Model:            aggregate.Model,
		AuthType:         aggregate.AuthType,
		AuthIndex:        aggregate.AuthIndex,
		RequestCount:     aggregate.RequestCount,
		FirstUsedAt:      &first,
		LastUsedAt:       &last,
		LastUsageEventID: aggregate.LastUsageEventID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := tx.Create(&row).Error; err != nil {
		retry := tx.Model(&entities.UsageModel{}).
			Where("model = ? AND auth_type = ? AND auth_index = ?", aggregate.Model, aggregate.AuthType, aggregate.AuthIndex).
			Updates(updates)
		if retry.Error != nil {
			return fmt.Errorf("insert usage model: %w; retry update: %v", err, retry.Error)
		}
	}
	return nil
}
