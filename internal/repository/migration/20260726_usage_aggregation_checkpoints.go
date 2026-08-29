package migration

import (
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// usageAggregationCheckpointSeed 是迁移期使用的完整目标行，便于写后逐字段验证。
type usageAggregationCheckpointSeed struct {
	Name                       entities.UsageAggregationCheckpointName
	LastAggregatedUsageEventID int64
	StatsUpdatedAt             *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// usageAggregationCheckpointsMigration 原子合并两个旧表，并预置 Latency 的零水位。
func usageAggregationCheckpointsMigration(tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}
	// 默认 migration 外层事务覆盖建表、复制、验证和删旧表，任一步失败都恢复原 schema。
	if err := tx.AutoMigrate(&entities.UsageAggregationCheckpoint{}); err != nil {
		return fmt.Errorf("create usage aggregation checkpoints: %w", err)
	}
	now := timeutil.NormalizeStorageTime(time.Now())

	// 每个旧表独立读取；表或行缺失都初始化为 0，而不是阻断历史升级。
	overviewSeed, err := loadLegacyOverviewAggregationCheckpointSeed(tx, now)
	if err != nil {
		return err
	}
	activitySeed, err := loadLegacyActivityAggregationCheckpointSeed(tx, now)
	if err != nil {
		return err
	}
	// Latency 尚无旧聚合，零水位保证后续回填从当前仍保留的最早事件开始。
	latencySeed := zeroUsageAggregationCheckpointSeed(entities.UsageAggregationCheckpointLatency, now)

	// 三行全部写入并读回验证后才能删除任一旧表。
	for _, seed := range []usageAggregationCheckpointSeed{overviewSeed, activitySeed, latencySeed} {
		if err := ensureUsageAggregationCheckpointSeed(tx, seed); err != nil {
			return err
		}
	}

	// 历史类型继续保留在源码里，但物理旧表在同一事务尾部删除，消除双事实来源。
	if tx.Migrator().HasTable(&entities.UsageOverviewAggregationCheckpoint{}) {
		if err := tx.Migrator().DropTable(&entities.UsageOverviewAggregationCheckpoint{}); err != nil {
			return fmt.Errorf("drop legacy usage overview aggregation checkpoints: %w", err)
		}
	}
	if tx.Migrator().HasTable(&entities.UsageActivityAggregationCheckpoint{}) {
		if err := tx.Migrator().DropTable(&entities.UsageActivityAggregationCheckpoint{}); err != nil {
			return fmt.Errorf("drop legacy usage activity aggregation checkpoints: %w", err)
		}
	}
	return nil
}

func loadLegacyOverviewAggregationCheckpointSeed(tx *gorm.DB, now time.Time) (usageAggregationCheckpointSeed, error) {
	// 没有旧 Overview 表时直接返回零水位，不执行会报 no such table 的 SELECT。
	if !tx.Migrator().HasTable(&entities.UsageOverviewAggregationCheckpoint{}) {
		return zeroUsageAggregationCheckpointSeed(entities.UsageAggregationCheckpointOverview, now), nil
	}
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	err := tx.Where("name = ?", "overview").Take(&checkpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zeroUsageAggregationCheckpointSeed(entities.UsageAggregationCheckpointOverview, now), nil
	}
	if err != nil {
		return usageAggregationCheckpointSeed{}, fmt.Errorf("load legacy usage overview aggregation checkpoint: %w", err)
	}
	return usageAggregationCheckpointSeed{
		Name:                       entities.UsageAggregationCheckpointOverview,
		LastAggregatedUsageEventID: checkpoint.LastAggregatedUsageEventID,
		StatsUpdatedAt:             checkpoint.StatsUpdatedAt,
		CreatedAt:                  checkpoint.CreatedAt,
		UpdatedAt:                  checkpoint.UpdatedAt,
	}, nil
}

func loadLegacyActivityAggregationCheckpointSeed(tx *gorm.DB, now time.Time) (usageAggregationCheckpointSeed, error) {
	// Activity 历史版本可能从未启动，因此表缺失和空表都按零水位处理。
	if !tx.Migrator().HasTable(&entities.UsageActivityAggregationCheckpoint{}) {
		return zeroUsageAggregationCheckpointSeed(entities.UsageAggregationCheckpointActivity, now), nil
	}
	var checkpoint entities.UsageActivityAggregationCheckpoint
	err := tx.Where("name = ?", "activity").Take(&checkpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zeroUsageAggregationCheckpointSeed(entities.UsageAggregationCheckpointActivity, now), nil
	}
	if err != nil {
		return usageAggregationCheckpointSeed{}, fmt.Errorf("load legacy usage activity aggregation checkpoint: %w", err)
	}
	return usageAggregationCheckpointSeed{
		Name:                       entities.UsageAggregationCheckpointActivity,
		LastAggregatedUsageEventID: checkpoint.LastAggregatedUsageEventID,
		StatsUpdatedAt:             checkpoint.StatsUpdatedAt,
		CreatedAt:                  checkpoint.CreatedAt,
		UpdatedAt:                  checkpoint.UpdatedAt,
	}, nil
}

func zeroUsageAggregationCheckpointSeed(name entities.UsageAggregationCheckpointName, now time.Time) usageAggregationCheckpointSeed {
	// 零水位行使用同一个迁移时刻，避免三行出现无意义的时间漂移。
	return usageAggregationCheckpointSeed{Name: name, CreatedAt: now, UpdatedAt: now}
}

func ensureUsageAggregationCheckpointSeed(tx *gorm.DB, seed usageAggregationCheckpointSeed) error {
	// ON CONFLICT 不覆盖已有通用行；随后读回比较决定它是幂等状态还是危险冲突。
	row := entities.UsageAggregationCheckpoint{
		Name:                       seed.Name,
		LastAggregatedUsageEventID: seed.LastAggregatedUsageEventID,
		StatsUpdatedAt:             seed.StatsUpdatedAt,
		CreatedAt:                  seed.CreatedAt,
		UpdatedAt:                  seed.UpdatedAt,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return fmt.Errorf("create usage aggregation checkpoint %q: %w", seed.Name, err)
	}
	// 读回是删除旧表前的强制验证，trigger 或预存冲突都不能被当作成功迁移。
	var stored entities.UsageAggregationCheckpoint
	if err := tx.Where("name = ?", seed.Name).Take(&stored).Error; err != nil {
		return fmt.Errorf("verify usage aggregation checkpoint %q: %w", seed.Name, err)
	}
	if stored.LastAggregatedUsageEventID != seed.LastAggregatedUsageEventID || !optionalStorageTimesEqual(stored.StatsUpdatedAt, seed.StatsUpdatedAt) {
		return fmt.Errorf("usage aggregation checkpoint %q conflicts with legacy cursor", seed.Name)
	}
	return nil
}

func optionalStorageTimesEqual(left, right *time.Time) bool {
	// nil 只与 nil 相等；非 nil 使用 time.Equal 忽略同一时刻的 location 表示差异。
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
