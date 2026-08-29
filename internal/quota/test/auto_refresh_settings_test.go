package test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestAutoRefreshSettingsDefaultToDisabledWithoutSchedule(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDatabase(t), NewProviderRegistry(nil))

	settings, err := service.GetAutoRefreshSettings(context.Background())
	if err != nil {
		t.Fatalf("GetAutoRefreshSettings returned error: %v", err)
	}
	if settings.Enabled {
		t.Fatalf("expected auto refresh disabled by default, got %+v", settings)
	}
	if settings.Schedule != nil {
		t.Fatalf("expected nil schedule by default, got %+v", settings.Schedule)
	}
}

func TestUpdateAutoRefreshSettingsStoresTypedSchedule(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDatabase(t), NewProviderRegistry(nil))

	saved, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled: true,
		Schedule: &AutoRefreshSchedule{
			Unit:  AutoRefreshScheduleUnitHour,
			Value: 6,
		},
	})
	if err != nil {
		t.Fatalf("UpdateAutoRefreshSettings returned error: %v", err)
	}
	if !saved.Enabled || saved.Schedule == nil || saved.Schedule.Unit != AutoRefreshScheduleUnitHour || saved.Schedule.Value != 6 {
		t.Fatalf("unexpected saved settings: %+v", saved)
	}

	loaded, err := service.GetAutoRefreshSettings(context.Background())
	if err != nil {
		t.Fatalf("GetAutoRefreshSettings returned error: %v", err)
	}
	if !loaded.Enabled || loaded.Schedule == nil || loaded.Schedule.Unit != AutoRefreshScheduleUnitHour || loaded.Schedule.Value != 6 {
		t.Fatalf("unexpected loaded settings: %+v", loaded)
	}
}

func TestUpdateAutoRefreshSettingsSignalsScheduler(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDatabase(t), NewProviderRegistry(nil))

	_, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled: true,
		Schedule: &AutoRefreshSchedule{
			Unit:  AutoRefreshScheduleUnitHour,
			Value: 6,
		},
	})
	if err != nil {
		t.Fatalf("UpdateAutoRefreshSettings returned error: %v", err)
	}

	select {
	case <-autoRefreshSettingsChanged(service):
	default:
		t.Fatal("expected settings update to signal the auto refresh scheduler")
	}
}

func TestUpdateAutoRefreshSettingsResetsScheduleAnchor(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDatabase(t), NewProviderRegistry(nil))
	now := time.Date(2026, 5, 26, 10, 30, 0, 0, time.Local)
	setLastAutoRefreshRoundAt(service, now.Add(-time.Hour))
	setLastAutoRefreshAttemptAt(service, now.Add(-30*time.Minute))

	_, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitDay, Value: 30},
	})
	if err != nil {
		t.Fatalf("UpdateAutoRefreshSettings returned error: %v", err)
	}

	delay := nextAutoRefreshDelay(service, AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitDay, Value: 30},
	}, now)
	want := time.Date(2026, 5, 27, 0, 0, 0, 0, time.Local).Sub(now)
	if delay != want {
		t.Fatalf("expected updated day schedule to use fresh first-run midnight delay, got %s want %s", delay, want)
	}
}

func TestUpdateAutoRefreshSettingsAllowsEnabledWithoutSchedule(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDatabase(t), NewProviderRegistry(nil))

	saved, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{Enabled: true, Schedule: nil})
	if err != nil {
		t.Fatalf("UpdateAutoRefreshSettings returned error: %v", err)
	}
	if !saved.Enabled || saved.Schedule != nil {
		t.Fatalf("expected enabled settings with nil schedule, got %+v", saved)
	}

	loaded, err := service.GetAutoRefreshSettings(context.Background())
	if err != nil {
		t.Fatalf("GetAutoRefreshSettings returned error: %v", err)
	}
	if !loaded.Enabled || loaded.Schedule != nil {
		t.Fatalf("expected persisted enabled settings with nil schedule, got %+v", loaded)
	}
}

