package repository

import (
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

func SyncCPAAPIKeys(db *gorm.DB, keys []string, syncedAt time.Time) error {
	seen := make(map[string]struct{}, len(keys))
	uniqueKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniqueKeys = append(uniqueKeys, key)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		// CPA 核心清单只做 source=cpa 行的全量替换，插件行由 SyncPluginAPIKeys 独立维护。
		var existingRows []struct {
			ID        int64
			APIKey    string
			IsDeleted bool
		}
		if err := tx.Model(&entities.CPAAPIKey{}).Select("id, api_key, is_deleted").Where("source = ?", entities.CPAAPIKeySourceCPA).Find(&existingRows).Error; err != nil {
			return err
		}

		existingByKey := make(map[string]struct {
			ID        int64
			IsDeleted bool
		}, len(existingRows))
		for _, row := range existingRows {
			existingByKey[row.APIKey] = struct {
				ID        int64
				IsDeleted bool
			}{ID: row.ID, IsDeleted: row.IsDeleted}
		}

		incoming := make(map[string]struct{}, len(uniqueKeys))
		toCreate := make([]entities.CPAAPIKey, 0)
		for _, key := range uniqueKeys {
			incoming[key] = struct{}{}
			if existing, ok := existingByKey[key]; ok {
				updates := map[string]any{
					"display_key":    helper.RedactSensitiveValue(key),
					"is_deleted":     false,
					"last_synced_at": &syncedAt,
					"updated_at":     syncedAt,
				}
				if err := tx.Model(&entities.CPAAPIKey{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
					return err
				}
				continue
			}
			toCreate = append(toCreate, entities.CPAAPIKey{
				APIKey:       key,
				DisplayKey:   helper.RedactSensitiveValue(key),
				Source:       entities.CPAAPIKeySourceCPA,
				IsDeleted:    false,
				LastSyncedAt: &syncedAt,
			})
		}
		if len(toCreate) > 0 {
			if err := tx.Create(&toCreate).Error; err != nil {
				return err
			}
		}

		staleIDs := make([]int64, 0)
		for _, row := range existingRows {
			if row.IsDeleted {
				continue
			}
			if _, ok := incoming[row.APIKey]; ok {
				continue
			}
			staleIDs = append(staleIDs, row.ID)
		}
		if len(staleIDs) == 0 {
			return nil
		}
		return tx.Model(&entities.CPAAPIKey{}).Where("id IN ?", staleIDs).Updates(map[string]any{"is_deleted": true, "updated_at": syncedAt}).Error
	})
}

// PluginAPIKey 是 key-policy 插件清单中单个启用 key 的同步输入；ID 是插件 key 的短 id（如 "kath"），也是用量事件 api_group_key 的关联值。
type PluginAPIKey struct {
	ID         string
	Name       string
	KeyPreview string
}

