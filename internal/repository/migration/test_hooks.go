package migration

import "gorm.io/gorm"

// OrderedMigrationVersionsForTest 给外部 test 包保留迁移顺序覆盖，避免测试必须和实现同包。
func OrderedMigrationVersionsForTest() []string {
	migrations := orderedMigrations()
	versions := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		versions = append(versions, migration.version)
	}
	return versions
}

// RunSchemaMigrationForTest 暴露单条迁移执行器，专用于验证事务和错误日志语义。
func RunSchemaMigrationForTest(db *gorm.DB, version string, run func(*gorm.DB) error) error {
	return runSchemaMigration(db, databaseMigration{version: version, run: run})
}
