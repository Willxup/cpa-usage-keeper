package test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

const pricingStorageStructureVersion = "20261002_pricing_storage_structure"

func TestPricingStorageStructureMigrationPreservesOldRowsAndNullBackfillState(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	seedPhysicalPricingStorageBeforeCost(t, db)
	runOnlyMigration(t, db, pricingStorageStructureVersion)

	for _, table := range []string{"usage_events", "usage_events_archive"} {
		rowID := int64(1)
		if table == "usage_events_archive" {
			rowID = 3
		}
		assertPricingColumnType(t, db, table, "cost_usd", "REAL")
		assertPricingColumnType(t, db, table, "cost_available", "BOOLEAN")
		for _, column := range []string{"cost_usd", "cost_available"} {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("missing %s.%s", table, column)
			}
		}
		var eventKey string
		var cost sql.NullFloat64
		var available sql.NullBool
		if err := db.Raw("SELECT event_key, cost_usd, cost_available FROM "+table+" WHERE id = ?", rowID).Row().Scan(&eventKey, &cost, &available); err != nil {
			t.Fatalf("load old %s row: %v", table, err)
		}
		if eventKey != "old-"+table || cost.Valid || available.Valid {
			t.Fatalf("old %s row changed or pretended to be priced: key=%q cost=%+v available=%+v", table, eventKey, cost, available)
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertPricingColumnType(t, db, table, "cost_usd", "REAL")
		assertPricingColumnType(t, db, table, "unavailable_cost_count", "INTEGER")
		for _, column := range []string{"cost_usd", "unavailable_cost_count"} {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("missing %s.%s", table, column)
			}
		}
		var requests int64
		var cost sql.NullFloat64
		var unavailable sql.NullInt64
		if err := db.Raw("SELECT request_count, cost_usd, unavailable_cost_count FROM "+table+" WHERE id = 1").Row().Scan(&requests, &cost, &unavailable); err != nil {
			t.Fatalf("load old %s bucket: %v", table, err)
		}
		if requests != 7 || cost.Valid || unavailable.Valid {
			t.Fatalf("old %s bucket changed or pretended to be filled: requests=%d cost=%+v unavailable=%+v", table, requests, cost, unavailable)
		}
	}
	var branches string
	assertPricingColumnType(t, db, "model_price_settings", "branches_json", "TEXT")
	if err := db.Raw("SELECT branches_json FROM model_price_settings WHERE id = 1").Row().Scan(&branches); err != nil || branches != "[]" {
		t.Fatalf("old model branches=%q err=%v, want []", branches, err)
	}
	for _, table := range []string{"pricing_state", "pricing_migration_state"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing %s control table", table)
		}
	}

	// 重跑结构版本只能补缺列；已经写入的费用与旧事实都不能被清空。
	if err := db.Exec("UPDATE usage_events SET cost_usd = 1.25, cost_available = 1 WHERE id = 1").Error; err != nil {
		t.Fatalf("write sample cost before rerun: %v", err)
	}
	runOnlyMigration(t, db, pricingStorageStructureVersion)
	var cost float64
	var available bool
	if err := db.Raw("SELECT cost_usd, cost_available FROM usage_events WHERE id = 1").Row().Scan(&cost, &available); err != nil || cost != 1.25 || !available {
		t.Fatalf("rerun changed stored cost: cost=%g available=%t err=%v", cost, available, err)
	}
}

func TestFreshPricingStorageSchemaContainsNullableCostAndControlTables(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "fresh-pricing.db")})
	if err != nil {
		t.Fatalf("open fresh pricing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		assertPricingColumnType(t, db, table, "cost_usd", "REAL")
		assertPricingColumnType(t, db, table, "cost_available", "BOOLEAN")
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertPricingColumnType(t, db, table, "cost_usd", "REAL")
		assertPricingColumnType(t, db, table, "unavailable_cost_count", "INTEGER")
	}
	assertPricingColumnType(t, db, "model_price_settings", "branches_json", "TEXT")
	for _, table := range []string{"pricing_state", "pricing_migration_state"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("fresh database is missing %s", table)
		}
	}
}

