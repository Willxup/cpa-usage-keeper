package repository

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SnapshotRunInput struct {
	FetchedAt    time.Time
	CPABaseURL   string
	ExportedAt   *time.Time
	Version      string
	Status       string
	HTTPStatus   int
	PayloadHash  string
	RawPayload   []byte
	ErrorMessage string
}

type SnapshotRunResult struct {
	Status         string
	HTTPStatus     int
	BackupFilePath string
	ErrorMessage   string
	InsertedEvents int
	DedupedEvents  int
	ExportedAt     *time.Time
}

type SnapshotRunsCleanupResult struct {
	Deleted int64
}

type StorageCleanupResult struct {
	RedisInbox   RedisUsageInboxCleanupResult
	SnapshotRuns SnapshotRunsCleanupResult
}

func OpenDatabase(cfg config.Config) (*gorm.DB, error) {
	dsn := sqliteDSN(cfg.SQLitePath)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %s: %w", filepath.Clean(cfg.SQLitePath), err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("configure sqlite database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return nil, fmt.Errorf("enable sqlite WAL: %w", err)
	}
	if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
		return nil, fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	if err := db.Exec("PRAGMA foreign_keys=ON").Error; err != nil {
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	if err := db.AutoMigrate(models.All()...); err != nil {
		return nil, fmt.Errorf("auto migrate database: %w", err)
	}

	return db, nil
}

func sqliteDSN(path string) string {
	trimmed := strings.TrimSpace(path)
	if strings.Contains(trimmed, "?") {
		return trimmed
	}
	return trimmed + "?_busy_timeout=5000&_foreign_keys=on"
}

func CreateSnapshotRun(db *gorm.DB, input SnapshotRunInput) (*models.SnapshotRun, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	run := &models.SnapshotRun{
		FetchedAt:    input.FetchedAt.UTC(),
		CPABaseURL:   strings.TrimSpace(input.CPABaseURL),
		ExportedAt:   normalizeOptionalTime(input.ExportedAt),
		Version:      strings.TrimSpace(input.Version),
		Status:       strings.TrimSpace(input.Status),
		HTTPStatus:   input.HTTPStatus,
		PayloadHash:  strings.TrimSpace(input.PayloadHash),
		RawPayload:   append([]byte(nil), input.RawPayload...),
		ErrorMessage: strings.TrimSpace(input.ErrorMessage),
	}
	if run.Status == "" {
		run.Status = "pending"
	}

	if err := db.Create(run).Error; err != nil {
		return nil, fmt.Errorf("create snapshot run: %w", err)
	}

	return run, nil
}

func FinalizeSnapshotRun(db *gorm.DB, snapshotRunID uint, result SnapshotRunResult) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	updates := map[string]any{
		"status":           strings.TrimSpace(result.Status),
		"http_status":      result.HTTPStatus,
		"backup_file_path": strings.TrimSpace(result.BackupFilePath),
		"error_message":    strings.TrimSpace(result.ErrorMessage),
		"inserted_events":  result.InsertedEvents,
		"deduped_events":   result.DedupedEvents,
	}
	if updates["status"] == "" {
		updates["status"] = "completed"
	}
	if exportedAt := normalizeOptionalTime(result.ExportedAt); exportedAt != nil {
		updates["exported_at"] = *exportedAt
	}

	if err := db.Model(&models.SnapshotRun{}).Where("id = ?", snapshotRunID).Updates(updates).Error; err != nil {
		return fmt.Errorf("finalize snapshot run %d: %w", snapshotRunID, err)
	}

	return nil
}

func InsertUsageEvents(db *gorm.DB, events []models.UsageEvent) (int, int, error) {
	if db == nil {
		return 0, 0, fmt.Errorf("database is nil")
	}
	if len(events) == 0 {
		return 0, 0, nil
	}

	const batchSize = 100
	inserted := 0

	for start := 0; start < len(events); start += batchSize {
		end := min(start+batchSize, len(events))
		batch := events[start:end]
		result := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_key"}},
			DoNothing: true,
		}).Create(&batch)
		if result.Error != nil {
			return 0, 0, fmt.Errorf("insert usage events: %w", result.Error)
		}
		inserted += int(result.RowsAffected)
	}

	deduped := len(events) - inserted
	return inserted, deduped, nil
}

func FindLatestUsageEventTimestamp(db *gorm.DB) (*time.Time, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var event models.UsageEvent
	if err := db.Select("timestamp").Order("timestamp DESC").Limit(1).Take(&event).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("find latest usage event timestamp: %w", err)
	}

	timestamp := event.Timestamp.UTC()
	return &timestamp, nil
}

func CleanupSnapshotRuns(db *gorm.DB, now time.Time) (SnapshotRunsCleanupResult, error) {
	if db == nil {
		return SnapshotRunsCleanupResult{}, fmt.Errorf("database is nil")
	}

	localNow := now.In(time.Local)
	localTodayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	keepIDs := make([]uint, 0, 7)
	for dayOffset := 0; dayOffset <= 7; dayOffset++ {
		dayStart := localTodayStart.AddDate(0, 0, -dayOffset).UTC()
		dayEnd := localTodayStart.AddDate(0, 0, -dayOffset+1).UTC()
		if dayOffset == 0 {
			dayEnd = now.UTC().Add(time.Nanosecond)
		}
		var dayIDs []uint
		err := db.Model(&models.SnapshotRun{}).Select("id").Where("fetched_at >= ? AND fetched_at < ?", dayStart, dayEnd).Order("fetched_at DESC, id DESC").Limit(1).Pluck("id", &dayIDs).Error
		if err != nil {
			return SnapshotRunsCleanupResult{}, fmt.Errorf("load snapshot run retained for cleanup: %w", err)
		}
		if len(dayIDs) > 0 {
			keepIDs = append(keepIDs, dayIDs[0])
		}
	}

	if len(keepIDs) == 0 {
		return SnapshotRunsCleanupResult{}, nil
	}
	deleted := db.Model(&models.SnapshotRun{}).Where("id NOT IN ?", keepIDs).Delete(&models.SnapshotRun{})
	if deleted.Error != nil {
		return SnapshotRunsCleanupResult{}, fmt.Errorf("delete old snapshot runs: %w", deleted.Error)
	}
	return SnapshotRunsCleanupResult{Deleted: deleted.RowsAffected}, nil
}

func CleanupStorage(db *gorm.DB, now time.Time) (StorageCleanupResult, error) {
	redisResult, err := CleanupRedisUsageInbox(db, now)
	if err != nil {
		return StorageCleanupResult{RedisInbox: redisResult}, err
	}
	snapshotResult, err := CleanupSnapshotRuns(db, now)
	if err != nil {
		return StorageCleanupResult{RedisInbox: redisResult, SnapshotRuns: snapshotResult}, err
	}
	if err := db.Exec("VACUUM").Error; err != nil {
		return StorageCleanupResult{RedisInbox: redisResult, SnapshotRuns: snapshotResult}, err
	}
	return StorageCleanupResult{RedisInbox: redisResult, SnapshotRuns: snapshotResult}, nil
}

func Vacuum(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	return db.Exec("VACUUM").Error
}

func FindLastSnapshotRunWithBackup(db *gorm.DB) (*models.SnapshotRun, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var run models.SnapshotRun
	if err := db.Where("status IN ? AND backup_file_path <> ?", []string{"completed", "completed_with_warnings"}, "").Order("id DESC").First(&run).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("find last snapshot run with backup: %w", err)
	}

	return &run, nil
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
