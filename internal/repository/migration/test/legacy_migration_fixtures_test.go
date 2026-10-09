package test

import (
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
)

// 每次重新标记目标为待执行，保留旧测试对重复执行迁移的幂等检查。
func runLegacyMigration(db *gorm.DB, version string) error {
	if err := migration.MarkAllAsApplied(db); err != nil {
		return err
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		return err
	}
	return migration.Run(db)
}

func orderedMigrationVersions(t *testing.T) []string {
	t.Helper()
	db := openUnmigratedTestDatabase(t)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("record registered migration versions: %v", err)
	}
	// MarkAllAsApplied 按注册顺序写入；rowid 保留该顺序，不能按版本字符串排序。
	var versions []string
	if err := db.Table("schema_migrations").Order("rowid").Pluck("version", &versions).Error; err != nil {
		t.Fatalf("read registered migration order: %v", err)
	}
	return versions
}
