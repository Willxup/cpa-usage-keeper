package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// createErrorEventsMigration 为既有数据库创建 Error Event 最终表及详情查询复合索引。
func createErrorEventsMigration(db *gorm.DB) error {
	if err := db.AutoMigrate(&entities.ErrorEvent{}); err != nil {
		return fmt.Errorf("create CPA error events schema: %w", err)
	}
	return nil
}
