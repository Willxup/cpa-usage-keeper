package migration

import "gorm.io/gorm"

// addUsageEventFailureFieldsMigration 给 usage_events 表增加失败详情字段。
func addUsageEventFailureFieldsMigration(tx *gorm.DB) error {
	columns := []struct {
		name string
		sql  string
	}{
		{"failure_status_code", "ALTER TABLE usage_events ADD COLUMN failure_status_code INTEGER"},
		{"failure_code", "ALTER TABLE usage_events ADD COLUMN failure_code TEXT NOT NULL DEFAULT ''"},
		{"failure_message", "ALTER TABLE usage_events ADD COLUMN failure_message TEXT NOT NULL DEFAULT ''"},
		{"failure_body", "ALTER TABLE usage_events ADD COLUMN failure_body TEXT NOT NULL DEFAULT ''"},
	}
	for _, col := range columns {
		if tx.Migrator().HasColumn("usage_events", col.name) {
			continue
		}
		if err := tx.Exec(col.sql).Error; err != nil {
			return err
		}
	}
	return nil
}