func TestGetAutoRefreshSettingsReadsConsistentSnapshot(t *testing.T) {
	db := openQuotaTestDatabase(t)
	ctx := context.Background()
	initialSchedule := `{"unit":"hour","value":6}`
	if _, err := repository.UpsertAppSetting(ctx, db, entities.AppSetting{
		SettingKey: "quota.auto_refresh.enabled",
		Value:      stringPointer("true"),
		ValueType:  entities.AppSettingValueTypeBool,
	}); err != nil {
		t.Fatalf("save enabled setting: %v", err)
	}
	if _, err := repository.UpsertAppSetting(ctx, db, entities.AppSetting{
		SettingKey: "quota.auto_refresh.schedule",
		Value:      &initialSchedule,
		ValueType:  entities.AppSettingValueTypeJSON,
	}); err != nil {
		t.Fatalf("save schedule setting: %v", err)
	}

	mutated := false
	db.Callback().Query().After("gorm:query").Register("mutate_auto_refresh_schedule_between_setting_reads", func(tx *gorm.DB) {
		if mutated || !statementIncludesSettingKey(tx, "quota.auto_refresh.enabled") {
			return
		}
		mutated = true
		if err := tx.Session(&gorm.Session{NewDB: true}).Exec(
			"UPDATE app_settings SET value = ? WHERE setting_key = ?",
			`{"unit":"day","value":2}`,
			"quota.auto_refresh.schedule",
		).Error; err != nil {
			tx.AddError(err)
		}
	})
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))

	loaded, err := service.GetAutoRefreshSettings(ctx)
	if err != nil {
		t.Fatalf("GetAutoRefreshSettings returned error: %v", err)
	}
	if loaded.Schedule == nil || loaded.Schedule.Unit != AutoRefreshScheduleUnitHour || loaded.Schedule.Value != 6 {
		t.Fatalf("expected settings from one consistent snapshot, got %+v", loaded)
	}
	if !mutated {
		t.Fatal("expected test hook to mutate schedule after the enabled setting read")
	}
}

func TestUpdateAutoRefreshSettingsRollsBackWhenScheduleSaveFails(t *testing.T) {
	db := openQuotaTestDatabase(t)
	db.Callback().Create().Before("gorm:create").Register("fail_schedule_setting_create", func(tx *gorm.DB) {
		if appSettingKeyFromStatement(tx) == "quota.auto_refresh.schedule" {
			tx.AddError(errors.New("forced schedule save failure"))
		}
	})
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))

	_, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitHour, Value: 6},
	})
	if err == nil || !strings.Contains(err.Error(), "forced schedule save failure") {
		t.Fatalf("expected forced schedule save failure, got %v", err)
	}

	loaded, loadErr := service.GetAutoRefreshSettings(context.Background())
	if loadErr != nil {
		t.Fatalf("GetAutoRefreshSettings returned error: %v", loadErr)
	}
	if loaded.Enabled || loaded.Schedule != nil {
		t.Fatalf("expected settings transaction to roll back, got %+v", loaded)
	}
}

func TestUpdateAutoRefreshSettingsValidatesScheduleRange(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDatabase(t), NewProviderRegistry(nil))

	_, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitMinute, Value: 61},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error for oversized minute value, got %v", err)
	}

	_, err = service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{
		Enabled:  true,
		Schedule: &AutoRefreshSchedule{Unit: AutoRefreshScheduleUnitWeek, Value: 0},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error for invalid weekday, got %v", err)
	}
}

func appSettingKeyFromStatement(tx *gorm.DB) string {
	if tx == nil || !tx.Statement.ReflectValue.IsValid() {
		return ""
	}
	value := tx.Statement.ReflectValue
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() {
		return ""
	}
	for _, name := range []string{"SettingKey", "Key"} {
		field := value.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
	}
	return ""
}

func statementIncludesSettingKey(tx *gorm.DB, key string) bool {
	if tx == nil || tx.Statement == nil {
		return false
	}
	for _, variable := range tx.Statement.Vars {
		if value, ok := variable.(string); ok && value == key {
			return true
		}
		if values, ok := variable.([]string); ok {
			for _, value := range values {
				if value == key {
					return true
				}
			}
		}
	}
	return false
}

func stringPointer(value string) *string {
	return &value
}
