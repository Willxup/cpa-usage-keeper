package test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
)

func TestRepairProjectTimezoneReformatsEventsAndRebuildsRollups(t *testing.T) {
	previousLocal := time.Local
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load New York timezone: %v", err)
	}
	time.Local = newYork
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "timezone-repair.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	event := entities.UsageEvent{
		EventKey: "timezone-repair-event", APIGroupKey: "provider-a", Model: "model-a",
		Timestamp: time.Date(2026, 10, 2, 13, 0, 0, 0, newYork), InputTokens: 10, TotalTokens: 10,
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	// Simulate a database written before TZ changed from Shanghai to New York.
	oldTimestamp := "2026-10-02T13:00:00+08:00"
	if err := db.Exec("UPDATE usage_events SET timestamp = ?, created_at = ? WHERE event_key = ?", oldTimestamp, oldTimestamp, event.EventKey).Error; err != nil {
		t.Fatalf("seed legacy timestamp: %v", err)
	}
	oldZone, _ := json.Marshal("Asia/Shanghai")
	oldZoneValue := string(oldZone)
	if _, err := repository.UpsertAppSetting(context.Background(), db, entities.AppSetting{
		SettingKey: "keeper.project_timezone", Value: &oldZoneValue, ValueType: entities.AppSettingValueTypeJSON,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed previous timezone marker: %v", err)
	}

	backups := 0
	if err := repository.RepairProjectTimezone(db, func(context.Context) error { backups++; return nil }); err != nil {
		t.Fatalf("repair project timezone: %v", err)
	}
	if backups != 1 {
		t.Fatalf("backup callback count = %d, want 1", backups)
	}

	var stored struct {
		Timestamp string
		CreatedAt string
	}
	if err := db.Raw("SELECT timestamp, created_at FROM usage_events WHERE event_key = ?", event.EventKey).Scan(&stored).Error; err != nil {
		t.Fatalf("read repaired event timestamp: %v", err)
	}
	wantTimestamp := "2026-10-02T01:00:00-04:00"
	if stored.Timestamp != wantTimestamp || stored.CreatedAt != wantTimestamp {
		t.Fatalf("stored event times = timestamp %q, created_at %q; want %q", stored.Timestamp, stored.CreatedAt, wantTimestamp)
	}

	var daily entities.UsageOverviewDailyStat
	if err := db.Where("api_group_key = ?", "provider-a").Take(&daily).Error; err != nil {
		t.Fatalf("read daily rollup: %v", err)
	}
	if got := timeutil.FormatStorageTime(daily.BucketStart); got != "2026-10-02T00:00:00-04:00" || daily.RequestCount != 1 || daily.TotalTokens != 10 {
		t.Fatalf("unexpected daily rollup: bucket=%q requests=%d tokens=%d", got, daily.RequestCount, daily.TotalTokens)
	}

	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatalf("read overview checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 1 {
		t.Fatalf("overview checkpoint = %d, want 1", checkpoint.LastAggregatedUsageEventID)
	}
	if err := repository.RepairProjectTimezone(db, func(context.Context) error { backups++; return nil }); err != nil {
		t.Fatalf("repeat repair project timezone: %v", err)
	}
	if backups != 1 {
		t.Fatalf("backup callback count after idempotent repair = %d, want 1", backups)
	}
}

