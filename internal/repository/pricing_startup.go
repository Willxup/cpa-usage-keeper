package repository

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// InitializeFreshPricingDatabase 只对已持久标识为 fresh 的库执行正常建表，不运行旧数据回放。
// 中途退出可重入，全部结构与版本完成后才标记 schema/data 完成，启动环境仍由 App 核验。
func InitializeFreshPricingDatabase(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	var state entities.PricingMigrationState
	if err := db.Clauses(dbresolver.Write).WithContext(ctx).Where("id = ?", 1).Take(&state).Error; err != nil {
		return fmt.Errorf("load fresh pricing initialization: %w", err)
	}
	if state.InitKind != PricingInitKindFresh {
		return fmt.Errorf("fresh pricing initialization requires fresh identity")
	}
	if state.DataComplete && state.SchemaComplete {
		return nil
	}
	if err := db.WithContext(ctx).AutoMigrate(entities.All()...); err != nil {
		return fmt.Errorf("create fresh database schema: %w", err)
	}
	if err := migration.MarkAllAsApplied(db.WithContext(ctx)); err != nil {
		return fmt.Errorf("mark fresh schema migrations: %w", err)
	}
	return db.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND init_kind = ?", 1, PricingInitKindFresh).
			Updates(map[string]any{"phase": "data_complete", "schema_complete": true, "data_complete": true})
		if result.Error != nil {
			return fmt.Errorf("record fresh pricing data completion: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("fresh pricing initialization identity changed")
		}
		return nil
	})
}

// VerifyPricingStartupDataComplete 在公开业务前只读核对持久费用状态；运行时 ready 由 HTTP 外壳单独表达。
func VerifyPricingStartupDataComplete(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	var state entities.PricingMigrationState
	if err := db.Clauses(dbresolver.Read).WithContext(ctx).Where("id = ?", 1).Take(&state).Error; err != nil {
		return fmt.Errorf("load pricing startup state: %w", err)
	}
	if !state.SchemaComplete || !state.DataComplete || state.Phase != "data_complete" ||
		state.InitKind != PricingInitKindFresh && state.InitKind != PricingInitKindLegacy {
		return fmt.Errorf("pricing data is not ready")
	}
	return nil
}