// SyncPluginAPIKeys 按插件清单全量替换 source=plugin 的本地行；只有插件 API 拉取成功时才允许调用，失败必须保留现状。
func SyncPluginAPIKeys(db *gorm.DB, keys []PluginAPIKey, syncedAt time.Time) error {
	seen := make(map[string]struct{}, len(keys))
	uniqueKeys := make([]PluginAPIKey, 0, len(keys))
	for _, key := range keys {
		key.ID = strings.TrimSpace(key.ID)
		key.Name = strings.TrimSpace(key.Name)
		key.KeyPreview = strings.TrimSpace(key.KeyPreview)
		if key.ID == "" {
			continue
		}
		if _, ok := seen[key.ID]; ok {
			continue
		}
		seen[key.ID] = struct{}{}
		uniqueKeys = append(uniqueKeys, key)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		// 插件清单只维护 source=plugin 行，绝不触碰 CPA 核心 key 的替换结果。
		var existingRows []struct {
			ID        int64
			APIKey    string
			KeyAlias  string
			IsDeleted bool
		}
		if err := tx.Model(&entities.CPAAPIKey{}).Select("id, api_key, key_alias, is_deleted").Where("source = ?", entities.CPAAPIKeySourcePlugin).Find(&existingRows).Error; err != nil {
			return err
		}

		existingByKey := make(map[string]struct {
			ID        int64
			KeyAlias  string
			IsDeleted bool
		}, len(existingRows))
		for _, row := range existingRows {
			existingByKey[row.APIKey] = struct {
				ID        int64
				KeyAlias  string
				IsDeleted bool
			}{ID: row.ID, KeyAlias: row.KeyAlias, IsDeleted: row.IsDeleted}
		}

		incoming := make(map[string]struct{}, len(uniqueKeys))
		toCreate := make([]entities.CPAAPIKey, 0)
		for _, key := range uniqueKeys {
			incoming[key.ID] = struct{}{}
			// 插件 key 的 api_key 列存插件 id；display_key 优先使用插件自带的 key_preview，缺失时回退到 id。
			displayKey := key.KeyPreview
			if displayKey == "" {
				displayKey = key.ID
			}
			if existing, ok := existingByKey[key.ID]; ok {
				updates := map[string]any{
					"display_key":    displayKey,
					"is_deleted":     false,
					"last_synced_at": &syncedAt,
					"updated_at":     syncedAt,
				}
				// 只在本地别名为空时用插件 name 填充，不覆盖用户手工修改过的别名。
				if strings.TrimSpace(existing.KeyAlias) == "" && key.Name != "" {
					updates["key_alias"] = key.Name
				}
				if err := tx.Model(&entities.CPAAPIKey{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
					return err
				}
				continue
			}
			toCreate = append(toCreate, entities.CPAAPIKey{
				APIKey:       key.ID,
				DisplayKey:   displayKey,
				KeyAlias:     key.Name,
				Source:       entities.CPAAPIKeySourcePlugin,
				IsDeleted:    false,
				LastSyncedAt: &syncedAt,
			})
		}
		if len(toCreate) > 0 {
			if err := tx.Create(&toCreate).Error; err != nil {
				return err
			}
		}

		// 插件清单里消失的 plugin 行才软删；CPA 侧同步不参与这里的判断。
		staleIDs := make([]int64, 0)
		for _, row := range existingRows {
			if row.IsDeleted {
				continue
			}
			if _, ok := incoming[row.APIKey]; ok {
				continue
			}
			staleIDs = append(staleIDs, row.ID)
		}
		if len(staleIDs) == 0 {
			return nil
		}
		return tx.Model(&entities.CPAAPIKey{}).Where("id IN ?", staleIDs).Updates(map[string]any{"is_deleted": true, "updated_at": syncedAt}).Error
	})
}

func ListActiveCPAAPIKeys(db *gorm.DB) ([]entities.CPAAPIKey, error) {
	var rows []entities.CPAAPIKey
	err := db.Where("is_deleted = ?", false).Order("id asc").Find(&rows).Error
	return rows, err
}

func FindActiveCPAAPIKeyByID(db *gorm.DB, id int64) (entities.CPAAPIKey, error) {
	var row entities.CPAAPIKey
	err := db.Where("id = ? AND is_deleted = ?", id, false).First(&row).Error
	return row, err
}

func FindActiveCPAAPIKeyByValue(db *gorm.DB, apiKey string) (entities.CPAAPIKey, error) {
	var row entities.CPAAPIKey
	// 插件 key 的 id 是短字符串、可猜测，绝不允许作为 api-key 登录凭据匹配。
	err := db.Where("api_key = ? AND is_deleted = ? AND source <> ?", apiKey, false, entities.CPAAPIKeySourcePlugin).First(&row).Error
	return row, err
}

func UpdateCPAAPIKeyAlias(db *gorm.DB, id int64, keyAlias string) error {
	result := db.Model(&entities.CPAAPIKey{}).Where("id = ? AND is_deleted = ?", id, false).Update("key_alias", strings.TrimSpace(keyAlias))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateCPAAPIKeyLocalRankingProfile 在同一写事务中保存并回读 Key 的本地展示资料。
func UpdateCPAAPIKeyLocalRankingProfile(db *gorm.DB, id int64, keyAlias string, avatarID uint8) (entities.CPAAPIKey, error) {
	var row entities.CPAAPIKey
	err := db.Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.CPAAPIKey{}).Where("id = ?", id).Updates(map[string]any{
			"key_alias":               strings.TrimSpace(keyAlias),
			"local_ranking_avatar_id": avatarID,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("id = ?", id).First(&row).Error
	})
	return row, err
}
