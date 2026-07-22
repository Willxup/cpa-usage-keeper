package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository/overviewstore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// usageOverviewFiveDimensionsBatchSize 用短事务重放 raw events，避免长期占用 SQLite writer。
	usageOverviewFiveDimensionsBatchSize = 1000
	// usageOverviewMigrationCheckpointName 与运行时继续共享既有 Overview cursor。
	usageOverviewMigrationCheckpointName = "overview"
)

// usageOverviewFiveDimensionsMigration 从当前仍保留的 usage_events 重建五维 hourly/daily rollup。
func usageOverviewFiveDimensionsMigration(db *gorm.DB) error {
	now := timeutil.NormalizeStorageTime(time.Now())

	// 固定 migration 启动时可见的最大 ID；之后新增事件留给正常增量聚合追赶。
	var targetEventID int64
	if db.Migrator().HasTable(&entities.UsageEvent{}) {
		if err := db.Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&targetEventID).Error; err != nil {
			return fmt.Errorf("load usage overview five-dimension target: %w", err)
		}
	}

	// schema、旧数据清空和 checkpoint 归零必须一起提交，失败时保留可用旧表。
	if err := prepareUsageOverviewFiveDimensions(db); err != nil {
		return err
	}

	// 每批同时写入 hourly、daily 并推进同一个 checkpoint；中断后重启会从零安全重建。
	for {
		processed, err := migrateUsageOverviewFiveDimensionsBatch(db, now, targetEventID)
		if err != nil {
			return err
		}
		if processed == 0 {
			break
		}
	}

	// 只有已提交 cursor 到达固定 target 后，外层 runner 才能记录 migration version。
	return verifyUsageOverviewFiveDimensionsTarget(db, targetEventID)
}

func prepareUsageOverviewFiveDimensions(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// AutoMigrate 为两个 rollup 增加五列并创建最终十列唯一索引。
		if err := tx.AutoMigrate(&entities.UsageOverviewHourlyStat{}, &entities.UsageOverviewDailyStat{}); err != nil {
			return fmt.Errorf("create usage overview five-dimension schema: %w", err)
		}
		// checkpoint schema 本次没有变化；只为异常缺表的旧库补建，避免无关表重建。
		if !tx.Migrator().HasTable(&entities.UsageOverviewAggregationCheckpoint{}) {
			if err := tx.AutoMigrate(&entities.UsageOverviewAggregationCheckpoint{}); err != nil {
				return fmt.Errorf("create usage overview aggregation checkpoint schema: %w", err)
			}
		}

		// 最终索引建立后删除旧五列唯一索引，避免继续维护无效重复结构。
		for _, oldIndex := range []struct {
			table string
			name  string
		}{
			{table: "usage_overview_hourly_stats", name: "uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias"},
			{table: "usage_overview_daily_stats", name: "uniq_usage_overview_daily_stats_bucket_api_model_auth_alias"},
		} {
			if tx.Migrator().HasIndex(oldIndex.table, oldIndex.name) {
				if err := tx.Migrator().DropIndex(oldIndex.table, oldIndex.name); err != nil {
					return fmt.Errorf("drop legacy usage overview index %s: %w", oldIndex.name, err)
				}
			}
		}

		// 已有 rollup 不含五维信息，不能保留或与重建结果混合。
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&entities.UsageOverviewHourlyStat{}).Error; err != nil {
			return fmt.Errorf("clear usage overview hourly stats: %w", err)
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&entities.UsageOverviewDailyStat{}).Error; err != nil {
			return fmt.Errorf("clear usage overview daily stats: %w", err)
		}

		// checkpoint 与空 rollup 同事务归零，禁止页面看到新 schema 配旧 cursor。
		checkpoint := entities.UsageOverviewAggregationCheckpoint{Name: usageOverviewMigrationCheckpointName}
		if err := tx.Where("name = ?", usageOverviewMigrationCheckpointName).FirstOrCreate(&checkpoint).Error; err != nil {
			return fmt.Errorf("get usage overview migration checkpoint: %w", err)
		}
		if err := tx.Model(&entities.UsageOverviewAggregationCheckpoint{}).
			Where("id = ?", checkpoint.ID).
			Updates(map[string]any{
				"last_aggregated_usage_event_id": 0,
				"stats_updated_at":               nil,
			}).Error; err != nil {
			return fmt.Errorf("reset usage overview migration checkpoint: %w", err)
		}
		return nil
	})
}

func migrateUsageOverviewFiveDimensionsBatch(db *gorm.DB, now time.Time, targetEventID int64) (int, error) {
	processed := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		var checkpoint entities.UsageOverviewAggregationCheckpoint
		if err := tx.Where("name = ?", usageOverviewMigrationCheckpointName).Take(&checkpoint).Error; err != nil {
			return fmt.Errorf("load usage overview migration checkpoint: %w", err)
		}
		if checkpoint.LastAggregatedUsageEventID >= targetEventID {
			return nil
		}

		// 迁移与运行时读取完全相同的旧计数列和五个新增维度。
		var events []entities.UsageEvent
		if err := tx.Select("id, api_group_key, model, model_alias, auth_index, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, timestamp, failed, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
			Where("id > ? AND id <= ?", checkpoint.LastAggregatedUsageEventID, targetEventID).
			Order("id asc").
			Limit(usageOverviewFiveDimensionsBatchSize).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage overview five-dimension events: %w", err)
		}
		if len(events) == 0 {
			return nil
		}

		hourlyRows, dailyRows, maxEventID := overview.BuildRows(events)
		if err := overviewstore.ApplyRows(tx, hourlyRows, dailyRows, now); err != nil {
			return err
		}
		if err := tx.Model(&entities.UsageOverviewAggregationCheckpoint{}).
			Where("id = ?", checkpoint.ID).
			Updates(map[string]any{
				"last_aggregated_usage_event_id": maxEventID,
				"stats_updated_at":               timeutil.FormatStorageTime(now),
			}).Error; err != nil {
			return fmt.Errorf("update usage overview five-dimension checkpoint: %w", err)
		}
		processed = len(events)
		return nil
	})
	return processed, err
}

func verifyUsageOverviewFiveDimensionsTarget(db *gorm.DB, targetEventID int64) error {
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", usageOverviewMigrationCheckpointName).Take(&checkpoint).Error; err != nil {
		return fmt.Errorf("verify usage overview five-dimension checkpoint: %w", err)
	}
	if checkpoint.LastAggregatedUsageEventID < targetEventID {
		return fmt.Errorf("usage overview five-dimension checkpoint %d did not reach target %d", checkpoint.LastAggregatedUsageEventID, targetEventID)
	}
	return nil
}
