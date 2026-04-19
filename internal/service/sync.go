package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/backup"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type ExportFetcher interface {
	FetchUsageExport(ctx context.Context) (*cpa.ExportResult, error)
}

type BackupWriter interface {
	Write(snapshotRunID uint, fetchedAt time.Time, payload []byte) (string, error)
}

type SyncService struct {
	db             *gorm.DB
	client         ExportFetcher
	baseURL        string
	backupEnabled  bool
	backupInterval time.Duration
	backupWriter   BackupWriter
	now            func() time.Time
}

type SyncResult struct {
	SnapshotRunID  uint
	Status         string
	HTTPStatus     int
	InsertedEvents int
	DedupedEvents  int
	PayloadHash    string
	ExportedAt     *time.Time
	BackupFilePath string
}

func NewSyncService(db *gorm.DB, cfg config.Config) *SyncService {
	var writer BackupWriter
	if cfg.BackupEnabled {
		writer = backup.NewWriter(cfg.BackupDir)
	}
	return NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:        cfg.CPABaseURL,
		Client:         cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout),
		BackupEnabled:  cfg.BackupEnabled,
		BackupWriter:   writer,
		BackupInterval: cfg.BackupInterval,
	})
}

type SyncServiceOptions struct {
	BaseURL        string
	Client         ExportFetcher
	BackupEnabled  bool
	BackupWriter   BackupWriter
	BackupInterval time.Duration
	Now            func() time.Time
}

func NewSyncServiceWithOptions(db *gorm.DB, opts SyncServiceOptions) *SyncService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &SyncService{
		db:             db,
		client:         opts.Client,
		baseURL:        strings.TrimSpace(opts.BaseURL),
		backupEnabled:  opts.BackupEnabled,
		backupInterval: opts.BackupInterval,
		backupWriter:   opts.BackupWriter,
		now:            now,
	}
}

func NewSyncServiceWithClient(db *gorm.DB, baseURL string, client ExportFetcher) *SyncService {
	return NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: baseURL,
		Client:  client,
	})
}

func (s *SyncService) SyncOnce(ctx context.Context) error {
	_, err := s.syncOnce(ctx)
	return err
}

func (s *SyncService) SyncNow(ctx context.Context) (*SyncResult, error) {
	return s.syncOnce(ctx)
}

