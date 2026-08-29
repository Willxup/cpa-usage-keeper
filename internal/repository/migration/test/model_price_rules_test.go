package test

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const modelPriceRulesMigrationVersion = "20260723_model_price_rules"

type modelPriceRuleColumn struct {
	Name       string
	Type       string
	NotNull    int     `gorm:"column:notnull"`
	DefaultRaw *string `gorm:"column:dflt_value"`
	PrimaryKey int     `gorm:"column:pk"`
}

type modelPriceRuleForeignKey struct {
	Table    string
	From     string
	To       string
	OnDelete string `gorm:"column:on_delete"`
}

func TestModelPriceRulesFreshAndUpgradeSchemasMatch(t *testing.T) {
	fresh := openFreshModelPriceRulesDatabase(t, "fresh.db")
	upgrade := openLegacyModelPriceRulesDatabase(t, "upgrade.db")
	if err := migration.Run(upgrade); err != nil {
		t.Fatalf("run model price rules migration: %v", err)
	}

	assertModelPriceRuleSchema(t, fresh)
	assertModelPriceRuleSchema(t, upgrade)
	if got, want := describeModelPriceRuleSchema(t, upgrade), describeModelPriceRuleSchema(t, fresh); !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade schema differs from fresh schema\nupgrade: %+v\nfresh:   %+v", got, want)
	}

	if err := migration.Run(upgrade); err != nil {
		t.Fatalf("rerun model price rules migration: %v", err)
	}
	var applied int64
	if err := upgrade.Table("schema_migrations").Where("version = ?", modelPriceRulesMigrationVersion).Count(&applied).Error; err != nil {
		t.Fatalf("count model price rules migration: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected migration version once, got %d", applied)
	}
}

func TestModelPriceRulesMigrationRollsBackDDLWhenVersionWriteFails(t *testing.T) {
	db := openLegacyModelPriceRulesDatabase(t, "rollback.db")
	if err := db.Exec(`CREATE TRIGGER fail_model_price_rules_version
		BEFORE INSERT ON schema_migrations
		WHEN NEW.version = '20260723_model_price_rules'
		BEGIN
			SELECT RAISE(FAIL, 'forced model price rules migration failure');
		END`).Error; err != nil {
		t.Fatalf("create migration failure trigger: %v", err)
	}

	if err := migration.Run(db); err == nil {
		t.Fatal("expected migration version write to fail")
	}
	if db.Migrator().HasTable(&entities.ModelPriceRule{}) {
		t.Fatal("expected transactional migration to roll back the new table")
	}
	if err := db.Exec("DROP TRIGGER fail_model_price_rules_version").Error; err != nil {
		t.Fatalf("drop migration failure trigger: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun model price rules migration: %v", err)
	}
	assertModelPriceRuleSchema(t, db)
}

func assertModelPriceRuleSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasTable(&entities.ModelPriceRule{}) {
		t.Fatal("expected model_price_rules table")
	}
	description := describeModelPriceRuleSchema(t, db)
	columns := make(map[string]modelPriceRuleColumn, len(description.columns))
	for _, column := range description.columns {
		columns[column.Name] = column
	}
	for _, name := range []string{"model_price_setting_id", "key", "value", "multiplier"} {
		if columns[name].NotNull != 1 {
			t.Errorf("expected %s to be NOT NULL, got %+v", name, columns[name])
		}
	}
	if columns["multiplier"].DefaultRaw == nil || strings.Trim(*columns["multiplier"].DefaultRaw, "()'") != "1" {
		t.Errorf("expected multiplier default 1, got %+v", columns["multiplier"])
	}
	if !description.hasUniqueIdentityIndex {
		t.Fatal("expected unique model/key/value rule index")
	}
	if len(description.foreignKeys) != 1 {
		t.Fatalf("expected one explicit foreign key, got %+v", description.foreignKeys)
	}
	fk := description.foreignKeys[0]
	if fk.Table != "model_price_settings" || fk.From != "model_price_setting_id" || fk.To != "id" || !strings.EqualFold(fk.OnDelete, "CASCADE") {
		t.Fatalf("unexpected model price rule foreign key: %+v", fk)
	}
	var violations []struct {
		Table string
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		t.Fatalf("run foreign_key_check: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected no foreign key violations, got %+v", violations)
	}
}

type modelPriceRuleSchemaDescription struct {
	columns                []modelPriceRuleColumn
	foreignKeys            []modelPriceRuleForeignKey
	hasUniqueIdentityIndex bool
}

func describeModelPriceRuleSchema(t *testing.T, db *gorm.DB) modelPriceRuleSchemaDescription {
	t.Helper()
	var columns []modelPriceRuleColumn
	if err := db.Raw("PRAGMA table_info('model_price_rules')").Scan(&columns).Error; err != nil {
		t.Fatalf("describe model_price_rules columns: %v", err)
	}
	sort.Slice(columns, func(i, j int) bool { return columns[i].Name < columns[j].Name })
	var foreignKeys []modelPriceRuleForeignKey
	if err := db.Raw("PRAGMA foreign_key_list('model_price_rules')").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("describe model_price_rules foreign keys: %v", err)
	}
	sort.Slice(foreignKeys, func(i, j int) bool { return foreignKeys[i].From < foreignKeys[j].From })

	var indexes []struct {
		Name   string
		Unique int
	}
	if err := db.Raw("PRAGMA index_list('model_price_rules')").Scan(&indexes).Error; err != nil {
		t.Fatalf("list model_price_rules indexes: %v", err)
	}
	hasUnique := false
	for _, index := range indexes {
		if index.Name == "uniq_model_price_rules_identity" && index.Unique == 1 {
			hasUnique = true
		}
	}
	return modelPriceRuleSchemaDescription{columns: columns, foreignKeys: foreignKeys, hasUniqueIdentityIndex: hasUnique}
}

func openFreshModelPriceRulesDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), name)})
	if err != nil {
		t.Fatalf("open fresh database: %v", err)
	}
	closeModelPriceRulesMigrationDatabase(t, db)
	return db
}

func openLegacyModelPriceRulesDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	closeModelPriceRulesMigrationDatabase(t, db)
	if err := db.AutoMigrate(&entities.ModelPriceSetting{}); err != nil {
		t.Fatalf("create legacy pricing schema: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", modelPriceRulesMigrationVersion).Error; err != nil {
		t.Fatalf("enable model price rules migration: %v", err)
	}
	return db
}

func closeModelPriceRulesMigrationDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}
