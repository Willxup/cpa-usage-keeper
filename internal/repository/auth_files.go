package repository

import (
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuthFileInput struct {
	AuthIndex   string
	Name        string
	Email       string
	Type        string
	Provider    string
	Label       string
	Status      string
	Source      string
	Disabled    bool
	Unavailable bool
	RuntimeOnly bool
}

func ReplaceAuthFiles(db *gorm.DB, files []AuthFileInput) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	normalized := make([]models.AuthFile, 0, len(files))
	authIndexes := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		authIndex := strings.TrimSpace(file.AuthIndex)
		if authIndex == "" {
			continue
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		authIndexes = append(authIndexes, authIndex)
		normalized = append(normalized, models.AuthFile{
			AuthIndex:   authIndex,
			Name:        strings.TrimSpace(file.Name),
			Email:       strings.TrimSpace(file.Email),
			Type:        strings.TrimSpace(file.Type),
			Provider:    strings.TrimSpace(file.Provider),
			Label:       strings.TrimSpace(file.Label),
			Status:      strings.TrimSpace(file.Status),
			Source:      strings.TrimSpace(file.Source),
			Disabled:    file.Disabled,
			Unavailable: file.Unavailable,
			RuntimeOnly: file.RuntimeOnly,
		})
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if len(normalized) == 0 {
			if err := tx.Where("1 = 1").Delete(&models.AuthFile{}).Error; err != nil {
				return fmt.Errorf("soft delete auth files: %w", err)
			}
			return nil
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "auth_index"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name",
				"email",
				"type",
				"provider",
				"label",
				"status",
				"source",
				"disabled",
				"unavailable",
				"runtime_only",
				"updated_at",
				"deleted_at",
			}),
		}).Create(&normalized).Error; err != nil {
			return fmt.Errorf("upsert auth files: %w", err)
		}

		if err := tx.Where("auth_index NOT IN ?", authIndexes).Delete(&models.AuthFile{}).Error; err != nil {
			return fmt.Errorf("soft delete stale auth files: %w", err)
		}

		return nil
	})
}

func ListAuthFiles(db *gorm.DB) ([]models.AuthFile, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var files []models.AuthFile
	if err := db.Order("auth_index asc").Find(&files).Error; err != nil {
		return nil, fmt.Errorf("list auth files: %w", err)
	}
	return files, nil
}
