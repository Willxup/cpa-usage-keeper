package cooldown

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// AuthFileRestoreInfo 是 restore worker 使用的简化的 auth file 信息。
type AuthFileRestoreInfo struct {
	Name     string
	Disabled bool
}

const (
	defaultScanInterval   = 3 * time.Minute
	defaultBatchSize      = 10
	defaultWorkerLimit    = 1
	defaultMaxAttempts    = 0 // 0 = 无限重试
)

// RestoreWorkerConfig 是恢复 worker 的配置。
type RestoreWorkerConfig struct {
	// Enabled 控制是否启用恢复 worker。
	Enabled bool
	// ScanInterval 是两次扫描之间的间隔。
	ScanInterval time.Duration
	// BatchSize 是单次扫描最多处理的数量。
	BatchSize int
	// WorkerLimit 是并发恢复的限制（默认 1）。
	WorkerLimit int
	// MaxAttempts 是最大恢复尝试次数，0 表示无限重试。
	MaxAttempts int
	// DryRun 控制是否只记录不真正调用 CPA API。
	DryRun bool
	// Now 用于测试替换时间。
	Now func() time.Time
}

// DefaultRestoreWorkerConfig 返回默认配置。
func DefaultRestoreWorkerConfig() RestoreWorkerConfig {
	return RestoreWorkerConfig{
		Enabled:      true,
		ScanInterval: defaultScanInterval,
		BatchSize:    defaultBatchSize,
		WorkerLimit:  defaultWorkerLimit,
		MaxAttempts:  defaultMaxAttempts,
		DryRun:       false,
		Now:          time.Now,
	}
}

// RestoreWorker 是后台 cooldown 恢复 worker。
// 定期扫描 cooldown 表，自动恢复到期的 auth file。 
type RestoreWorker struct {
	db      *gorm.DB
	client  CooldownClient
	config  RestoreWorkerConfig
}

// NewRestoreWorker 创建恢复 worker。
func NewRestoreWorker(db *gorm.DB, client CooldownClient, config RestoreWorkerConfig) *RestoreWorker {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	scanInterval := config.ScanInterval
	if scanInterval <= 0 {
		scanInterval = defaultScanInterval
	}
	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	workerLimit := config.WorkerLimit
	if workerLimit <= 0 {
		workerLimit = defaultWorkerLimit
	}
	return &RestoreWorker{
		db:     db,
		client: client,
		config: RestoreWorkerConfig{
			Enabled:      config.Enabled,
			ScanInterval: scanInterval,
			BatchSize:    batchSize,
			WorkerLimit:  workerLimit,
			MaxAttempts:  config.MaxAttempts,
			DryRun:       config.DryRun,
			Now:          now,
		},
	}
}

// Run 启动恢复 worker 循环，支持 context cancellation。
func (w *RestoreWorker) Run(ctx context.Context) error {
	if w.db == nil {
		return fmt.Errorf("restore worker database is nil")
	}
	if !w.config.Enabled {
		logrus.Info("cooldown restore worker is disabled")
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"scan_interval": w.config.ScanInterval,
		"batch_size":    w.config.BatchSize,
		"dry_run":       w.config.DryRun,
	}).Info("cooldown restore worker started")

	// 启动时先执行一次恢复
	if err := w.restoreDue(ctx); err != nil {
		logrus.WithError(err).Error("cooldown restore worker initial restore failed")
	}

	ticker := time.NewTicker(w.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logrus.Info("cooldown restore worker stopped")
			return nil
		case <-ticker.C:
			if err := w.restoreDue(ctx); err != nil {
				logrus.WithError(err).Error("cooldown restore worker restore failed")
			}
		}
	}
}

