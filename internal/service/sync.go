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
}

type UsageFetcher interface {
	FetchUsage(ctx context.Context, fetchedAt time.Time) (*UsageFetchResult, error)
}

type MetadataFetcher interface {
	FetchAuthFiles(ctx context.Context) (*cpa.AuthFilesResult, error)
	FetchGeminiAPIKeys(ctx context.Context) (*cpa.ProviderKeyConfigResult, error)
	FetchClaudeAPIKeys(ctx context.Context) (*cpa.ProviderKeyConfigResult, error)
	FetchCodexAPIKeys(ctx context.Context) (*cpa.ProviderKeyConfigResult, error)
	FetchVertexAPIKeys(ctx context.Context) (*cpa.ProviderKeyConfigResult, error)
	FetchOpenAICompatibility(ctx context.Context) (*cpa.OpenAICompatibilityResult, error)
}

type CPAClientFetcher interface {
	ExportFetcher
	MetadataFetcher
}

type BackupWriter interface {
	Write(snapshotRunID uint, fetchedAt time.Time, payload []byte) (string, error)
}

type BackupCleaner interface {
	Cleanup(retentionDays int, now time.Time) (int, error)
}

const syncPrefilterOverlapWindow = 24 * time.Hour
const redisInboxProcessLimit = 1000

const (
	syncMetadataOptional = false
	syncMetadataRequired = true
)

type SyncService struct {
	db                  *gorm.DB
	client              CPAClientFetcher
	usageFetcher        UsageFetcher
	redisUsageFetcher   UsageFetcher
	redisQueue          RedisQueue
	redisQueueKey       string
	usageSyncMode       string
	legacyUsageFetcher  UsageFetcher
	metadataFetcher     MetadataFetcher
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

type RedisBatchSyncResult struct {
	Empty          bool
	Status         string
	SnapshotRunID  uint
	InsertedEvents int
	DedupedEvents  int
}

type RedisInboxPullResult struct {
	Empty        bool
	Status       string
	InsertedRows int
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
		UsageSyncMode:       cfg.UsageSyncMode,
		RedisQueue:          cpa.NewRedisQueueClient(cfg.CPABaseURL, cfg.RedisQueueAddr, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.RedisQueueKey, cfg.RedisQueueBatchSize),
		RedisQueueKey:       cfg.RedisQueueKey,
		BackupEnabled:       cfg.BackupEnabled,
		BackupWriter:        writer,
		BackupInterval:      cfg.BackupInterval,
		BackupRetentionDays: cfg.BackupRetentionDays,
		BackupCleaner:       cleaner,
	})
}

type SyncServiceOptions struct {
	BaseURL             string
	Client              CPAClientFetcher
	UsageFetcher        UsageFetcher
	MetadataFetcher     MetadataFetcher
	UsageSyncMode       string
	RedisQueue          RedisQueue
	RedisQueueKey       string
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
	usageFetcher := opts.UsageFetcher
	metadataFetcher := opts.MetadataFetcher
	if metadataFetcher == nil {
		metadataFetcher = opts.Client
	}
	legacyFetcher := legacyUsageFetcher{client: opts.Client}
	var redisFetcher UsageFetcher
	if opts.RedisQueue != nil {
		redisFetcher = newRedisUsageFetcher(opts.RedisQueue)
	}
	if usageFetcher == nil && opts.Client != nil {
		usageFetcher = legacyFetcher
	}
	if opts.UsageSyncMode == "redis" {
		if redisFetcher == nil {
			redisFetcher = newRedisUsageFetcher(opts.RedisQueue)
		}
		usageFetcher = redisFetcher
	}
	return &SyncService{
		db:                  db,
		client:              opts.Client,
		usageFetcher:        usageFetcher,
		redisUsageFetcher:   redisFetcher,
		redisQueue:          opts.RedisQueue,
		redisQueueKey:       redisQueueKey(opts.RedisQueueKey),
		usageSyncMode:       strings.TrimSpace(opts.UsageSyncMode),
		legacyUsageFetcher:  legacyFetcher,
		metadataFetcher:     metadataFetcher,
		baseURL:             strings.TrimSpace(opts.BaseURL),
		backupEnabled:       opts.BackupEnabled,
		backupInterval:      opts.BackupInterval,
		backupRetentionDays: opts.BackupRetentionDays,
		backupWriter:        opts.BackupWriter,
		backupCleaner:       opts.BackupCleaner,
		now:                 now,
	}
}