func TestRepairProjectTimezoneParsesLegacyWallClockInPreviousLocation(t *testing.T) {
	previousLocal := time.Local
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load New York timezone: %v", err)
	}
	time.Local = newYork
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "timezone-repair-legacy.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	event := entities.UsageEvent{EventKey: "legacy-wall-clock", APIGroupKey: "provider-a", Model: "model-a", Timestamp: time.Now()}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	legacyWallClock := "2026-10-02 13:00:00"
	if err := db.Exec("UPDATE usage_events SET timestamp = ?, created_at = ? WHERE event_key = ?", legacyWallClock, legacyWallClock, event.EventKey).Error; err != nil {
		t.Fatalf("seed legacy wall-clock timestamp: %v", err)
	}
	oldZone, _ := json.Marshal("Asia/Shanghai")
	oldZoneValue := string(oldZone)
	if _, err := repository.UpsertAppSetting(context.Background(), db, entities.AppSetting{SettingKey: "keeper.project_timezone", Value: &oldZoneValue, ValueType: entities.AppSettingValueTypeJSON, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("seed previous timezone marker: %v", err)
	}

	if err := repository.RepairProjectTimezone(db, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("repair project timezone: %v", err)
	}
	var stored struct {
		Timestamp string
		CreatedAt string
	}
	if err := db.Raw("SELECT timestamp, created_at FROM usage_events WHERE event_key = ?", event.EventKey).Scan(&stored).Error; err != nil {
		t.Fatalf("read repaired event: %v", err)
	}
	if stored.Timestamp != "2026-10-02T01:00:00-04:00" || stored.CreatedAt != stored.Timestamp {
		t.Fatalf("legacy wall-clock converted to timestamp=%q created_at=%q", stored.Timestamp, stored.CreatedAt)
	}
}

func TestRepairProjectTimezoneRebuildsArchiveOnlyDataAndPreservesCheckpoint(t *testing.T) {
	previousLocal := time.Local
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load New York timezone: %v", err)
	}
	time.Local = newYork
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "timezone-repair-archive.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	const eventID = 7
	oldTimestamp := "2026-10-02T13:00:00+08:00"
	if err := db.Exec("INSERT INTO usage_events_archive (id, event_key, api_group_key, model, timestamp, created_at, input_tokens, total_tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", eventID, "archive-event", "archive-provider", "model-a", oldTimestamp, oldTimestamp, 3, 3).Error; err != nil {
		t.Fatalf("seed archive event: %v", err)
	}
	now := time.Now()
	for _, name := range []entities.UsageAggregationCheckpointName{entities.UsageAggregationCheckpointOverview, entities.UsageAggregationCheckpointActivity, entities.UsageAggregationCheckpointLatency} {
		if err := db.Create(&entities.UsageAggregationCheckpoint{Name: name, LastAggregatedUsageEventID: eventID, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatalf("seed checkpoint %q: %v", name, err)
		}
	}
	oldZone, _ := json.Marshal("Asia/Shanghai")
	oldZoneValue := string(oldZone)
	if _, err := repository.UpsertAppSetting(context.Background(), db, entities.AppSetting{SettingKey: "keeper.project_timezone", Value: &oldZoneValue, ValueType: entities.AppSettingValueTypeJSON, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed previous timezone marker: %v", err)
	}

	backups := 0
	if err := repository.RepairProjectTimezone(db, func(context.Context) error { backups++; return nil }); err != nil {
		t.Fatalf("repair project timezone: %v", err)
	}
	if backups != 1 {
		t.Fatalf("backup callback count = %d, want 1", backups)
	}
	var stored struct {
		Timestamp string
		CreatedAt string
	}
	if err := db.Raw("SELECT timestamp, created_at FROM usage_events_archive WHERE id = ?", eventID).Scan(&stored).Error; err != nil {
		t.Fatalf("read repaired archive event: %v", err)
	}
	if stored.Timestamp != "2026-10-02T01:00:00-04:00" || stored.CreatedAt != stored.Timestamp {
		t.Fatalf("archive event times = timestamp %q, created_at %q", stored.Timestamp, stored.CreatedAt)
	}
	var daily entities.UsageOverviewDailyStat
	if err := db.Where("api_group_key = ?", "archive-provider").Take(&daily).Error; err != nil {
		t.Fatalf("read archive daily rollup: %v", err)
	}
	if daily.RequestCount != 1 || daily.TotalTokens != 3 {
		t.Fatalf("archive daily rollup = requests %d tokens %d", daily.RequestCount, daily.TotalTokens)
	}
	var checkpoints []entities.UsageAggregationCheckpoint
	if err := db.Order("name").Find(&checkpoints).Error; err != nil {
		t.Fatalf("read repaired checkpoints: %v", err)
	}
	if len(checkpoints) != 3 {
		t.Fatalf("checkpoint count = %d, want 3", len(checkpoints))
	}
	for _, checkpoint := range checkpoints {
		if checkpoint.LastAggregatedUsageEventID != eventID {
			t.Fatalf("checkpoint %q = %d, want %d", checkpoint.Name, checkpoint.LastAggregatedUsageEventID, eventID)
		}
	}
}

func TestRepairProjectTimezoneClearsStaleRollupsWithoutRawEvents(t *testing.T) {
	previousLocal := time.Local
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load New York timezone: %v", err)
	}
	time.Local = newYork
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "timezone-repair-stale-rollup.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: time.Now(), APIGroupKey: "stale", Model: "model", RequestCount: 1}).Error; err != nil {
		t.Fatalf("seed stale rollup: %v", err)
	}
	oldZone, _ := json.Marshal("Asia/Shanghai")
	oldZoneValue := string(oldZone)
	if _, err := repository.UpsertAppSetting(context.Background(), db, entities.AppSetting{SettingKey: "keeper.project_timezone", Value: &oldZoneValue, ValueType: entities.AppSettingValueTypeJSON, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("seed previous timezone marker: %v", err)
	}
	backups := 0
	if err := repository.RepairProjectTimezone(db, func(context.Context) error { backups++; return nil }); err != nil {
		t.Fatalf("repair project timezone: %v", err)
	}
	if backups != 1 {
		t.Fatalf("backup callback count = %d, want 1", backups)
	}
	var count int64
	if err := db.Model(&entities.UsageOverviewDailyStat{}).Count(&count).Error; err != nil {
		t.Fatalf("count repaired rollups: %v", err)
	}
	if count != 0 {
		t.Fatalf("stale daily rollups remaining = %d, want 0", count)
	}
}