func TestPricingStorageControlTablesConstrainSingletonAndRevision(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	seedPhysicalPricingStorageBeforeCost(t, db)
	runOnlyMigration(t, db, pricingStorageStructureVersion)
	if err := db.Create(&entities.PricingState{ID: 1, ConfigRevision: 0}).Error; err != nil {
		t.Fatalf("create pricing state: %v", err)
	}
	if err := db.Create(&entities.PricingState{ID: 2, ConfigRevision: 0}).Error; err == nil {
		t.Fatal("pricing state accepted a second singleton ID")
	}
	if err := db.Model(&entities.PricingState{}).Where("id = 1").Update("config_revision", -1).Error; err == nil {
		t.Fatal("pricing state accepted a negative revision")
	}
	if err := db.Create(&entities.PricingMigrationState{ID: 1, InitKind: "legacy", Phase: "opening"}).Error; err != nil {
		t.Fatalf("create migration control state: %v", err)
	}
	if err := db.Create(&entities.PricingMigrationState{ID: 2, InitKind: "legacy", Phase: "opening"}).Error; err == nil {
		t.Fatal("migration state accepted a second singleton ID")
	}
	var baseline, cursors sql.NullString
	var schemaComplete, dataComplete bool
	if err := db.Raw("SELECT baseline_json, cursors_json, schema_complete, data_complete FROM pricing_migration_state WHERE id = 1").Row().Scan(&baseline, &cursors, &schemaComplete, &dataComplete); err != nil {
		t.Fatalf("read empty migration control: %v", err)
	}
	if baseline.Valid || cursors.Valid || schemaComplete || dataComplete {
		t.Fatalf("new migration control claimed completed data: baseline=%+v cursors=%+v schema=%t data=%t", baseline, cursors, schemaComplete, dataComplete)
	}
}

// 真实旧物理列与非空旧值证明增列不会借 AutoMigrate 重建并吞掉原始事实。
func seedPhysicalPricingStorageBeforeCost(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		"CREATE TABLE usage_events (id integer PRIMARY KEY, event_key text)",
		"CREATE TABLE usage_events_archive (id integer PRIMARY KEY, event_key text)",
		"CREATE TABLE usage_overview_hourly_stats (id integer PRIMARY KEY, request_count integer)",
		"CREATE TABLE usage_overview_daily_stats (id integer PRIMARY KEY, request_count integer)",
		"CREATE TABLE model_price_settings (id integer PRIMARY KEY, model text)",
		"INSERT INTO usage_events (id, event_key) VALUES (1, 'old-usage_events')",
		"INSERT INTO usage_events_archive (id, event_key) VALUES (3, 'old-usage_events_archive')",
		"INSERT INTO usage_overview_hourly_stats (id, request_count) VALUES (1, 7)",
		"INSERT INTO usage_overview_daily_stats (id, request_count) VALUES (1, 7)",
		"INSERT INTO model_price_settings (id, model) VALUES (1, 'old-model')",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create old pricing fixture with %q: %v", statement, err)
		}
	}
}

func assertPricingColumnType(t *testing.T, db *gorm.DB, table, column, wantType string) {
	t.Helper()
	var columns []struct {
		Name string
		Type string
	}
	if err := db.Raw("PRAGMA table_info(" + table + ")").Scan(&columns).Error; err != nil {
		t.Fatalf("inspect %s columns: %v", table, err)
	}
	for _, item := range columns {
		if item.Name == column {
			if !strings.EqualFold(item.Type, wantType) {
				t.Fatalf("%s.%s type=%q, want %s", table, column, item.Type, wantType)
			}
			return
		}
	}
	t.Fatalf("missing %s.%s", table, column)
}