func NewSyncServiceWithClient(db *gorm.DB, baseURL string, client CPAClientFetcher) *SyncService {
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
	if s != nil && s.redisQueue != nil && s.usageSyncMode == "redis" {
		if _, err := s.PullRedisUsageInbox(ctx); err != nil {
			return nil, err
		}
		result, err := s.ProcessRedisUsageInbox(ctx, true)
		return syncResultFromRedisBatch(result), err
	}
	return s.syncOnce(ctx)
}

func syncResultFromRedisBatch(result *RedisBatchSyncResult) *SyncResult {
	if result == nil {
		return nil
	}
	return &SyncResult{
		SnapshotRunID:  result.SnapshotRunID,
		Status:         result.Status,
		InsertedEvents: result.InsertedEvents,
		DedupedEvents:  result.DedupedEvents,
	}
}

func (s *SyncService) SyncStatus(ctx context.Context) (string, error) {
	result, err := s.syncOnce(ctx)
	if result == nil {
		return "", err
	}
	return result.Status, err
}

func (s *SyncService) SyncMetadata(ctx context.Context) error {
	if err := s.validate(syncMetadataRequired); err != nil {
		return err
	}
	logrus.Debug("metadata sync started")
	authFilesResult, authFilesErr := s.metadataFetcher.FetchAuthFiles(ctx)
	providerConfig, fetchedProviderTypes, providerMetadataErr := fetchProviderMetadata(ctx, s.metadataFetcher)
	err := joinErrors(
		syncAuthFiles(s.db, authFilesResult, authFilesErr),
		syncProviderMetadata(s.db, providerConfig, fetchedProviderTypes, providerMetadataErr),
	)
	fields := logrus.Fields{
		"status": "completed",
	}
	if err != nil {
		fields["status"] = "completed_with_warnings"
		fields["error"] = err.Error()
	}
	logrus.WithFields(fields).Debug("metadata sync finished")
	return err
}

// PullRedisUsageInbox 是 Redis 同步的拉取阶段：只 LPOP 队列消息并原样写入 redis_usage_inboxes。
// 这个阶段不解码消息、不写 usage_events、不创建 snapshot_runs，保证 Redis 消费和本地处理职责分离。
func (s *SyncService) PullRedisUsageInbox(ctx context.Context) (*RedisInboxPullResult, error) {
	if err := s.validate(syncMetadataOptional); err != nil {
		return nil, err
	}
	if s.redisQueue == nil {
		return nil, fmt.Errorf("sync service redis queue is nil")
	}

	fetchedAt := s.now().UTC()
	messages, err := s.redisQueue.PopUsage(ctx)
	if err != nil {
		return &RedisInboxPullResult{Status: "failed"}, fmt.Errorf("fetch redis usage: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"queue_key":     s.redisQueueKey,
		"message_count": len(messages),
	}).Debug("redis usage batch popped")
	if len(messages) == 0 {
		return &RedisInboxPullResult{Empty: true, Status: "empty"}, nil
	}

	inboxRows, err := insertRedisInboxMessages(s.db, s.redisQueueKey, messages, fetchedAt)
	if err != nil {
		return &RedisInboxPullResult{Status: "failed"}, fmt.Errorf("insert redis usage inbox: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"queue_key": s.redisQueueKey,
		"row_count": len(inboxRows),
	}).Debug("redis usage inbox rows inserted")
	return &RedisInboxPullResult{Status: "completed", InsertedRows: len(inboxRows)}, nil
}