// restoreDue 扫描并恢复已到期的 cooldown 记录。
func (w *RestoreWorker) restoreDue(ctx context.Context) error {
	now := timeutil.NormalizeStorageTime(w.config.Now())
	dueRecords, err := repository.ListDueCooldowns(w.db, now, w.config.BatchSize)
	if err != nil {
		return fmt.Errorf("list due cooldowns: %w", err)
	}

	if len(dueRecords) == 0 {
		return nil
	}

	logrus.WithField("count", len(dueRecords)).Info("cooldown restore: found due records")

	// 先拉取一次所有 auth files
	authFilesResult, err := w.client.FetchAuthFiles(ctx)
	if err != nil {
		return fmt.Errorf("fetch auth files for restore: %w", err)
	}

	// 构建 auth_index -> auth file 的映射
	type authFileInfo = AuthFileRestoreInfo
	authFilesByIndex := make(map[string]authFileInfo)
	for _, file := range authFilesResult.Payload.Files {
		idx := file.AuthIndex
		disabled := false
		if file.Disabled != nil {
			disabled = *file.Disabled
		}
		authFilesByIndex[idx] = authFileInfo{
			Name:     file.Name,
			Disabled: disabled,
		}
	}

	// 逐条恢复（worker_limit = 1 所以串行即可）
	for _, record := range dueRecords {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := w.restoreOne(ctx, record, authFilesByIndex); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"id":         record.ID,
				"auth_file":  record.AuthFileName,
				"auth_index": record.AuthIndex,
			}).Error("cooldown restore: failed to restore one record")
		}
	}

	return nil
}

// restoreOne 恢复单条 cooldown 记录。
func (w *RestoreWorker) restoreOne(ctx context.Context, record entities.AuthFileCooldown, authFilesByIndex map[string]AuthFileRestoreInfo) error {
	// 检查重试次数限制
	if w.config.MaxAttempts > 0 && record.RestoreAttempts >= w.config.MaxAttempts {
		logrus.WithFields(logrus.Fields{
			"id":               record.ID,
			"auth_file":        record.AuthFileName,
			"restore_attempts": record.RestoreAttempts,
			"max_attempts":     w.config.MaxAttempts,
		}).Warn("cooldown restore: max attempts reached, skipping")
		return nil
	}

	authIndex := record.AuthIndex
	info, found := authFilesByIndex[authIndex]

	// auth file 不存在
	if !found {
		logrus.WithFields(logrus.Fields{
			"id":         record.ID,
			"auth_index": authIndex,
		}).Warn("cooldown restore: auth file not found")
		return repository.MarkMissing(w.db, record.ID)
	}

	// auth file 已经 enabled（外部恢复）
	if !info.Disabled {
		logrus.WithFields(logrus.Fields{
			"id":         record.ID,
			"auth_file":  info.Name,
			"auth_index": authIndex,
		}).Info("cooldown restore: auth file already enabled externally")
		return repository.MarkRecoveredExternal(w.db, record.ID)
	}

	// Keeper 未禁用（用户手动禁用），跳过
	if !record.DisabledByKeeper {
		logrus.WithFields(logrus.Fields{
			"id":         record.ID,
			"auth_file":  info.Name,
			"auth_index": authIndex,
		}).Info("cooldown restore: auth file disabled by user, skipping")
		return repository.MarkSkippedManual(w.db, record.ID)
	}

	// 已经恢复过或标记不活跃的
	if record.State != entities.AuthFileCooldownActive && record.State != entities.AuthFileCooldownRestoreFailed {
		return nil
	}

	// 需要真正恢复
	logrus.WithFields(logrus.Fields{
		"auth_file":  info.Name,
		"auth_index": authIndex,
		"id":         record.ID,
	}).Info("cooldown restore: restoring auth file")

	if w.config.DryRun {
		logrus.WithFields(logrus.Fields{
			"auth_file":  info.Name,
			"auth_index": authIndex,
			"dry_run":    true,
		}).Info("DRY RUN: would restore auth file")
		return repository.MarkRecovered(w.db, record.ID)
	}

	if err := w.client.UpdateAuthFileStatus(ctx, info.Name, false); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"auth_file":  info.Name,
			"auth_index": authIndex,
		}).Error("cooldown restore: failed to restore auth file")
		return repository.MarkRestoreFailed(w.db, record.ID, err.Error())
	}

	return repository.MarkRecovered(w.db, record.ID)
}
