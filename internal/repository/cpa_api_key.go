package repository

import (
	"errors"
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
		var existingRows []struct {
			ID        int64
			APIKey    string
			IsDeleted bool
		}
		if err := tx.Model(&entities.CPAAPIKey{}).Select("id, api_key, is_deleted").Find(&existingRows).Error; err != nil {
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

// UpsertActiveCPAAPIKey 新增或重新激活一条本地 API Key 记录，并把别名写回；创建时使用调用方传入的 syncedAt 作为时间基准。
func UpsertActiveCPAAPIKey(db *gorm.DB, apiKey string, keyAlias string, syncedAt time.Time) (entities.CPAAPIKey, error) {
	apiKey = strings.TrimSpace(apiKey)
	keyAlias = strings.TrimSpace(keyAlias)
	if apiKey == "" {
		return entities.CPAAPIKey{}, errors.New("api key is empty")
	}
	var existing entities.CPAAPIKey
	err := db.Where("api_key = ?", apiKey).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row := entities.CPAAPIKey{
			APIKey:       apiKey,
			DisplayKey:   helper.RedactSensitiveValue(apiKey),
			KeyAlias:     keyAlias,
			IsDeleted:    false,
			LastSyncedAt: &syncedAt,
			CreatedAt:    syncedAt,
			UpdatedAt:    syncedAt,
		}
		if err := db.Create(&row).Error; err != nil {
			return entities.CPAAPIKey{}, err
		}
		return row, nil
	}
	if err != nil {
		return entities.CPAAPIKey{}, err
	}
	updates := map[string]any{
		"display_key":    helper.RedactSensitiveValue(apiKey),
		"is_deleted":     false,
		"key_alias":      keyAlias,
		"last_synced_at": &syncedAt,
		"updated_at":     syncedAt,
	}
	if err := db.Model(&entities.CPAAPIKey{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return entities.CPAAPIKey{}, err
	}
	return FindActiveCPAAPIKeyByID(db, existing.ID)
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
	err := db.Where("api_key = ? AND is_deleted = ?", apiKey, false).First(&row).Error
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
