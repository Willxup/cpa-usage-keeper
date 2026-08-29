package test

import (
	"context"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestAppSettingsUpsertAndReadNullableValue(t *testing.T) {
	db := openAppSettingsRepositoryDatabase(t)
	ctx := context.Background()
	key := "quota.auto_refresh.schedule"

	setting, found, err := repository.GetAppSetting(ctx, db, key)
	if err != nil {
		t.Fatalf("GetAppSetting returned error: %v", err)
	}
	if found {
		t.Fatalf("expected missing setting, got %+v", setting)
	}

	saved, err := repository.UpsertAppSetting(ctx, db, entities.AppSetting{
		SettingKey: key,
		Value:      nil,
		ValueType:  entities.AppSettingValueTypeJSON,
	})
	if err != nil {
		t.Fatalf("UpsertAppSetting returned error: %v", err)
	}
	if saved.SettingKey != key || saved.Value != nil || saved.ValueType != entities.AppSettingValueTypeJSON {
		t.Fatalf("unexpected saved setting: %+v", saved)
	}

	loaded, found, err := repository.GetAppSetting(ctx, db, key)
	if err != nil {
		t.Fatalf("GetAppSetting after save returned error: %v", err)
	}
	if !found || loaded.SettingKey != key || loaded.Value != nil || loaded.ValueType != entities.AppSettingValueTypeJSON {
		t.Fatalf("unexpected loaded setting: found=%v setting=%+v", found, loaded)
	}

	value := `{"unit":"hour","value":6}`
	updated, err := repository.UpsertAppSetting(ctx, db, entities.AppSetting{
		SettingKey: key,
		Value:      &value,
		ValueType:  entities.AppSettingValueTypeJSON,
	})
	if err != nil {
		t.Fatalf("UpsertAppSetting update returned error: %v", err)
	}
	if updated.Value == nil || *updated.Value != value {
		t.Fatalf("expected JSON value to update, got %+v", updated.Value)
	}
}

func TestAppSettingsUsesPortableSettingKeyColumn(t *testing.T) {
	db := openAppSettingsRepositoryDatabase(t)

	columns := appSettingsColumnNames(t, db)
	if !columns["setting_key"] {
		t.Fatal("expected app_settings to use setting_key column")
	}
	if columns["key"] {
		t.Fatal("expected app_settings to avoid key column")
	}
}

func appSettingsColumnNames(t *testing.T, db *gorm.DB) map[string]bool {
	t.Helper()
	type columnInfo struct {
		Name string `gorm:"column:name"`
	}
	var rows []columnInfo
	if err := db.Raw("PRAGMA table_info(app_settings)").Scan(&rows).Error; err != nil {
		t.Fatalf("read app_settings columns: %v", err)
	}
	columns := make(map[string]bool, len(rows))
	for _, row := range rows {
		columns[row.Name] = true
	}
	return columns
}

func openAppSettingsRepositoryDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app-settings.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
