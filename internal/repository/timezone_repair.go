package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const projectTimezoneSettingKey = "keeper.project_timezone"

// RepairProjectTimezone keeps project-timezone text storage and all derived buckets
// in sync when TZ changes between process starts. The backup callback is invoked
// only when a repair is needed and must create a consistent database snapshot.
func RepairProjectTimezone(db *gorm.DB, backup func(context.Context) error) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if !db.Migrator().HasTable(&entities.AppSetting{}) {
		return fmt.Errorf("app settings table is missing")
	}

	zone := time.Local.String()
	setting, found, err := GetAppSetting(context.Background(), db, projectTimezoneSettingKey)
	if err != nil {
		return err
	}
	previousZone := ""
	if found && setting.Value != nil {
		previousZone = decodeProjectTimezone(*setting.Value)
	}
	if found && previousZone == zone {
		return nil
	}
	hasData, err := hasUsageData(db)
	if err != nil {
		return err
	}
	if !hasData {
		return saveProjectTimezoneMarker(db, zone)
	}
	previousLocation := time.Local
	if previousZone != "" {
		previousLocation, err = time.LoadLocation(previousZone)
		if err != nil {
			return fmt.Errorf("load previous project timezone %q: %w", previousZone, err)
		}
	}

	// A missing marker is treated as a first-run migration. This is deliberate:
	// databases created before the marker existed need the same repair once.
	if backup == nil {
		return fmt.Errorf("project timezone changed to %q but no backup callback was provided", zone)
	}
	if err := backup(context.Background()); err != nil {
		return fmt.Errorf("backup before project timezone repair: %w", err)
	}

	now := timeutil.NormalizeStorageTime(time.Now())
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := normalizeUsageEventTimestamps(tx, previousLocation); err != nil {
			return err
		}
		if err := rebuildTimezoneRollups(tx, now); err != nil {
			return err
		}
		if err := saveProjectTimezoneMarkerInTransaction(tx, zone, now); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("repair project timezone %q: %w", zone, err)
	}
	return nil
}

func hasUsageData(db *gorm.DB) (bool, error) {
	for _, table := range []string{
		"usage_events",
		"usage_events_archive",
		"usage_overview_hourly_stats",
		"usage_overview_daily_stats",
		"usage_activity_stats",
		"usage_latency_stats",
	} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM " + table).Scan(&count).Error; err != nil {
			return false, fmt.Errorf("count %s rows: %w", table, err)
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func saveProjectTimezoneMarker(db *gorm.DB, zone string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		return saveProjectTimezoneMarkerInTransaction(tx, zone, timeutil.NormalizeStorageTime(time.Now()))
	})
}

func saveProjectTimezoneMarkerInTransaction(tx *gorm.DB, zone string, now time.Time) error {
	encodedZone, err := json.Marshal(zone)
	if err != nil {
		return fmt.Errorf("encode project timezone: %w", err)
	}
	value := string(encodedZone)
	if _, err := UpsertAppSetting(context.Background(), tx, entities.AppSetting{
		SettingKey: projectTimezoneSettingKey,
		Value:      &value,
		ValueType:  entities.AppSettingValueTypeJSON,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		return err
	}
	return nil
}

func decodeProjectTimezone(value string) string {
	var zone string
	if json.Unmarshal([]byte(value), &zone) == nil {
		return strings.TrimSpace(zone)
	}
	return strings.TrimSpace(value)
}

func normalizeUsageEventTimestamps(tx *gorm.DB, previousLocation *time.Location) error {
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if !tx.Migrator().HasTable(table) {
			continue
		}
		var rows []struct {
			ID        int64
			Timestamp *string
			CreatedAt *string
		}
		if err := tx.Raw("SELECT id, CAST(timestamp AS TEXT) AS timestamp, CAST(created_at AS TEXT) AS created_at FROM " + table + " WHERE timestamp IS NOT NULL OR created_at IS NOT NULL").Scan(&rows).Error; err != nil {
			return fmt.Errorf("read %s timestamps: %w", table, err)
		}
		for _, row := range rows {
			updates := map[string]any{}
			for column, value := range map[string]*string{"timestamp": row.Timestamp, "created_at": row.CreatedAt} {
				if value == nil {
					continue
				}
				parsed, err := timeutil.ParseStorageTimeInLocation(*value, previousLocation)
				if err != nil {
					return fmt.Errorf("parse %s %s %d: %w", table, column, row.ID, err)
				}
				updates[column] = timeutil.FormatStorageTime(parsed)
			}
			if len(updates) > 0 {
				if err := tx.Table(table).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
					return fmt.Errorf("normalize %s timestamps %d: %w", table, row.ID, err)
				}
			}
		}
	}
	return nil
}