func (s *SyncService) syncOnce(ctx context.Context) (*SyncResult, error) {
	if s == nil {
		return nil, fmt.Errorf("sync service is nil")
	}
	if s.db == nil {
		return nil, fmt.Errorf("sync service database is nil")
	}
	if s.client == nil {
		return nil, fmt.Errorf("sync service client is nil")
	}

	fetchedAt := s.now().UTC()
	fetchResult, fetchErr := s.client.FetchUsageExport(ctx)

	var (
		httpStatus int
		rawPayload []byte
		payloadHash string
		exportedAt *time.Time
		version string
	)
	if fetchResult != nil {
		httpStatus = fetchResult.StatusCode
		rawPayload = append([]byte(nil), fetchResult.Body...)
		payloadHash = hashPayload(rawPayload)
		if !fetchResult.Payload.ExportedAt.IsZero() {
			normalized := fetchResult.Payload.ExportedAt.UTC()
			exportedAt = &normalized
		}
		if fetchResult.Payload.Version > 0 {
			version = fmt.Sprintf("%d", fetchResult.Payload.Version)
		}
	}

	snapshotRun, err := repository.CreateSnapshotRun(s.db, repository.SnapshotRunInput{
		FetchedAt:    fetchedAt,
		CPABaseURL:   s.baseURL,
		ExportedAt:   exportedAt,
		Version:      version,
		Status:       initialSnapshotStatus(fetchErr),
		HTTPStatus:   httpStatus,
		PayloadHash:  payloadHash,
		RawPayload:   rawPayload,
		ErrorMessage: errorMessage(fetchErr),
	})
	if err != nil {
		return nil, err
	}

	if fetchErr != nil {
		finalizeErr := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
			Status:       "failed",
			HTTPStatus:   httpStatus,
			ErrorMessage: errorMessage(fetchErr),
			ExportedAt:   exportedAt,
		})
		if finalizeErr != nil {
			return nil, fmt.Errorf("fetch usage export: %v; finalize snapshot run: %w", fetchErr, finalizeErr)
		}
		return &SyncResult{
			SnapshotRunID: snapshotRun.ID,
			Status:        "failed",
			HTTPStatus:    httpStatus,
			PayloadHash:   payloadHash,
			ExportedAt:    exportedAt,
		}, fmt.Errorf("fetch usage export: %w", fetchErr)
	}

	lastBackupSnapshotRun, err := repository.FindLastSnapshotRunWithBackup(s.db)
	if err != nil {
		finalizeErr := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
			Status:       "failed",
			HTTPStatus:   httpStatus,
			ErrorMessage: errorMessage(err),
			ExportedAt:   exportedAt,
		})
		if finalizeErr != nil {
			return nil, fmt.Errorf("load last backup snapshot run: %v; finalize snapshot run: %w", err, finalizeErr)
		}
		return nil, fmt.Errorf("load last backup snapshot run: %w", err)
	}

	backupFilePath := ""
	if s.shouldWriteBackup(fetchedAt, lastBackupSnapshotRun) {
		backupFilePath, err = s.writeBackup(snapshotRun.ID, fetchedAt, rawPayload)
		if err != nil {
			finalizeErr := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
				Status:         "failed",
				HTTPStatus:     httpStatus,
				ErrorMessage:   errorMessage(err),
				BackupFilePath: backupFilePath,
				ExportedAt:     exportedAt,
			})
			if finalizeErr != nil {
				return nil, fmt.Errorf("write backup: %v; finalize snapshot run: %w", err, finalizeErr)
			}
			return nil, fmt.Errorf("write backup: %w", err)
		}
	}

	events := FlattenUsageExport(snapshotRun.ID, fetchResult.Payload)
	inserted, deduped, err := repository.InsertUsageEvents(s.db, events)
	if err != nil {
		finalizeErr := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
			Status:         "failed",
			HTTPStatus:     httpStatus,
			ErrorMessage:   errorMessage(err),
			BackupFilePath: backupFilePath,
			ExportedAt:     exportedAt,
		})
		if finalizeErr != nil {
			return nil, fmt.Errorf("insert usage events: %v; finalize snapshot run: %w", err, finalizeErr)
		}
		return nil, fmt.Errorf("insert usage events: %w", err)
	}

	if err := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
		Status:         "completed",
		HTTPStatus:     httpStatus,
		BackupFilePath: backupFilePath,
		InsertedEvents: inserted,
		DedupedEvents:  deduped,
		ExportedAt:     exportedAt,
	}); err != nil {
		return nil, err
	}

	return &SyncResult{
		SnapshotRunID:  snapshotRun.ID,
		Status:         "completed",
		HTTPStatus:     httpStatus,
		InsertedEvents: inserted,
		DedupedEvents:  deduped,
		PayloadHash:    payloadHash,
		ExportedAt:     exportedAt,
		BackupFilePath: backupFilePath,
	}, nil
}

func (s *SyncService) writeBackup(snapshotRunID uint, fetchedAt time.Time, payload []byte) (string, error) {
	if !s.backupEnabled || len(payload) == 0 {
		return "", nil
	}
	if s.backupWriter == nil {
		return "", fmt.Errorf("backup writer is nil")
	}
	return s.backupWriter.Write(snapshotRunID, fetchedAt, payload)
}

func (s *SyncService) shouldWriteBackup(fetchedAt time.Time, lastBackupSnapshotRun *models.SnapshotRun) bool {
	if !s.backupEnabled {
		return false
	}
	if s.backupInterval <= 0 {
		return true
	}
	if lastBackupSnapshotRun == nil {
		return true
	}
	return fetchedAt.UTC().Sub(lastBackupSnapshotRun.FetchedAt.UTC()) >= s.backupInterval
}

func hashPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func initialSnapshotStatus(err error) string {
	if err != nil {
		return "failed"
	}
	return "pending"
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