// ProcessRedisUsageInbox 是 Redis 同步的本地处理阶段：只读取 pending/process_failed inbox 行并写入 usage_events。
// Redis 路径不再写 snapshot_runs；成功处理后仅用 usage_event_key 记录 inbox 与最终事件的关联。
func (s *SyncService) ProcessRedisUsageInbox(ctx context.Context, syncMetadata bool) (*RedisBatchSyncResult, error) {
	if err := s.validate(syncMetadata); err != nil {
		return nil, err
	}
	fetchedAt := s.now().UTC()
	processableRows, err := repository.ListProcessableRedisUsageInbox(s.db, redisInboxProcessLimit)
	if err != nil {
		return &RedisBatchSyncResult{Status: "failed"}, fmt.Errorf("list processable redis usage inbox: %w", err)
	}
	if len(processableRows) == 0 {
		return &RedisBatchSyncResult{Empty: true, Status: "empty"}, nil
	}
	logrus.WithField("row_count", len(processableRows)).Debug("redis usage inbox rows found for processing")
	return s.processRedisInboxRows(ctx, processableRows, fetchedAt, syncMetadata)
}

// CleanupRedisUsageInbox 只清理 Redis inbox 表，供测试和单独维护入口使用；每日任务使用 CleanupStorage 统一执行。
func (s *SyncService) CleanupRedisUsageInbox(ctx context.Context) error {
	if err := s.validate(syncMetadataOptional); err != nil {
		return err
	}
	_, err := repository.CleanupRedisUsageInbox(s.db, s.now())
	return err
}

// CleanupStorage 是每日 03:00 维护任务调用的统一入口：先清 Redis inbox，再清 snapshot_runs，最后 VACUUM 收缩 SQLite。
func (s *SyncService) CleanupStorage(ctx context.Context) error {
	if err := s.validate(syncMetadataOptional); err != nil {
		return err
	}
	_, err := repository.CleanupStorage(s.db, s.now())
	return err
}

// SyncRedisBatch 保留为兼容入口：先处理本地存量 inbox，空了再拉一次 Redis 并立即处理。
// 后台任务不要调用它，后台必须使用拆分后的 PullRedisUsageInbox、ProcessRedisUsageInbox 和 CleanupStorage。
func (s *SyncService) SyncRedisBatch(ctx context.Context, syncMetadata bool) (*RedisBatchSyncResult, error) {
	if result, err := s.ProcessRedisUsageInbox(ctx, syncMetadata); err != nil || result == nil || !result.Empty {
		return result, err
	}
	if _, err := s.PullRedisUsageInbox(ctx); err != nil {
		return &RedisBatchSyncResult{Status: "failed"}, err
	}
	return s.ProcessRedisUsageInbox(ctx, syncMetadata)
}

