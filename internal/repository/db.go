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
	FetchedAt   time.Time
	CPABaseURL  string
	ExportedAt  *time.Time
	Version     string
	Status      string
	HTTPStatus  int
	PayloadHash string
	RawPayload  []byte
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

func OpenDatabase(cfg config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(cfg.SQLitePath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %s: %w", filepath.Clean(cfg.SQLitePath), err)
	}

	if err := db.AutoMigrate(models.All()...); err != nil {
		return nil, fmt.Errorf("auto migrate database: %w", err)
	}

	return db, nil
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
		"status":          strings.TrimSpace(result.Status),
		"http_status":     result.HTTPStatus,
		"backup_file_path": strings.TrimSpace(result.BackupFilePath),
		"error_message":   strings.TrimSpace(result.ErrorMessage),
		"inserted_events": result.InsertedEvents,
		"deduped_events":  result.DedupedEvents,
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

	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_key"}},
		DoNothing: true,
	}).Create(&events)
	if result.Error != nil {
		return 0, 0, fmt.Errorf("insert usage events: %w", result.Error)
	}

	inserted := int(result.RowsAffected)
	deduped := len(events) - inserted
	return inserted, deduped, nil
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