func TestRepairProjectTimezoneRollsBackOnMalformedTimestamp(t *testing.T) {
	previousLocal := time.Local
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load New York timezone: %v", err)
	}
	time.Local = newYork
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "timezone-repair-rollback.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	event := entities.UsageEvent{EventKey: "malformed-time", APIGroupKey: "provider-a", Model: "model-a", Timestamp: time.Now()}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: time.Now(), APIGroupKey: "stale", Model: "model", RequestCount: 1}).Error; err != nil {
		t.Fatalf("seed rollup: %v", err)
	}
	if err := db.Exec("UPDATE usage_events SET timestamp = ? WHERE event_key = ?", "not-a-time", event.EventKey).Error; err != nil {
		t.Fatalf("seed malformed timestamp: %v", err)
	}
	oldZone, _ := json.Marshal("Asia/Shanghai")
	oldZoneValue := string(oldZone)
	if _, err := repository.UpsertAppSetting(context.Background(), db, entities.AppSetting{SettingKey: "keeper.project_timezone", Value: &oldZoneValue, ValueType: entities.AppSettingValueTypeJSON, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("seed previous timezone marker: %v", err)
	}
	backups := 0
	if err := repository.RepairProjectTimezone(db, func(context.Context) error { backups++; return nil }); err == nil {
		t.Fatal("repair succeeded with malformed timestamp")
	}
	if backups != 1 {
		t.Fatalf("backup callback count = %d, want 1", backups)
	}
	var storedTimestamp string
	if err := db.Raw("SELECT quote(timestamp) FROM usage_events WHERE event_key = ?", event.EventKey).Scan(&storedTimestamp).Error; err != nil {
		t.Fatalf("read rolled-back event: %v", err)
	}
	if storedTimestamp != "'not-a-time'" {
		t.Fatalf("malformed timestamp changed to %q", storedTimestamp)
	}
	var count int64
	if err := db.Model(&entities.UsageOverviewDailyStat{}).Count(&count).Error; err != nil {
		t.Fatalf("count rolled-back rollups: %v", err)
	}
	if count != 1 {
		t.Fatalf("rollup count after rollback = %d, want 1", count)
	}
	var marker entities.AppSetting
	if err := db.Where("setting_key = ?", "keeper.project_timezone").Take(&marker).Error; err != nil {
		t.Fatalf("read timezone marker: %v", err)
	}
	var markerZone string
	if marker.Value != nil {
		_ = json.Unmarshal([]byte(*marker.Value), &markerZone)
	}
	if markerZone != "Asia/Shanghai" {
		t.Fatalf("timezone marker changed after rollback: %v", marker.Value)
	}
}
