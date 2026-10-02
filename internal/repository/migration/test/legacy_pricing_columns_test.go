package test

import (
	"testing"

	"gorm.io/gorm"
)

// 历史迁移执行完毕仍未进入新费用结构阶段，物理旧表不得提前获得新列。
func assertNoFuturePricingColumns(t *testing.T, db *gorm.DB, table string, columns ...string) {
	t.Helper()
	for _, column := range columns {
		if db.Migrator().HasColumn(table, column) {
			t.Fatalf("historical migration added future column %s.%s", table, column)
		}
	}
}
