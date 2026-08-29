package repository

import (
	"context"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var appSettingColumns = []string{
	"setting_key",
	"value",
	"value_type",
	"created_at",
	"updated_at",
}

func GetAppSetting(ctx context.Context, db *gorm.DB, key string) (entities.AppSetting, bool, error) {
	if db == nil {
		return entities.AppSetting{}, false, fmt.Errorf("database is nil")
	}
	normalizedKey := strings.TrimSpace(key)
	if normalizedKey == "" {
		return entities.AppSetting{}, false, fmt.Errorf("setting key is required")
	}

	var setting entities.AppSetting
	err := db.WithContext(ctx).Select(appSettingColumns).Where(&entities.AppSetting{SettingKey: normalizedKey}).First(&setting).Error
	if err == nil {
		return setting, true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return entities.AppSetting{}, false, nil
	}
	return entities.AppSetting{}, false, fmt.Errorf("get app setting %s: %w", normalizedKey, err)
}

func GetAppSettings(ctx context.Context, db *gorm.DB, keys []string) (map[string]entities.AppSetting, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	normalizedKeys := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			return nil, fmt.Errorf("setting key is required")
		}
		if _, ok := seen[normalizedKey]; ok {
			continue
		}
		seen[normalizedKey] = struct{}{}
		normalizedKeys = append(normalizedKeys, normalizedKey)
	}
	result := make(map[string]entities.AppSetting, len(normalizedKeys))
	if len(normalizedKeys) == 0 {
		return result, nil
	}

	var settings []entities.AppSetting
	if err := db.WithContext(ctx).
		Select(appSettingColumns).
		Where("setting_key IN ?", normalizedKeys).
		Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("get app settings: %w", err)
	}
	for _, setting := range settings {
		result[setting.SettingKey] = setting
	}
	return result, nil
}

func UpsertAppSetting(ctx context.Context, db *gorm.DB, setting entities.AppSetting) (entities.AppSetting, error) {
	if db == nil {
		return entities.AppSetting{}, fmt.Errorf("database is nil")
	}
	setting.SettingKey = strings.TrimSpace(setting.SettingKey)
	if setting.SettingKey == "" {
		return entities.AppSetting{}, fmt.Errorf("setting key is required")
	}
	setting.ValueType = strings.TrimSpace(setting.ValueType)
	if setting.ValueType == "" {
		return entities.AppSetting{}, fmt.Errorf("setting value_type is required")
	}

	if err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "value_type", "updated_at"}),
	}).Create(&setting).Error; err != nil {
		return entities.AppSetting{}, fmt.Errorf("upsert app setting %s: %w", setting.SettingKey, err)
	}
	loaded, found, err := GetAppSetting(ctx, db, setting.SettingKey)
	if err != nil {
		return entities.AppSetting{}, err
	}
	if !found {
		return entities.AppSetting{}, fmt.Errorf("app setting %s was not found after upsert", setting.SettingKey)
	}
	return loaded, nil
}
