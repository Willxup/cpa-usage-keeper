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
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type ExportFetcher interface {
	FetchUsageExport(ctx context.Context) (*cpa.ExportResult, error)
	FetchAuthFiles(ctx context.Context) (*cpa.AuthFilesResult, error)
	FetchManagementConfig(ctx context.Context) (*cpa.ManagementConfigResult, error)
}

type BackupWriter interface {
	Write(snapshotRunID uint, fetchedAt time.Time, payload []byte) (string, error)
}

type BackupCleaner interface {
	Cleanup(retentionDays int, now time.Time) (int, error)
}

const syncPrefilterOverlapWindow = 24 * time.Hour

type SyncService struct {
	db                  *gorm.DB
	client              ExportFetcher
	baseURL             string
	backupEnabled       bool
	backupInterval      time.Duration
	backupRetentionDays int
	backupWriter        BackupWriter
	backupCleaner       BackupCleaner
	now                 func() time.Time
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
	var cleaner BackupCleaner
	if cfg.BackupEnabled {
		backupStore := backup.NewWriter(cfg.BackupDir)
		writer = backupStore
		cleaner = backupStore
	}
	return NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:             cfg.CPABaseURL,
		Client:              cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout),
		BackupEnabled:       cfg.BackupEnabled,
		BackupWriter:        writer,
		BackupInterval:      cfg.BackupInterval,
		BackupRetentionDays: cfg.BackupRetentionDays,
		BackupCleaner:       cleaner,
	})
}

type SyncServiceOptions struct {
	BaseURL             string
	Client              ExportFetcher
	BackupEnabled       bool
	BackupWriter        BackupWriter
	BackupInterval      time.Duration
	BackupRetentionDays int
	BackupCleaner       BackupCleaner
	Now                 func() time.Time
}

func NewSyncServiceWithOptions(db *gorm.DB, opts SyncServiceOptions) *SyncService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &SyncService{
		db:                  db,
		client:              opts.Client,
		baseURL:             strings.TrimSpace(opts.BaseURL),
		backupEnabled:       opts.BackupEnabled,
		backupInterval:      opts.BackupInterval,
		backupRetentionDays: opts.BackupRetentionDays,
		backupWriter:        opts.BackupWriter,
		backupCleaner:       opts.BackupCleaner,
		now:                 now,
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

func (s *SyncService) SyncStatus(ctx context.Context) (string, error) {
	result, err := s.syncOnce(ctx)
	if result == nil {
		return "", err
	}
	return result.Status, err
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
		httpStatus  int
		rawPayload  []byte
		payloadHash string
		exportedAt  *time.Time
		version     string
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

	authFilesResult, authFilesErr := s.client.FetchAuthFiles(ctx)
	managementConfigResult, managementConfigErr := s.client.FetchManagementConfig(ctx)

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

	var lastBackupSnapshotRun *models.SnapshotRun
	if s.backupEnabled {
		lastBackupSnapshotRun, err = repository.FindLastSnapshotRunWithBackup(s.db)
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
	events, err = filterUsageEventsByLocalWatermark(s.db, events, syncPrefilterOverlapWindow)
	if err != nil {
		finalizeErr := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
			Status:         "failed",
			HTTPStatus:     httpStatus,
			ErrorMessage:   errorMessage(err),
			BackupFilePath: backupFilePath,
			ExportedAt:     exportedAt,
		})
		if finalizeErr != nil {
			return nil, fmt.Errorf("filter usage events: %v; finalize snapshot run: %w", err, finalizeErr)
		}
		return nil, fmt.Errorf("filter usage events: %w", err)
	}
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

	authFilesSyncErr := syncAuthFiles(s.db, authFilesResult, authFilesErr)
	providerMetadataSyncErr := syncProviderMetadata(s.db, managementConfigResult, managementConfigErr)
	partialSyncErr := joinErrors(authFilesSyncErr, providerMetadataSyncErr)
	finalStatus := "completed"
	if partialSyncErr != nil {
		finalStatus = "completed_with_warnings"
	}
	finalErrorMessage := errorMessage(partialSyncErr)
	if err := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
		Status:         finalStatus,
		HTTPStatus:     httpStatus,
		BackupFilePath: backupFilePath,
		InsertedEvents: inserted,
		DedupedEvents:  deduped,
		ExportedAt:     exportedAt,
		ErrorMessage:   finalErrorMessage,
	}); err != nil {
		return nil, err
	}

	result := &SyncResult{
		SnapshotRunID:  snapshotRun.ID,
		Status:         finalStatus,
		HTTPStatus:     httpStatus,
		InsertedEvents: inserted,
		DedupedEvents:  deduped,
		PayloadHash:    payloadHash,
		ExportedAt:     exportedAt,
		BackupFilePath: backupFilePath,
	}
	if partialSyncErr != nil {
		return result, partialSyncErr
	}
	if err := s.cleanupBackups(fetchedAt); err != nil {
		return result, fmt.Errorf("cleanup backups: %w", err)
	}
	return result, nil
}

