package test

import (
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
)

func TestPricingMigrationPhaseBoundaryLeavesPublishedSchemaWithoutNewCostColumns(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	seedPhysicalPricingStorageBeforeCost(t, db)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("模拟已发布旧版本：%v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", pricingStorageStructureVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.RunPublished(db); err != nil {
		t.Fatalf("运行已发布旧迁移：%v", err)
	}
	assertNoFuturePricingColumns(t, db, "usage_events", "cost_usd", "cost_available")
	assertNoFuturePricingColumns(t, db, "model_price_settings", "branches_json")
	if err := migration.RunPricingStorageStructure(db); err != nil {
		t.Fatalf("固定基线后运行新结构迁移：%v", err)
	}
	if !db.Migrator().HasColumn("usage_events", "cost_usd") || !db.Migrator().HasColumn("model_price_settings", "branches_json") {
		t.Fatal("M3 结构迁移没有添加费用与分支列")
	}
	if err := migration.RunPricingStorageStructure(db); err != nil {
		t.Fatalf("重启重复结构迁移：%v", err)
	}
}

func TestPricingStoragePhaseRejectsMissingPublishedMigration(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	seedPhysicalPricingStorageBeforeCost(t, db)
	if err := migration.RunPricingStorageStructure(db); err == nil {
		t.Fatal("旧迁移未完成时不得提前添加新费用结构")
	}
	assertNoFuturePricingColumns(t, db, "usage_events", "cost_usd", "cost_available")
}
