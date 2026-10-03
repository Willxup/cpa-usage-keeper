package repository

import (
	"cpa-usage-keeper/internal/entities"
	"crypto/sha256"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Shared metadata updates labels only, including revoked keys. Missing metadata
// leaves existing Keeper aliases intact; explicit empty values clear the alias.
func SyncCPAAPIKeyNames(db *gorm.DB, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []entities.CPAAPIKey
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(row.APIKey)))
			name, exists := names[fingerprint]
			if !exists {
				continue
			}
			name = strings.TrimSpace(name)
			if utf8.RuneCountInString(name) > 128 {
				return fmt.Errorf("invalid shared key name")
			}
			for _, char := range name {
				if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
					return fmt.Errorf("invalid shared key name")
				}
			}
			if name != "" && strings.Contains(name, row.APIKey) {
				return fmt.Errorf("shared name contains key")
			}
			if row.KeyAlias == name {
				continue
			}
			if row.KeyAlias != "" {
				backup := entities.AppSetting{SettingKey: fmt.Sprintf("api_key_names.previous_alias.%d", row.ID), Value: new(row.KeyAlias), ValueType: "string"}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&backup).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&entities.CPAAPIKey{}).Where("id = ?", row.ID).Update("key_alias", name).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
