package repository

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

const pluginAPIKeyLookupBatchSize = 500

// PluginAPIKeyRegistrationResult 中 Remaining 只包含尚未完成的身份，可在进程内继续重试。
type PluginAPIKeyRegistrationResult struct {
	Remaining        []entities.PluginAPIKeyIdentity
	Inserted         int64
	Conflicts        int
	DeferredForInbox bool
}

// RegisterPluginAPIKeys 先用 reader 排除已存身份，再逐个短事务登记缺失批次。
// 不读取 usage_events，不覆盖已有记录；每次实际写入前都沿用聚合 worker 的 inbox 让路合同。
func RegisterPluginAPIKeys(ctx context.Context, db *gorm.DB, identities []entities.PluginAPIKeyIdentity) (PluginAPIKeyRegistrationResult, error) {
	result := PluginAPIKeyRegistrationResult{Remaining: identities}
	missing := make([]entities.PluginAPIKeyIdentity, 0, len(identities))
	for start := 0; start < len(identities); start += pluginAPIKeyLookupBatchSize {
		batch := identities[start:min(start+pluginAPIKeyLookupBatchSize, len(identities))]
		keys := make([]string, 0, len(batch))
		for _, identity := range batch {
			keys = append(keys, identity.APIGroupKey)
		}
		var existing []entities.CPAAPIKey
		if err := db.WithContext(ctx).Clauses(dbresolver.Read).Select("api_key, source_type, access_provider").
			Where("api_key IN ?", keys).Find(&existing).Error; err != nil {
			return result, fmt.Errorf("find existing plugin API keys: %w", err)
		}
		byKey := make(map[string]entities.CPAAPIKey, len(existing))
		for _, row := range existing {
			byKey[row.APIKey] = row
		}
		for _, identity := range batch {
			row, found := byKey[identity.APIGroupKey]
			if !found {
				missing = append(missing, identity)
			} else if row.SourceType != entities.CPAAPIKeySourcePlugin || row.AccessProvider != identity.AccessProvider {
				result.Conflicts++
			}
		}
	}
	result.Remaining = missing
	writeDB := db.Clauses(dbresolver.Write).Session(&gorm.Session{Context: ctx})
	batchSize := insertBatchSize(entities.CPAAPIKey{})
	for start := 0; start < len(missing); start += batchSize {
		result.Remaining = missing[start:]
		hasInbox, err := HasProcessableRedisUsageInbox(ctx, writeDB)
		if err != nil {
			return result, err
		}
		if hasInbox {
			result.DeferredForInbox = true
			return result, nil
		}
		batch := missing[start:min(start+batchSize, len(missing))]
		rows := make([]entities.CPAAPIKey, 0, len(batch))
		keys := make([]string, 0, len(batch))
		for _, identity := range batch {
			rows = append(rows, entities.CPAAPIKey{
				APIKey: identity.APIGroupKey, SourceType: entities.CPAAPIKeySourcePlugin,
				AccessProvider: identity.AccessProvider,
			})
			keys = append(keys, identity.APIGroupKey)
		}
		var inserted int64
		conflicts := 0
		err = writeDB.Transaction(func(tx *gorm.DB) error {
			// reader 预查后也可能出现同名 native；唯一索引仅跳过，禁止重写其来源或别名。
			created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "api_key"}}, DoNothing: true}).Create(&rows)
			if created.Error != nil {
				return created.Error
			}
			inserted = created.RowsAffected
			var stored []entities.CPAAPIKey
			if err := tx.Select("api_key, source_type, access_provider").Where("api_key IN ?", keys).Find(&stored).Error; err != nil {
				return err
			}
			providers := make(map[string]string, len(batch))
			for _, identity := range batch {
				providers[identity.APIGroupKey] = identity.AccessProvider
			}
			for _, row := range stored {
				if row.SourceType != entities.CPAAPIKeySourcePlugin || row.AccessProvider != providers[row.APIKey] {
					conflicts++
				}
			}
			return nil
		})
		if err != nil {
			return result, fmt.Errorf("register plugin API keys: %w", err)
		}
		result.Inserted += inserted
		result.Conflicts += conflicts
	}
	result.Remaining = nil
	return result, nil
}