// processRedisInboxRows 只从已落库的原始消息解码和写入事件，坏消息会标记为 decode_failed，不阻塞同批其它数据。
// 可解码但入库失败的消息标记为 process_failed，后续 ProcessRedisUsageInbox 会按 id 顺序重试。
func (s *SyncService) processRedisInboxRows(ctx context.Context, inboxRows []models.RedisUsageInbox, fetchedAt time.Time, syncMetadata bool) (*RedisBatchSyncResult, error) {
	logrus.WithFields(logrus.Fields{
		"row_count":     len(inboxRows),
		"sync_metadata": syncMetadata,
	}).Debug("redis usage inbox processing started")
	validRows := make([]models.RedisUsageInbox, 0, len(inboxRows))
	events := make([]models.UsageEvent, 0, len(inboxRows))
	decodeErrs := make([]error, 0)
	for _, row := range inboxRows {
		event, _, decodeErr := DecodeRedisUsageMessage(row.RawMessage, fetchedAt)
		if decodeErr != nil {
			if markErr := repository.MarkRedisUsageInboxDecodeFailed(s.db, row.ID, decodeErr); markErr != nil {
				return &RedisBatchSyncResult{Status: "failed"}, fmt.Errorf("mark redis usage inbox decode failed: %w", markErr)
			}
			decodeErrs = append(decodeErrs, decodeErr)
			continue
		}
		validRows = append(validRows, row)
		events = append(events, event)
	}
	decodeErr := joinErrors(decodeErrs...)
	logrus.WithFields(logrus.Fields{
		"row_count":           len(inboxRows),
		"valid_event_count":   len(events),
		"decode_failed_count": len(decodeErrs),
	}).Debug("redis usage inbox rows decoded")
	if len(events) == 0 {
		if decodeErr != nil {
			return &RedisBatchSyncResult{Status: "completed_with_warnings"}, decodeErr
		}
		return &RedisBatchSyncResult{Empty: true, Status: "empty"}, nil
	}

	fetchResult := &UsageFetchResult{Events: events}
	logrus.WithField("event_count", len(events)).Debug("redis usage events persistence started")
	result, err := s.persistRedisUsageEvents(ctx, fetchResult, syncMetadata)
	if result == nil {
		markRedisInboxRowsProcessFailed(s.db, validRows, err)
		return nil, err
	}
	if err != nil && result.Status == "failed" {
		markRedisInboxRowsProcessFailed(s.db, validRows, err)
		return &RedisBatchSyncResult{Status: result.Status}, err
	}
	for i, row := range validRows {
		if markErr := repository.MarkRedisUsageInboxProcessedWithoutSnapshot(s.db, row.ID, fetchResult.Events[i].EventKey, fetchedAt); markErr != nil {
			return &RedisBatchSyncResult{Status: "failed"}, fmt.Errorf("mark redis usage inbox processed: %w", markErr)
		}
	}
	logrus.WithFields(logrus.Fields{
		"processed_rows":  len(validRows),
		"inserted_events": result.InsertedEvents,
		"deduped_events":  result.DedupedEvents,
		"status":          result.Status,
	}).Debug("redis usage inbox rows processed")

	status := result.Status
	returnErr := err
	if decodeErr != nil {
		status = "completed_with_warnings"
		if returnErr != nil {
			returnErr = joinErrors(returnErr, decodeErr)
		} else {
			returnErr = decodeErr
		}
	}
	return &RedisBatchSyncResult{
		Status:         status,
		SnapshotRunID:  result.SnapshotRunID,
		InsertedEvents: result.InsertedEvents,
		DedupedEvents:  result.DedupedEvents,
	}, returnErr
}

// SyncLegacyStatus 执行 legacy_export 回退路径并返回 snapshot_run 最终状态。
// legacy_export 仍然会创建 snapshot_runs、保存原始导出 payload，并把 usage_events 关联到本次 snapshot_run。
func (s *SyncService) SyncLegacyStatus(ctx context.Context) (string, error) {
	if err := s.validate(syncMetadataRequired); err != nil {
		return "", err
	}
	if s.legacyUsageFetcher == nil {
		return "", fmt.Errorf("sync service legacy usage fetcher is nil")
	}

	fetchedAt := s.now().UTC()
	fetchResult, fetchErr := s.legacyUsageFetcher.FetchUsage(ctx, fetchedAt)
	result, err := s.persistUsageResult(ctx, fetchedAt, fetchResult, fetchErr, true, true)
	if result == nil {
		return "", err
	}
	return result.Status, err
}

// syncOnce 执行一次完整的 legacy_export 同步：拉取导出、创建 snapshot_run、写 usage_events、同步 metadata 和备份。
// 该路径用于 legacy_export 模式以及 auto 探测 Redis 不可用后的回退模式，不参与 Redis inbox 分阶段处理。
func (s *SyncService) syncOnce(ctx context.Context) (*SyncResult, error) {
	if err := s.validate(syncMetadataRequired); err != nil {
		return nil, err
	}
	if s.usageFetcher == nil && s.client != nil {
		s.usageFetcher = legacyUsageFetcher{client: s.client}
	}
	if s.usageFetcher == nil {
		return nil, fmt.Errorf("sync service usage fetcher is nil")
	}

	fetchedAt := s.now().UTC()
	fetchResult, fetchErr := s.usageFetcher.FetchUsage(ctx, fetchedAt)
	return s.persistUsageResult(ctx, fetchedAt, fetchResult, fetchErr, true, true)
}

