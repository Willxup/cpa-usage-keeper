package repository

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

type CPAAPIKeyMetadata struct {
	APIKey     string
	DisplayKey string
	KeyAlias   string
}

type CPAAPIKeySourceSnapshot struct {
	Source string
	Keys   []CPAAPIKeyMetadata
}

func SyncCPAAPIKeys(db *gorm.DB, keys []string, syncedAt time.Time) error {
	metadata := make([]CPAAPIKeyMetadata, 0, len(keys))
	for _, key := range keys {
		metadata = append(metadata, CPAAPIKeyMetadata{APIKey: key})
	}
	return SyncCPAAPIKeySnapshots(db, []CPAAPIKeySourceSnapshot{{Source: entities.CPAAPIKeySourceNative, Keys: metadata}}, syncedAt)
}

func SyncCPAAPIKeySnapshots(db *gorm.DB, snapshots []CPAAPIKeySourceSnapshot, syncedAt time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		seenSources := make(map[string]struct{}, len(snapshots))
		for _, snapshot := range snapshots {
			source, err := normalizeCPAAPIKeySource(snapshot.Source)
			if err != nil {
				return err
			}
			if _, exists := seenSources[source]; exists {
				return fmt.Errorf("duplicate CPA API key source snapshot %q", source)
			}
			seenSources[source] = struct{}{}
			if err := syncCPAAPIKeySource(tx, source, snapshot.Keys, syncedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

func syncCPAAPIKeySource(tx *gorm.DB, source string, keys []CPAAPIKeyMetadata, syncedAt time.Time) error {
	var existingRows []entities.CPAAPIKey
	if err := tx.Select("id, api_key, source, is_deleted").Find(&existingRows).Error; err != nil {
		return err
	}
	existingByKey := make(map[string]entities.CPAAPIKey, len(existingRows))
	for _, row := range existingRows {
		existingByKey[row.APIKey] = row
	}

	incoming := make(map[string]struct{}, len(keys))
	toCreate := make([]entities.CPAAPIKey, 0)
	for _, item := range keys {
		item.APIKey = strings.TrimSpace(item.APIKey)
		if item.APIKey == "" {
			continue
		}
		if _, duplicate := incoming[item.APIKey]; duplicate {
			continue
		}
		incoming[item.APIKey] = struct{}{}
		displayKey := strings.TrimSpace(item.DisplayKey)
		if displayKey == "" || source == entities.CPAAPIKeySourceNative {
			displayKey = helper.RedactSensitiveValue(item.APIKey)
		}
		if existing, ok := existingByKey[item.APIKey]; ok {
			if existing.Source != source {
				return fmt.Errorf("CPA API key %q belongs to source %q, not %q", item.APIKey, existing.Source, source)
			}
			updates := map[string]any{
				"display_key":    displayKey,
				"is_deleted":     false,
				"last_synced_at": &syncedAt,
				"updated_at":     syncedAt,
			}
			if source == entities.CPAAPIKeySourceCPAKeyPolicy {
				updates["key_alias"] = strings.TrimSpace(item.KeyAlias)
			}
			if err := tx.Model(&entities.CPAAPIKey{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return err
			}
			continue
		}
		toCreate = append(toCreate, entities.CPAAPIKey{
			APIKey:       item.APIKey,
			DisplayKey:   displayKey,
			KeyAlias:     strings.TrimSpace(item.KeyAlias),
			Source:       source,
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
		if row.Source != source || row.IsDeleted {
			continue
		}
		if _, ok := incoming[row.APIKey]; !ok {
			staleIDs = append(staleIDs, row.ID)
		}
	}
	if len(staleIDs) == 0 {
		return nil
	}
	return tx.Model(&entities.CPAAPIKey{}).Where("id IN ?", staleIDs).Updates(map[string]any{"is_deleted": true, "updated_at": syncedAt}).Error
}

func normalizeCPAAPIKeySource(source string) (string, error) {
	source = strings.TrimSpace(source)
	switch source {
	case entities.CPAAPIKeySourceNative, entities.CPAAPIKeySourceCPAKeyPolicy:
		return source, nil
	default:
		return "", fmt.Errorf("unsupported CPA API key source %q", source)
	}
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
	err := db.Where("api_key = ? AND source = ? AND is_deleted = ?", apiKey, entities.CPAAPIKeySourceNative, false).First(&row).Error
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
