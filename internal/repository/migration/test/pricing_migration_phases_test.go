package test

import (
	"strings"
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
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertNoFuturePricingColumns(t, db, table, "speed_tps_sum", "speed_sample_count", "decode_speed_tps_sum", "decode_speed_sample_count")
	}
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

// main 的新增迁移必须先归入已发布阶段，不能被未发布 v2 的费用分界截断。
func TestPricingStorageWaitsForLatestMainMetadataMigration(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	seedPhysicalPricingStorageBeforeCost(t, db)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version IN ?", []string{usageEventTraceMetadataMigrationVersion, pricingStorageStructureVersion, "20261005_remove_ranking"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.RunPricingStorageStructure(db); err == nil || !strings.Contains(err.Error(), usageEventTraceMetadataMigrationVersion) {
		t.Fatalf("费用结构必须等待最新 main 迁移，got %v", err)
	}
	if err := migration.RunPublished(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		for _, column := range []string{"execution_id", "trace_id", "node_kind", "is_fork", "is_compaction"} {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("已发布阶段缺少 %s.%s", table, column)
			}
		}
		assertNoFuturePricingColumns(t, db, table, "cost_usd", "cost_available")
		if err := db.Table(table).Where("id > 0").Updates(map[string]any{"execution_id": "saved-by-v1", "is_fork": false}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 已经在 v1 执行过的迁移应跳过，保存值不能被重新置空。
	if err := migration.RunPublished(db); err != nil {
		t.Fatal(err)
	}
	if err := migration.RunPricingStorageStructure(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var row struct {
			ExecutionID *string
			IsFork      *bool
			CostUSD     *float64
		}
		if err := db.Table(table).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.ExecutionID == nil || *row.ExecutionID != "saved-by-v1" || row.IsFork == nil || *row.IsFork || row.CostUSD != nil {
			t.Fatalf("升级改变 v1 元数据或提前回填费用: %+v", row)
		}
	}
	var rankingVersions int64
	if err := db.Table("schema_migrations").Where("version = ?", "20261005_remove_ranking").Count(&rankingVersions).Error; err != nil {
		t.Fatal(err)
	}
	if rankingVersions != 0 {
		t.Fatal("已发布阶段不得执行 v2 Ranking 移除")
	}
}
