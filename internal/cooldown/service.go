package cooldown

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// CooldownClient 是 CooldownService 依赖的 CPA client 接口。
type CooldownClient interface {
	FetchAuthFiles(ctx context.Context) (*response.AuthFilesResult, error)
	UpdateAuthFileStatus(ctx context.Context, name string, disabled bool) error
}

// CooldownService 处理 Codex 429 usage_limit_reached 事件的自动禁用逻辑。
type CooldownService struct {
	db     *gorm.DB
	client CooldownClient
	dryRun bool
	now    func() time.Time
}

// NewCooldownService 创建 CooldownService。
func NewCooldownService(db *gorm.DB, client CooldownClient, dryRun bool) *CooldownService {
	return &CooldownService{
		db:     db,
		client: client,
		dryRun: dryRun,
		now:    time.Now,
	}
}

// codexAuthFileInfo 是从 CPA auth files 解析出的单条 codex 账号信息，仅供 cooldown 内部使用。
type codexAuthFileInfo struct {
	Name             string
	Path             string
	PreviousDisabled bool
	Found            bool
}

// findCodexAuthFileByAuthIndex 在 CPA auth files 中按 auth_index 查找 codex 账号。
func findCodexAuthFileByAuthIndex(files []authfiles.AuthFile, authIndex string) codexAuthFileInfo {
	for _, file := range files {
		if strings.TrimSpace(file.Provider) != "codex" || strings.TrimSpace(file.AuthIndex) != authIndex {
			continue
		}
		info := codexAuthFileInfo{
			Name:  strings.TrimSpace(file.Name),
			Path:  strings.TrimSpace(file.Path),
			Found: true,
		}
		if file.Disabled != nil {
			info.PreviousDisabled = *file.Disabled
		}
		return info
	}
	return codexAuthFileInfo{}
}

// recordCooldownError 只记录错误日志，不写入 active cooldown。
// 原因：FetchAuthFiles 失败或 auth file not found 是临时状态，写入 active cooldown 会阻止
// 后续 429 事件重新尝试 FetchAuthFiles/DisableAuthFile（UpsertOrExtendActiveCooldown 会
// 把它当成已处理的 cooldown）。所以这里只 log，让下次 429 事件能正常重试完整流程。
func (s *CooldownService) recordCooldownError(tel *service.Codex429Telemetry, recoverAt time.Time, lastError string) {
	logrus.WithFields(logrus.Fields{
		"auth_index": tel.AuthIndex,
		"request_id": tel.RequestID,
		"recover_at": recoverAt,
		"last_error": lastError,
	}).Warn("cooldown error recorded (not persisted, will retry on next 429 event)")
}

