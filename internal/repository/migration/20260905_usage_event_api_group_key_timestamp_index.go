package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// addUsageEventAPIGroupKeyTimestampIndexMigration 用 Key+时间升序复合索引替代单列 Key 索引。
func addUsageEventAPIGroupKeyTimestampIndexMigration(tx *gorm.DB) error {
	migrator := tx.Migrator()
	if !migrator.HasTable(&entities.UsageEvent{}) {
		return nil
	}
	const indexName = "idx_usage_events_api_group_key_timestamp"
	// 同名旧索引可能是 timestamp DESC；重建为实体声明，反向扫描即可满足 timestamp DESC, id DESC。
	if migrator.HasIndex(&entities.UsageEvent{}, indexName) {
		if err := migrator.DropIndex(&entities.UsageEvent{}, indexName); err != nil {
			return fmt.Errorf("drop previous idx_usage_events_api_group_key_timestamp: %w", err)
		}
	}
	if err := migrator.CreateIndex(&entities.UsageEvent{}, indexName); err != nil {
		return fmt.Errorf("create idx_usage_events_api_group_key_timestamp: %w", err)
	}
	// 复合索引的最左列继续支持 Key 查询，移除单列索引以减少每次写入的维护成本。
	const singleKeyIndexName = "idx_usage_events_api_group_key"
	if migrator.HasIndex(&entities.UsageEvent{}, singleKeyIndexName) {
		if err := migrator.DropIndex(&entities.UsageEvent{}, singleKeyIndexName); err != nil {
			return fmt.Errorf("drop redundant idx_usage_events_api_group_key: %w", err)
		}
	}
	return nil
}
