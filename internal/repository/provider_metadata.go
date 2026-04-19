package repository

import (
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProviderMetadataInput struct {
	LookupKey    string
	ProviderType string
	DisplayName  string
	ProviderKey  string
	MatchKind    string
}

func ReplaceProviderMetadata(db *gorm.DB, items []ProviderMetadataInput) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	normalized := make([]models.ProviderMetadata, 0, len(items))
	lookupKeys := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		lookupKey := strings.TrimSpace(item.LookupKey)
		if lookupKey == "" {
			continue
		}
		if _, ok := seen[lookupKey]; ok {
			continue
		}
		seen[lookupKey] = struct{}{}
		lookupKeys = append(lookupKeys, lookupKey)
		normalized = append(normalized, models.ProviderMetadata{
			LookupKey:    lookupKey,
			ProviderType: strings.TrimSpace(item.ProviderType),
			DisplayName:  strings.TrimSpace(item.DisplayName),
			ProviderKey:  strings.TrimSpace(item.ProviderKey),
			MatchKind:    strings.TrimSpace(item.MatchKind),
		})
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if len(normalized) == 0 {
			if err := tx.Where("1 = 1").Delete(&models.ProviderMetadata{}).Error; err != nil {
				return fmt.Errorf("soft delete provider metadata: %w", err)
			}
			return nil
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "lookup_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"provider_type",
				"display_name",
				"provider_key",
				"match_kind",
				"updated_at",
				"deleted_at",
			}),
		}).Create(&normalized).Error; err != nil {
			return fmt.Errorf("upsert provider metadata: %w", err)
		}

		if err := tx.Where("lookup_key NOT IN ?", lookupKeys).Delete(&models.ProviderMetadata{}).Error; err != nil {
			return fmt.Errorf("soft delete stale provider metadata: %w", err)
		}

		return nil
	})
}

func ListProviderMetadata(db *gorm.DB) ([]models.ProviderMetadata, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var items []models.ProviderMetadata
	if err := db.Order("provider_type asc, display_name asc, lookup_key asc").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list provider metadata: %w", err)
	}
	return items, nil
}