// persistRedisUsageEvents 是 Redis inbox 专用入库路径，只写 usage_events 和可选 metadata，不创建 snapshot_runs。
func (s *SyncService) persistRedisUsageEvents(ctx context.Context, fetchResult *UsageFetchResult, syncMetadata bool) (*SyncResult, error) {
	if fetchResult == nil {
		return nil, fmt.Errorf("redis usage fetch result is nil")
	}

	var authFilesResult *cpa.AuthFilesResult
	var providerConfig cpa.ProviderMetadataConfig
	var fetchedProviderTypes []string
	var authFilesErr error
	var providerMetadataErr error
	if syncMetadata {
		if s.metadataFetcher == nil && s.client != nil {
			s.metadataFetcher = s.client
		}
		if s.metadataFetcher == nil {
			return nil, fmt.Errorf("sync service metadata fetcher is nil")
		}
		authFilesResult, authFilesErr = s.metadataFetcher.FetchAuthFiles(ctx)
		providerConfig, fetchedProviderTypes, providerMetadataErr = fetchProviderMetadata(ctx, s.metadataFetcher)
	}

	events := fetchResult.Events
	for i := range events {
		events[i].SnapshotRunID = 0
	}
	var err error
	events, err = alignUsageEventKeysWithExistingCanonicalEvents(s.db, events)
	fetchResult.Events = events
	if err != nil {
		return &SyncResult{Status: "failed"}, fmt.Errorf("align usage events: %w", err)
	}
	logrus.WithField("event_count", len(events)).Debug("usage events insert started")
	inserted, deduped, err := repository.InsertUsageEvents(s.db, events)
	if err != nil {
		return &SyncResult{Status: "failed"}, fmt.Errorf("insert usage events: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"inserted_events": inserted,
		"deduped_events":  deduped,
	}).Debug("usage events insert finished")

	var partialSyncErr error
	if syncMetadata {
		authFilesSyncErr := syncAuthFiles(s.db, authFilesResult, authFilesErr)
		providerMetadataSyncErr := syncProviderMetadata(s.db, providerConfig, fetchedProviderTypes, providerMetadataErr)
		partialSyncErr = joinErrors(authFilesSyncErr, providerMetadataSyncErr)
	}
	status := "completed"
	if partialSyncErr != nil {
		status = "completed_with_warnings"
	}
	result := &SyncResult{Status: status, InsertedEvents: inserted, DedupedEvents: deduped}
	if partialSyncErr != nil {
		return result, partialSyncErr
	}
	return result, nil
}

