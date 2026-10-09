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

func TestAutoRefreshSettingsPersistAndSignalScheduler(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDatabase(t), NewProviderRegistry(nil))
	initial, err := service.GetAutoRefreshSettings(context.Background())
	if err != nil || initial.Enabled || initial.Schedule != nil {
		t.Fatalf("unexpected initial settings: %+v, err=%v", initial, err)
	}
	for _, schedule := range []*AutoRefreshSchedule{{Unit: AutoRefreshScheduleUnitHour, Value: 6}, nil} {
		want := AutoRefreshSettings{Enabled: true, Schedule: schedule}
		saved, err := service.UpdateAutoRefreshSettings(context.Background(), want)
		if err != nil || !reflect.DeepEqual(saved, want) {
			t.Fatalf("saved=%+v, want=%+v err=%v", saved, want, err)
		}
		loaded, err := service.GetAutoRefreshSettings(context.Background())
		if err != nil || !reflect.DeepEqual(loaded, want) {
			t.Fatalf("loaded=%+v, want=%+v err=%v", loaded, want, err)
		}
		select {
		case <-autoRefreshSettingsChanged(service):
		default:
			t.Fatal("settings update did not signal scheduler")
		}
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

func TestGetAutoRefreshSettingsReadsConsistentSnapshot(t *testing.T) {
	db := openQuotaTestDatabase(t)
	ctx := context.Background()
	initialSchedule := `{"unit":"hour","value":6}`
	if _, err := repository.UpsertAppSetting(ctx, db, entities.AppSetting{
		SettingKey: "quota.auto_refresh.enabled",
		Value:      new("true"),
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
		if setting, ok := tx.Statement.Dest.(*entities.AppSetting); ok && setting.SettingKey == "quota.auto_refresh.schedule" {
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
	for _, schedule := range []AutoRefreshSchedule{{Unit: AutoRefreshScheduleUnitMinute, Value: 61}, {Unit: AutoRefreshScheduleUnitWeek, Value: 0}} {
		_, err := service.UpdateAutoRefreshSettings(context.Background(), AutoRefreshSettings{Enabled: true, Schedule: &schedule})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid schedule %+v returned %v", schedule, err)
		}
	}
}

func statementIncludesSettingKey(tx *gorm.DB, key string) bool {
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