// HandleUsageLimit429 处理单条 Codex 429 usage_limit_reached 事件。
// 优化：先查 DB 中是否已有 active cooldown，避免每次都调用 FetchAuthFiles。
func (s *CooldownService) HandleUsageLimit429(ctx context.Context, tel *service.Codex429Telemetry) error {
	if tel == nil {
		return nil
	}
	if s.db == nil {
		return fmt.Errorf("cooldown service database is nil")
	}

	if strings.ToLower(strings.TrimSpace(tel.Provider)) != "codex" {
		return nil
	}
	if tel.StatusCode != 429 || tel.ErrorType != "usage_limit_reached" {
		return nil
	}

	recoverAt, err := ResolveRecoverAt(s.now(), tel.ResetsAt, tel.ResetsInSec)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"provider":   tel.Provider,
			"auth_index": tel.AuthIndex,
			"request_id": tel.RequestID,
			"error":      err.Error(),
		}).Warn("codex 429 usage_limit_reached but recover_at invalid, skipping cooldown")
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"provider":   tel.Provider,
		"auth_index": tel.AuthIndex,
		"request_id": tel.RequestID,
		"recover_at": recoverAt,
	}).Info("codex 429 usage_limit_reached detected, processing cooldown")

	// 先查 DB 中是否已有 active cooldown，避免不必要的 FetchAuthFiles 调用
	existing, err := repository.GetActiveCooldownByAuthIndex(s.db, "codex", tel.AuthIndex)
	if err != nil {
		return fmt.Errorf("get active cooldown: %w", err)
	}
	if existing != nil {
		if !recoverAt.After(existing.RecoverAt) {
			logrus.WithFields(logrus.Fields{
				"auth_index":       tel.AuthIndex,
				"existing_recover": existing.RecoverAt,
				"new_recover":      recoverAt,
			}).Debug("cooldown already active with later recover_at, skip")
			return nil
		}
		// 新 recover_at 更晚，只更新 recover_at
		cd := buildAuthFileCooldown(authFileCooldownBuildOptions{
			AuthIndex:        tel.AuthIndex,
			AuthFileName:     existing.AuthFileName,
			AuthFilePath:     existing.AuthFilePath,
			RecoverAt:        recoverAt,
			Reason:           entities.AuthFileCooldownReasonCodex429,
			Owner:            entities.AuthFileCooldownOwnerUsage429,
			Source:           entities.AuthFileCooldownSourceRequestEvent,
			PreviousDisabled: existing.PreviousDisabled,
			DisabledByKeeper: existing.DisabledByKeeper,
			UpstreamCode:     429,
			UpstreamMessage:  "usage_limit_reached",
			SourceEventKey:   tel.RequestID,
			SourceRequestID:  tel.RequestID,
		})
		if _, err := repository.UpsertOrExtendActiveCooldown(s.db, cd); err != nil {
			return fmt.Errorf("extend cooldown recover_at: %w", err)
		}
		logrus.WithFields(logrus.Fields{
			"auth_index": tel.AuthIndex,
			"recover_at": recoverAt,
		}).Info("cooldown recover_at extended, skip re-disabling auth file")
		return nil
	}

	// 首次 cooldown — 需要 FetchAuthFiles 查出 auth file 信息
	authFilesResult, err := s.client.FetchAuthFiles(ctx)
	if err != nil {
		logrus.WithError(err).WithField("auth_index", tel.AuthIndex).
			Error("failed to fetch auth files for cooldown")
		s.recordCooldownError(tel, recoverAt, fmt.Sprintf("fetch auth files: %v", err))
		return fmt.Errorf("fetch auth files for cooldown: %w", err)
	}
	if authFilesResult == nil {
		logrus.WithField("auth_index", tel.AuthIndex).
			Error("FetchAuthFiles returned nil result")
		s.recordCooldownError(tel, recoverAt, "FetchAuthFiles returned nil result")
		return fmt.Errorf("FetchAuthFiles returned nil result for cooldown")
	}

	info := findCodexAuthFileByAuthIndex(authFilesResult.Payload.Files, tel.AuthIndex)
	if !info.Found {
		logrus.WithField("auth_index", tel.AuthIndex).Warn("no matching auth file found for cooldown")
		s.recordCooldownError(tel, recoverAt, fmt.Sprintf("no matching auth file for auth_index %s", tel.AuthIndex))
		return nil
	}

	disabledByKeeper := !info.PreviousDisabled
	cd := buildAuthFileCooldown(authFileCooldownBuildOptions{
		AuthFileName:     info.Name,
		AuthFilePath:     info.Path,
		AuthIndex:        tel.AuthIndex,
		RecoverAt:        recoverAt,
		Reason:           entities.AuthFileCooldownReasonCodex429,
		Owner:            entities.AuthFileCooldownOwnerUsage429,
		Source:           entities.AuthFileCooldownSourceRequestEvent,
		PreviousDisabled: info.PreviousDisabled,
		DisabledByKeeper: disabledByKeeper,
		UpstreamCode:     429,
		UpstreamMessage:  "usage_limit_reached",
		SourceEventKey:   tel.RequestID,
		SourceRequestID:  tel.RequestID,
	})
	if tel.RawErrorBody != "" {
		cd.LastErrorBody = tel.RawErrorBodyTruncated()
	}

	upsertResult, err := repository.UpsertOrExtendActiveCooldown(s.db, cd)
	if err != nil {
		logrus.WithError(err).WithField("auth_index", tel.AuthIndex).
			Error("failed to upsert cooldown record")
		return fmt.Errorf("upsert cooldown: %w", err)
	}

	if upsertResult != repository.CooldownUpsertCreated {
		return nil
	}

	if !disabledByKeeper {
		logrus.WithFields(logrus.Fields{
			"auth_file":  info.Name,
			"auth_index": tel.AuthIndex,
		}).Info("auth file already disabled, keeper will not auto-restore")
		if cd.ID > 0 {
			_ = repository.MarkSkippedManual(s.db, cd.ID)
		}
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"auth_file":  info.Name,
		"auth_index": tel.AuthIndex,
	}).Info("cooldown: disabling auth file due to usage limit")

	if cd.ID == 0 {
		return fmt.Errorf("cooldown ID is 0 after upsert, cannot disable auth file")
	}
	return s.DisableAuthFile(ctx, cd.ID, info.Name, tel.AuthIndex)
}

// DisableAuthFile 调用 CPA API 禁用 auth file，并根据结果标记 cooldown 状态。
func (s *CooldownService) DisableAuthFile(ctx context.Context, cooldownID int64, authFileName, authIndex string) error {
	if s.dryRun {
		logrus.WithFields(logrus.Fields{
			"auth_file":   authFileName,
			"auth_index":  authIndex,
			"dry_run":     true,
			"cooldown_id": cooldownID,
		}).Info("DRY RUN: would disable auth file")
		return nil
	}

	if err := s.client.UpdateAuthFileStatus(ctx, authFileName, true); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"auth_file":  authFileName,
			"auth_index": authIndex,
		}).Error("failed to disable auth file via CPA API")
		if cooldownID > 0 {
			_ = repository.MarkDisableFailed(s.db, cooldownID, err.Error(), "")
		}
		return fmt.Errorf("disable auth file %s: %w", authFileName, err)
	}

	if cooldownID > 0 {
		if err := repository.MarkDisabled(s.db, cooldownID); err != nil {
			logrus.WithError(err).Error("failed to mark cooldown disabled after successful API call")
		}
	}
	return nil
}