func (s *SyncService) cleanupBackups(now time.Time) error {
	if !s.backupEnabled || s.backupRetentionDays <= 0 {
		return nil
	}
	if s.backupCleaner == nil {
		return fmt.Errorf("backup cleaner is nil")
	}
	_, err := s.backupCleaner.Cleanup(s.backupRetentionDays, now)
	return err
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

func filterUsageEventsByLocalWatermark(db *gorm.DB, events []models.UsageEvent, overlapWindow time.Duration) ([]models.UsageEvent, error) {
	if len(events) == 0 {
		return events, nil
	}

	watermark, err := repository.FindLatestUsageEventTimestamp(db)
	if err != nil {
		return nil, err
	}
	if watermark == nil {
		return events, nil
	}

	cutoff := watermark.UTC().Add(-overlapWindow)
	filtered := make([]models.UsageEvent, 0, len(events))
	for _, event := range events {
		if event.Timestamp.IsZero() || !event.Timestamp.UTC().Before(cutoff) {
			filtered = append(filtered, event)
		}
	}
	skipped := len(events) - len(filtered)
	if skipped > 0 {
		logrus.WithFields(logrus.Fields{
			"watermark":       watermark.UTC().Format(time.RFC3339),
			"cutoff":          cutoff.Format(time.RFC3339),
			"overlap_hours":   overlapWindow.Hours(),
			"filtered_events": skipped,
			"total_events":    len(events),
		}).Info("filtered old usage events before insert")
	}
	return filtered, nil
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

func syncAuthFiles(db *gorm.DB, result *cpa.AuthFilesResult, fetchErr error) error {
	if fetchErr != nil {
		return fmt.Errorf("fetch auth files: %w", fetchErr)
	}
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if result == nil {
		return fmt.Errorf("fetch auth files: empty response")
	}

	inputs := make([]repository.AuthFileInput, 0, len(result.Payload.Files))
	for _, file := range result.Payload.Files {
		inputs = append(inputs, repository.AuthFileInput{
			AuthIndex:   file.AuthIndex,
			Name:        file.Name,
			Email:       file.Email,
			Type:        file.Type,
			Provider:    file.Provider,
			Label:       file.Label,
			Status:      file.Status,
			Source:      file.Source,
			Disabled:    file.Disabled,
			Unavailable: file.Unavailable,
			RuntimeOnly: file.RuntimeOnly,
		})
	}
	if err := repository.ReplaceAuthFiles(db, inputs); err != nil {
		return fmt.Errorf("sync auth files: %w", err)
	}
	return nil
}

func syncProviderMetadata(db *gorm.DB, result *cpa.ManagementConfigResult, fetchErr error) error {
	if fetchErr != nil {
		return fmt.Errorf("fetch provider metadata: %w", fetchErr)
	}
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if result == nil {
		return fmt.Errorf("fetch provider metadata: empty response")
	}

	inputs := flattenProviderMetadata(result.Payload)
	if err := repository.ReplaceProviderMetadata(db, inputs); err != nil {
		return fmt.Errorf("sync provider metadata: %w", err)
	}
	return nil
}

func flattenProviderMetadata(cfg cpa.ManagementConfig) []repository.ProviderMetadataInput {
	items := make([]repository.ProviderMetadataInput, 0)
	seen := make(map[string]struct{})
	appendItem := func(lookupKey, providerType, displayName, providerKey, matchKind string) {
		lookupKey = strings.TrimSpace(lookupKey)
		providerType = strings.TrimSpace(providerType)
		displayName = strings.TrimSpace(displayName)
		providerKey = strings.TrimSpace(providerKey)
		matchKind = strings.TrimSpace(matchKind)
		if lookupKey == "" || providerType == "" || displayName == "" || providerKey == "" || matchKind == "" {
			return
		}
		if _, ok := seen[lookupKey]; ok {
			return
		}
		seen[lookupKey] = struct{}{}
		items = append(items, repository.ProviderMetadataInput{
			LookupKey:    lookupKey,
			ProviderType: providerType,
			DisplayName:  displayName,
			ProviderKey:  providerKey,
			MatchKind:    matchKind,
		})
	}
	appendProviderEntries := func(providerType string, configs []cpa.ProviderKeyConfig) {
		for _, cfg := range configs {
			displayName := firstNonEmpty(cfg.Prefix, cfg.Name, providerType)
			providerKey := providerType + ":" + displayName
			appendItem(cfg.APIKey, providerType, displayName, providerKey, "api_key")
			appendItem(cfg.Prefix, providerType, displayName, providerKey, "prefix")
		}
	}

	appendProviderEntries("gemini", cfg.GeminiAPIKeys)
	appendProviderEntries("claude", cfg.ClaudeAPIKeys)
	appendProviderEntries("codex", cfg.CodexAPIKeys)
	appendProviderEntries("vertex", cfg.VertexAPIKeys)

	for _, provider := range cfg.OpenAICompatibility {
		displayName := firstNonEmpty(provider.Name, provider.Prefix, "openai")
		providerKey := "openai:" + displayName
		appendItem(provider.Prefix, "openai", displayName, providerKey, "prefix")
		for _, entry := range provider.APIKeyEntries {
			appendItem(entry.APIKey, "openai", displayName, providerKey, "api_key")
		}
	}

	return items
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func joinErrors(errs ...error) error {
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		messages = append(messages, strings.TrimSpace(err.Error()))
	}
	if len(messages) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(messages, "; "))
}
