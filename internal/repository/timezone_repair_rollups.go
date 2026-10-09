package repository

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository/activitystore"
	"cpa-usage-keeper/internal/repository/latencystore"
	"cpa-usage-keeper/internal/repository/overviewstore"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func rebuildTimezoneRollups(tx *gorm.DB, now time.Time) error {
	for _, model := range []any{
		&entities.UsageOverviewHourlyStat{},
		&entities.UsageOverviewDailyStat{},
		&entities.UsageActivityStat{},
		&entities.UsageLatencyStat{},
	} {
		if tx.Migrator().HasTable(model) {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(model).Error; err != nil {
				return fmt.Errorf("clear timezone rollup %T: %w", model, err)
			}
		}
	}

	if err := resetTimezoneRollupCheckpoints(tx, now); err != nil {
		return err
	}
	if err := rebuildTimezoneRollupsFromTable(tx, "usage_events", now); err != nil {
		return err
	}
	if tx.Migrator().HasTable(&entities.UsageEventArchive{}) {
		if err := rebuildTimezoneRollupsFromTable(tx, "usage_events_archive", now); err != nil {
			return err
		}
	}
	return advanceTimezoneRollupCheckpoints(tx, now)
}

func resetTimezoneRollupCheckpoints(tx *gorm.DB, now time.Time) error {
	if !tx.Migrator().HasTable(&entities.UsageAggregationCheckpoint{}) {
		return nil
	}
	for _, name := range []entities.UsageAggregationCheckpointName{
		entities.UsageAggregationCheckpointOverview,
		entities.UsageAggregationCheckpointActivity,
		entities.UsageAggregationCheckpointLatency,
	} {
		checkpoint := entities.UsageAggregationCheckpoint{Name: name, CreatedAt: now, UpdatedAt: now}
		if err := tx.Where("name = ?", name).FirstOrCreate(&checkpoint).Error; err != nil {
			return fmt.Errorf("ensure timezone rollup checkpoint %q: %w", name, err)
		}
	}
	if err := tx.Model(&entities.UsageAggregationCheckpoint{}).
		Where("name IN ?", []entities.UsageAggregationCheckpointName{
			entities.UsageAggregationCheckpointOverview,
			entities.UsageAggregationCheckpointActivity,
			entities.UsageAggregationCheckpointLatency,
		}).Updates(map[string]any{
		"stats_updated_at": nil,
		"updated_at":       timeutil.FormatStorageTime(now),
	}).Error; err != nil {
		return fmt.Errorf("reset timezone rollup checkpoints: %w", err)
	}
	return nil
}

func rebuildTimezoneRollupsFromTable(tx *gorm.DB, table string, now time.Time) error {
	if !tx.Migrator().HasTable(table) {
		return nil
	}
	const batchSize = 1000
	lastID := int64(0)
	for {
		var events []entities.UsageEvent
		query := tx.Table(table).
			Select(entities.UsageAggregationEventProjectionColumns).
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(batchSize)
		if err := query.Find(&events).Error; err != nil {
			return fmt.Errorf("read %s for timezone rollups: %w", table, err)
		}
		if len(events) == 0 {
			return nil
		}
		hourly, daily, _ := overview.BuildRows(events)
		if err := overviewstore.ApplyRows(tx, hourly, daily, now); err != nil {
			return fmt.Errorf("rebuild overview timezone rollups from %s: %w", table, err)
		}
		activityRows, err := activity.BuildRows(events, now)
		if err != nil {
			return fmt.Errorf("build activity timezone rollups from %s: %w", table, err)
		}
		if err := activitystore.ApplyRows(tx, activityRows, now); err != nil {
			return fmt.Errorf("rebuild activity timezone rollups from %s: %w", table, err)
		}
		latencyRows, err := latency.BuildRows(events, now)
		if err != nil {
			return fmt.Errorf("build latency timezone rollups from %s: %w", table, err)
		}
		if err := latencystore.ApplyRows(tx, latencyRows, now); err != nil {
			return fmt.Errorf("rebuild latency timezone rollups from %s: %w", table, err)
		}
		lastID = events[len(events)-1].ID
	}
}

func advanceTimezoneRollupCheckpoints(tx *gorm.DB, now time.Time) error {
	var maxID int64
	query := "SELECT COALESCE(MAX(id), 0) FROM usage_events"
	if tx.Migrator().HasTable(&entities.UsageEventArchive{}) {
		query = "SELECT COALESCE(MAX(id), 0) FROM (SELECT id FROM usage_events UNION ALL SELECT id FROM usage_events_archive)"
	}
	if err := tx.Raw(query).Scan(&maxID).Error; err != nil {
		return fmt.Errorf("load timezone rollup checkpoint: %w", err)
	}
	if err := tx.Model(&entities.UsageAggregationCheckpoint{}).
		Where("name IN ?", []entities.UsageAggregationCheckpointName{
			entities.UsageAggregationCheckpointOverview,
			entities.UsageAggregationCheckpointActivity,
			entities.UsageAggregationCheckpointLatency,
		}).Updates(map[string]any{
		"last_aggregated_usage_event_id": gorm.Expr("MAX(last_aggregated_usage_event_id, ?)", maxID),
		"stats_updated_at":               timeutil.FormatStorageTime(now),
		"updated_at":                     timeutil.FormatStorageTime(now),
	}).Error; err != nil {
		return fmt.Errorf("advance timezone rollup checkpoints: %w", err)
	}
	return nil
}
