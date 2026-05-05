package migration

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

const redisUsageInboxStatusProcessed = "processed"

type usageIdentityStatsDelta struct {
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
	FirstUsedAt     *time.Time
	LastUsedAt      *time.Time
	MaxUsageEventID uint
}

func dropColumnIfExists(tx *gorm.DB, model any, columnName string, tableName string) error {
	if !tx.Migrator().HasTable(model) || !tx.Migrator().HasColumn(model, columnName) {
		return nil
	}
	if err := tx.Exec("ALTER TABLE " + tableName + " DROP COLUMN " + columnName).Error; err != nil {
		return fmt.Errorf("drop %s.%s column: %w", tableName, columnName, err)
	}
	return nil
}

func legacyDeletedStateSelect(tx *gorm.DB, table string) (string, string) {
	if tx.Migrator().HasColumn(table, "deleted_at") {
		return "deleted_at IS NOT NULL", "deleted_at"
	}
	return "false", "NULL"
}