// persistUsageResult 是 legacy_export 专用入库路径，负责 snapshot_runs 的完整生命周期和 usage_events 写入。
// 即使拉取失败也会创建并 finalize snapshot_run，用于保留失败状态、HTTP 状态、错误信息和原始 payload 审计线索。
func (s *SyncService) persistUsageResult(ctx context.Context, fetchedAt time.Time, fetchResult *UsageFetchResult, fetchErr error, syncMetadata bool, filterByWatermark bool) (*SyncResult, error) {
	logrus.WithFields(logrus.Fields{
		"sync_metadata":       syncMetadata,
		"filter_by_watermark": filterByWatermark,
	}).Debug("usage persistence started")

	var (
		httpStatus  int
		rawPayload  []byte
		payloadHash string
		exportedAt  *time.Time
		version     string
	)
	if fetchResult != nil {
		httpStatus = fetchResult.HTTPStatus
		rawPayload = append([]byte(nil), fetchResult.RawPayload...)
		payloadHash = hashPayload(rawPayload)
		exportedAt = fetchResult.ExportedAt
		version = fetchResult.Version
	}

	var authFilesResult *cpa.AuthFilesResult
	var providerConfig cpa.ProviderMetadataConfig
	var fetchedProviderTypes []string
	var authFilesErr error
	var providerMetadataErr error
	if syncMetadata {
		if s.metadataFetcher == nil && s.client != nil {
			s.metadataFetcher = s.client
		}
		if s.metadataFetcher == nil {
			return nil, fmt.Errorf("sync service metadata fetcher is nil")
		}
		authFilesResult, authFilesErr = s.metadataFetcher.FetchAuthFiles(ctx)
		providerConfig, fetchedProviderTypes, providerMetadataErr = fetchProviderMetadata(ctx, s.metadataFetcher)
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
	logrus.WithFields(logrus.Fields{
		"snapshot_run_id": snapshotRun.ID,
		"status":          snapshotRun.Status,
		"payload_bytes":   len(rawPayload),
	}).Debug("snapshot run created")

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
		logrus.WithField("snapshot_run_id", snapshotRun.ID).Debug("usage backup write started")
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
		logrus.WithFields(logrus.Fields{
			"snapshot_run_id": snapshotRun.ID,
			"backup_written":  backupFilePath != "",
		}).Debug("usage backup write finished")
	}

	events := fetchResult.Events
	for i := range events {
		events[i].SnapshotRunID = snapshotRun.ID
	}
	if filterByWatermark {
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
	}
	events, err = alignUsageEventKeysWithExistingCanonicalEvents(s.db, events)
	fetchResult.Events = events
	if err != nil {
		finalizeErr := repository.FinalizeSnapshotRun(s.db, snapshotRun.ID, repository.SnapshotRunResult{
			Status:         "failed",
			HTTPStatus:     httpStatus,
			ErrorMessage:   errorMessage(err),
			BackupFilePath: backupFilePath,
			ExportedAt:     exportedAt,
		})
		if finalizeErr != nil {
			return nil, fmt.Errorf("align usage events: %v; finalize snapshot run: %w", err, finalizeErr)
		}
		return nil, fmt.Errorf("align usage events: %w", err)
	}
	logrus.WithField("event_count", len(events)).Debug("usage events insert started")
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

	logrus.WithFields(logrus.Fields{
		"snapshot_run_id": snapshotRun.ID,
		"inserted_events": inserted,
		"deduped_events":  deduped,
	}).Debug("usage events insert finished")

	var partialSyncErr error
	if syncMetadata {
		authFilesSyncErr := syncAuthFiles(s.db, authFilesResult, authFilesErr)
		providerMetadataSyncErr := syncProviderMetadata(s.db, providerConfig, fetchedProviderTypes, providerMetadataErr)
		partialSyncErr = joinErrors(authFilesSyncErr, providerMetadataSyncErr)
	}
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
	logrus.WithFields(logrus.Fields{
		"snapshot_run_id": snapshotRun.ID,
		"status":          finalStatus,
		"inserted_events": inserted,
		"deduped_events":  deduped,
		"backup_written":  backupFilePath != "",
	}).Debug("snapshot run finalized")

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

func alignUsageEventKeysWithExistingCanonicalEvents(db *gorm.DB, events []models.UsageEvent) ([]models.UsageEvent, error) {
	if len(events) == 0 {
		return events, nil
	}
	canonicalEventKeys := make(map[string]string, len(events))
	consumedCanonicalKeys := make(map[string]struct{}, len(events))
	for i := range events {
		events[i].Timestamp = events[i].Timestamp.UTC()
		canonicalKey := canonicalUsageEventKey(events[i])
		incomingKey := strings.TrimSpace(events[i].EventKey)
		if existingKey := canonicalEventKeys[canonicalKey]; existingKey != "" {
			if incomingKey == canonicalKey {
				events[i].EventKey = existingKey
			} else if existingKey == canonicalKey {
				if _, consumed := consumedCanonicalKeys[canonicalKey]; !consumed {
					events[i].EventKey = existingKey
					consumedCanonicalKeys[canonicalKey] = struct{}{}
				}
			}
			continue
		}

		var existing models.UsageEvent
		result := db.Select("event_key").Where(
			"TRIM(api_group_key) = ? AND TRIM(model) = ? AND timestamp = ? AND TRIM(source) = ? AND TRIM(auth_index) = ? AND failed = ? AND input_tokens = ? AND output_tokens = ? AND reasoning_tokens = ? AND cached_tokens = ? AND total_tokens = ?",
			strings.TrimSpace(events[i].APIGroupKey),
			strings.TrimSpace(events[i].Model),
			events[i].Timestamp,
			strings.TrimSpace(events[i].Source),
			strings.TrimSpace(events[i].AuthIndex),
			events[i].Failed,
			events[i].InputTokens,
			events[i].OutputTokens,
			events[i].ReasoningTokens,
			events[i].CachedTokens,
			events[i].TotalTokens,
		).Order("id ASC").Limit(1).Find(&existing)
		if result.Error != nil {
			return nil, fmt.Errorf("find equivalent usage event: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			canonicalEventKeys[canonicalKey] = incomingKey
			continue
		}
		existingKey := strings.TrimSpace(existing.EventKey)
		if existingKey != "" {
			if incomingKey == canonicalKey {
				events[i].EventKey = existingKey
			} else if existingKey == canonicalKey {
				alreadyConsumed, err := redisInboxAlreadyReferencesEventKey(db, canonicalKey)
				if err != nil {
					return nil, err
				}
				if !alreadyConsumed {
					events[i].EventKey = existingKey
					consumedCanonicalKeys[canonicalKey] = struct{}{}
				}
			}
			canonicalEventKeys[canonicalKey] = existingKey
		} else {
			canonicalEventKeys[canonicalKey] = incomingKey
		}
	}
	return events, nil
}

func redisInboxAlreadyReferencesEventKey(db *gorm.DB, eventKey string) (bool, error) {
	var count int64
	if err := db.Model(&models.RedisUsageInbox{}).Where("status = ? AND usage_event_key = ?", repository.RedisUsageInboxStatusProcessed, eventKey).Count(&count).Error; err != nil {
		return false, fmt.Errorf("count redis inbox references: %w", err)
	}
	return count > 0, nil
}

func canonicalUsageEventKey(event models.UsageEvent) string {
	return BuildEventKey(
		event.APIGroupKey,
		event.Model,
		event.Timestamp,
		event.Source,
		event.AuthIndex,
		event.Failed,
		cpa.TokenStats{
			InputTokens:     event.InputTokens,
			OutputTokens:    event.OutputTokens,
			ReasoningTokens: event.ReasoningTokens,
			CachedTokens:    event.CachedTokens,
			TotalTokens:     event.TotalTokens,
		},
	)
}

type legacyUsageFetcher struct {
	client interface {
		FetchUsageExport(ctx context.Context) (*cpa.ExportResult, error)
	}
}

// FetchUsage 从 legacy export 接口拉取完整导出结果，保留 raw payload 给 persistUsageResult 写入 snapshot_runs。
// 这是 Redis 队列不可用时的回退数据源，事件 key 仍在后续入库阶段与既有 canonical event 对齐。
func (f legacyUsageFetcher) FetchUsage(ctx context.Context, _ time.Time) (*UsageFetchResult, error) {
	if f.client == nil {
		return nil, fmt.Errorf("legacy usage client is nil")
	}
	logrus.Debug("legacy usage pull started")
	result, err := f.client.FetchUsageExport(ctx)
	if result == nil {
		logrus.WithError(err).Debug("legacy usage pull finished")
		return nil, err
	}
	var exportedAt *time.Time
	if !result.Payload.ExportedAt.IsZero() {
		normalized := result.Payload.ExportedAt.UTC()
		exportedAt = &normalized
	}
	version := ""
	if result.Payload.Version > 0 {
		version = fmt.Sprintf("%d", result.Payload.Version)
	}
	events := FlattenUsageExport(0, result.Payload)
	logFields := logrus.Fields{
		"http_status":   result.StatusCode,
		"event_count":   len(events),
		"payload_bytes": len(result.Body),
	}
	if err != nil {
		logFields["error"] = err.Error()
	}
	logrus.WithFields(logFields).Debug("legacy usage pull finished")
	return &UsageFetchResult{
		HTTPStatus: result.StatusCode,
		RawPayload: append([]byte(nil), result.Body...),
		ExportedAt: exportedAt,
		Version:    version,
		Events:     events,
	}, err
}

func (s *SyncService) validate(syncMetadata bool) error {
	if s == nil {
		return fmt.Errorf("sync service is nil")
	}
	if s.db == nil {
		return fmt.Errorf("sync service database is nil")
	}
	if syncMetadata {
		if s.metadataFetcher == nil && s.client != nil {
			s.metadataFetcher = s.client
		}
		if s.metadataFetcher == nil {
			return fmt.Errorf("sync service metadata fetcher is nil")
		}
	}
	return nil
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

// insertRedisInboxMessages 在解码前先把 Redis 原始消息落库，降低 LPOP 后本地处理失败导致的数据丢失风险。
func insertRedisInboxMessages(db *gorm.DB, queueKey string, messages []string, poppedAt time.Time) ([]models.RedisUsageInbox, error) {
	inputs := make([]repository.RedisInboxInsert, 0, len(messages))
	for _, message := range messages {
		inputs = append(inputs, repository.RedisInboxInsert{
			QueueKey:   queueKey,
			RawMessage: message,
			PoppedAt:   poppedAt,
		})
	}
	return repository.InsertRedisUsageInboxMessages(db, inputs)
}

func markRedisInboxRowsProcessFailed(db *gorm.DB, rows []models.RedisUsageInbox, err error) {
	if err == nil {
		return
	}
	for _, row := range rows {
		if markErr := repository.MarkRedisUsageInboxProcessFailed(db, row.ID, err); markErr != nil {
			logrus.WithError(markErr).WithField("inbox_id", row.ID).Warn("failed to mark redis usage inbox process failure")
			continue
		}
		var stored models.RedisUsageInbox
		if loadErr := db.First(&stored, row.ID).Error; loadErr != nil {
			logrus.WithError(loadErr).WithField("inbox_id", row.ID).Warn("failed to load redis usage inbox after process failure")
			continue
		}
		if stored.Status == repository.RedisUsageInboxStatusDiscarded {
			logrus.WithFields(logrus.Fields{
				"inbox_id":        stored.ID,
				"queue_key":       stored.QueueKey,
				"message_hash":    stored.MessageHash,
				"attempt_count":   stored.AttemptCount,
				"last_error":      stored.LastError,
				"popped_at":       stored.PoppedAt,
				"snapshot_run_id": stored.SnapshotRunID,
			}).Warn("discarded redis usage inbox row after repeated process failures")
		}
	}
}

func redisQueueKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return cpa.ManagementUsageQueueKey
	}
	return trimmed
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

func fetchProviderMetadata(ctx context.Context, fetcher MetadataFetcher) (cpa.ProviderMetadataConfig, []string, error) {
	var cfg cpa.ProviderMetadataConfig
	var fetchedProviderTypes []string
	var errs []error

	if result, err := fetcher.FetchGeminiAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch gemini api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("gemini api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "gemini")
		cfg.GeminiAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchClaudeAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch claude api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("claude api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "claude")
		cfg.ClaudeAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchCodexAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch codex api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("codex api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "codex")
		cfg.CodexAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchVertexAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch vertex api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("vertex api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "vertex")
		cfg.VertexAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchOpenAICompatibility(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch openai compatibility: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("openai compatibility response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "openai")
		cfg.OpenAICompatibility = result.Payload
	}

	return cfg, fetchedProviderTypes, joinErrors(errs...)
}

func syncProviderMetadata(db *gorm.DB, cfg cpa.ProviderMetadataConfig, fetchedProviderTypes []string, fetchErr error) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	inputs := flattenProviderMetadata(cfg)
	if err := repository.ReplaceProviderMetadataForProviderTypes(db, inputs, fetchedProviderTypes); err != nil {
		return fmt.Errorf("sync provider metadata: %w", err)
	}
	if fetchErr != nil {
		return fmt.Errorf("fetch provider metadata: %w", fetchErr)
	}
	return nil
}

func flattenProviderMetadata(cfg cpa.ProviderMetadataConfig) []repository.ProviderMetadataInput {
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
