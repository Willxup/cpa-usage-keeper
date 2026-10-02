package repository

import (
	"context"
	"errors"
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

const (
	PricingInitKindFresh    = "fresh"
	PricingInitKindLegacy   = "legacy"
	pricingInitPhaseOpening = "opening"
)

// BootstrapPricingInitialization 在首次写引导结构之前判定物理库身份，并单独提交单例记录。
// 重启只复用已保存身份；旧库在本方法返回后仍未执行业务 migration 或费用回填。
func BootstrapPricingInitialization(ctx context.Context, db *gorm.DB) (entities.PricingMigrationState, error) {
	if db == nil {
		return entities.PricingMigrationState{}, fmt.Errorf("database is nil")
	}
	var state entities.PricingMigrationState
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 在建控制表前固定身份；新文件与零字节库均没有项目表。
		hadTables, err := sqliteDatabaseHasTables(tx)
		if err != nil {
			return err
		}
		if !tx.Migrator().HasTable(&entities.PricingMigrationState{}) {
			if err := tx.Migrator().CreateTable(&entities.PricingMigrationState{}); err != nil {
				return fmt.Errorf("create pricing migration state: %w", err)
			}
		}
		err = tx.Where("id = ?", 1).First(&state).Error
		if err == nil {
			if state.InitKind != PricingInitKindFresh && state.InitKind != PricingInitKindLegacy {
				return fmt.Errorf("invalid persisted pricing init kind %q", state.InitKind)
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read pricing migration state: %w", err)
		}
		kind := PricingInitKindFresh
		if hadTables {
			kind = PricingInitKindLegacy
		}
		state = entities.PricingMigrationState{ID: 1, InitKind: kind, Phase: pricingInitPhaseOpening}
		if err := tx.Create(&state).Error; err != nil {
			return fmt.Errorf("save pricing initialization identity: %w", err)
		}
		return nil
	})
	return state, err
}

// EnsurePricingBootstrapInbox 仅在身份已持久化后创建缺失的接收表；已有旧表保持原列原值。
// 旧版本的 queue_key/source 转换留给原历史 migration，不在接收引导中预先改列。
func EnsurePricingBootstrapInbox(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id int64
		if err := tx.Table("pricing_migration_state").Select("id").Where("id = ?", 1).Scan(&id).Error; err != nil {
			return fmt.Errorf("check pricing initialization identity: %w", err)
		}
		if id != 1 {
			return fmt.Errorf("pricing initialization identity is missing")
		}
		if tx.Migrator().HasTable(&entities.RedisUsageInbox{}) {
			return nil
		}
		if err := tx.Migrator().CreateTable(&entities.RedisUsageInbox{}); err != nil {
			return fmt.Errorf("create pricing bootstrap inbox: %w", err)
		}
		return nil
	})
}
