package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// EnsureUsageIdentityActivityIndex 同时服务新库建表和已发布库的追加迁移。
func EnsureUsageIdentityActivityIndex(db *gorm.DB) error {
	if !db.Migrator().HasTable(&entities.UsageEvent{}) {
		return fmt.Errorf("create usage identity activity index: usage_events table is missing")
	}
	// SQLite 表达式索引保证不同时区及变长小数的真实时间顺序可由索引限量读取。
	statement := "CREATE INDEX IF NOT EXISTS " + entities.UsageIdentityActivityIndex +
		" ON usage_events (auth_type, auth_index, failed, " + entities.UsageIdentityActivityTimestamp + ", id)"
	if err := db.Exec(statement).Error; err != nil {
		return fmt.Errorf("create usage identity activity index: %w", err)
	}
	return nil
}
