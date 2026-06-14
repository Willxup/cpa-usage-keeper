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
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// CooldownClient 是 CooldownService 依赖的 CPA client 接口。
// 签名与 cpa.Client 的 FetchAuthFiles / UpdateAuthFileStatus 一致。
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
	Name            string
	Path            string
	PreviousDisabled bool
	Found           bool
}

// findCodexAuthFileByAuthIndex 在 CPA auth files 中按 auth_index 查找 codex 账号。
// 返回的 Found 为 false 表示未找到匹配。
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

// recordCooldownError 构建一条带 lastError 的 cooldown 并写入，用于 FetchAuthFiles 失败、auth file 不匹配等容错场景。
// 写入失败只记录日志，不返回错误，避免错误处理本身再抛错打断主流程。
func (s *CooldownService) recordCooldownError(tel *service.Codex429Telemetry, recoverAt time.Time, lastError string) {
	cooldown := s.buildCooldown(tel, recoverAt, "", "", false, false)
	cooldown.LastError = lastError
	if _, err := repository.UpsertCooldownExtendOnly(s.db, cooldown); err != nil {
		logrus.WithError(err).WithField("auth_index", tel.AuthIndex).
			Error("failed to upsert cooldown when recording error")
	}
}

// HandleUsageLimit429 处理单条 Codex 429 usage_limit_reached 事件。
// 1. 验证事件是否为 codex + 429 + usage_limit_reached
// 2. 解析 recover_at
// 3. 使用 auth_index 匹配 auth file
// 4. Upsert cooldown（只延长不缩短）
// 5. 仅当首次新建 cooldown 时调用 CPA API 禁用；延长或无变化时不重复禁用
func (s *CooldownService) HandleUsageLimit429(ctx context.Context, tel *service.Codex429Telemetry) error {
	if tel == nil {
		return nil
	}
	if s.db == nil {
		return fmt.Errorf("cooldown service database is nil")
	}

	// 1. 验证事件条件
	if strings.ToLower(strings.TrimSpace(tel.Provider)) != "codex" {
		return nil
	}
	if tel.StatusCode != 429 {
		return nil
	}
	if tel.ErrorType != "usage_limit_reached" {
		return nil
	}

	// 2. 解析 recover_at
	recoverAt := repository.ValidateCooldownRecoverAt(s.now(), tel.ResetsAt, tel.ResetsInSec)
	if recoverAt == nil {
		logrus.WithFields(logrus.Fields{
			"provider":   tel.Provider,
			"auth_index": tel.AuthIndex,
			"request_id": tel.RequestID,
		}).Warn("codex 429 usage_limit_reached but no valid resets_at or resets_in_seconds, skipping cooldown")
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"provider":   tel.Provider,
		"auth_index": tel.AuthIndex,
		"request_id": tel.RequestID,
		"recover_at": *recoverAt,
	}).Info("codex 429 usage_limit_reached detected, processing cooldown")

	// 3. 使用 auth_index 定位 auth file
	authFilesResult, err := s.client.FetchAuthFiles(ctx)
	if err != nil {
		logrus.WithError(err).WithField("auth_index", tel.AuthIndex).
			Error("failed to fetch auth files for cooldown")
		s.recordCooldownError(tel, *recoverAt, fmt.Sprintf("fetch auth files: %v", err))
		return fmt.Errorf("fetch auth files for cooldown: %w", err)
	}
	if authFilesResult == nil || authFilesResult.Payload == nil {
		logrus.WithField("auth_index", tel.AuthIndex).
			Error("FetchAuthFiles returned nil result or payload")
		s.recordCooldownError(tel, *recoverAt, "FetchAuthFiles returned nil result or payload")
		return fmt.Errorf("FetchAuthFiles returned nil result for cooldown")
	}

	info := findCodexAuthFileByAuthIndex(authFilesResult.Payload.Files, tel.AuthIndex)
	if !info.Found {
		logrus.WithField("auth_index", tel.AuthIndex).Warn("no matching auth file found for cooldown")
		s.recordCooldownError(tel, *recoverAt, fmt.Sprintf("no matching auth file for auth_index %s", tel.AuthIndex))
		return nil
	}

	// 4. 判断当前 disabled 状态并构建 cooldown
	disabledByKeeper := !info.PreviousDisabled
	cooldown := s.buildCooldown(tel, *recoverAt, info.Name, info.Path, info.PreviousDisabled, disabledByKeeper)
	if tel.RawErrorBody != "" {
		cooldown.LastErrorBody = tel.RawErrorBodyTruncated()
	}

	// 5. 写入 cooldown，根据结果决定是否需要调用 CPA API
	upsertResult, err := repository.UpsertCooldownExtendOnly(s.db, cooldown)
	if err != nil {
		logrus.WithError(err).WithField("auth_index", tel.AuthIndex).
			Error("failed to upsert cooldown record")
		return fmt.Errorf("upsert cooldown: %w", err)
	}

	// extended / unchanged 表示已有 cooldown 记录，auth file 的禁用状态已经处理过，不重复调用 CPA API。
	if upsertResult != repository.CooldownUpsertCreated {
		if upsertResult == repository.CooldownUpsertExtended {
			logrus.WithFields(logrus.Fields{
				"auth_file":  info.Name,
				"auth_index": tel.AuthIndex,
			}).Info("cooldown recover_at extended, skip re-disabling auth file")
		}
		return nil
	}

	// 仅新建 cooldown 时，根据 disabled 状态决定后续动作。
	// 如果用户已手动禁用，不调用 CPA API，标记为 skipped_manual。
	if !disabledByKeeper {
		logrus.WithFields(logrus.Fields{
			"auth_file":  info.Name,
			"auth_index": tel.AuthIndex,
		}).Info("auth file already disabled, keeper will not auto-restore")
		if cooldown.ID > 0 {
			_ = repository.MarkSkippedManual(s.db, cooldown.ID)
		}
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"auth_file":  info.Name,
		"auth_index": tel.AuthIndex,
	}).Info("cooldown: disabling auth file due to usage limit")

	if cooldown.ID == 0 {
		return fmt.Errorf("cooldown ID is 0 after upsert, cannot disable auth file")
	}
	return s.DisableAuthFile(ctx, cooldown.ID, info.Name, tel.AuthIndex)
}

func (s *CooldownService) buildCooldown(tel *service.Codex429Telemetry, recoverAt time.Time, authFileName, authFilePath string, previousDisabled bool, disabledByKeeper bool) *entities.AuthFileCooldown {
	return &entities.AuthFileCooldown{
		Provider:           "codex",
		AuthIndex:          tel.AuthIndex,
		AuthFileName:       authFileName,
		AuthFilePath:       authFilePath,
		RecoverAt:          recoverAt,
		Reason:             entities.AuthFileCooldownReasonCodex429,
		Owner:              entities.AuthFileCooldownOwnerUsage429,
		State:              entities.AuthFileCooldownActive,
		DisabledByKeeper:   disabledByKeeper,
		PreviousDisabled:   previousDisabled,
		Source:             entities.AuthFileCooldownSourceRequestEvent,
		UpstreamStatusCode: 429,
		UpstreamMessage:    "usage_limit_reached",
		SourceEventKey:     tel.RequestID,
		SourceRequestID:    tel.RequestID,
	}
}

// BuildInspectionCooldown 构建巡检来源的 cooldown 记录。
func (s *CooldownService) BuildInspectionCooldown(authIndex, authFileName, authFilePath string, recoverAt time.Time, previousDisabled bool, disabledByKeeper bool, upstreamMessage string) *entities.AuthFileCooldown {
	return &entities.AuthFileCooldown{
		Provider:           "codex",
		AuthIndex:          authIndex,
		AuthFileName:       authFileName,
		AuthFilePath:       authFilePath,
		RecoverAt:          recoverAt,
		Reason:             entities.AuthFileCooldownReasonInspection429,
		Owner:              entities.AuthFileCooldownOwnerInspection429,
		State:              entities.AuthFileCooldownActive,
		DisabledByKeeper:   disabledByKeeper,
		PreviousDisabled:   previousDisabled,
		Source:             entities.AuthFileCooldownSourceInspection,
		UpstreamStatusCode: 429,
		UpstreamMessage:    upstreamMessage,
	}
}

// DisableAuthFile 调用 CPA API 禁用 auth file，并根据结果标记 cooldown 状态。
// 不在事务中执行 HTTP 调用。返回 error 表示 CPA API 调用失败。
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

// ValidateRecoverAt 工具方法，供外部调用。
func ValidateRecoverAt(now time.Time, resetsAt *time.Time, resetsInSeconds *int64) *time.Time {
	return repository.ValidateCooldownRecoverAt(now, resetsAt, resetsInSeconds)
}

// ParseInspectionRecoverAt 从巡检 API 调用响应中解析 recover_at，支持多种字段名。
// 按优先级：resets_at > reset_at > reset_time > resets_in_seconds > reset_after_seconds > retry_after。
// 返回 nil 表示无法解析。
func ParseInspectionRecoverAt(now time.Time, raw map[string]any) *time.Time {
	type parser struct {
		timeField    string
		secondsField string
	}
	parsers := []parser{
		{timeField: "resets_at"},
		{timeField: "reset_at"},
		{timeField: "reset_time"},
		{secondsField: "resets_in_seconds"},
		{secondsField: "reset_after_seconds"},
		{secondsField: "retry_after"},
	}
	for _, p := range parsers {
		if p.timeField != "" {
			if v, ok := raw[p.timeField]; ok {
				switch tv := v.(type) {
				case string:
					t, err := time.Parse(time.RFC3339, tv)
					if err == nil && !t.IsZero() {
						n := timeutil.NormalizeStorageTime(t)
						return &n
					}
				case float64:
					t := time.Unix(int64(tv), 0)
					if !t.IsZero() {
						n := timeutil.NormalizeStorageTime(t)
						return &n
					}
				}
			}
		}
		if p.secondsField != "" {
			if v, ok := raw[p.secondsField]; ok {
				switch sv := v.(type) {
				case float64:
					if sv > 0 {
						n := timeutil.NormalizeStorageTime(now.Add(time.Duration(int64(sv)) * time.Second))
						return &n
					}
				case int64:
					if sv > 0 {
						n := timeutil.NormalizeStorageTime(now.Add(time.Duration(sv) * time.Second))
						return &n
					}
				}
			}
		}
	}
	return nil
}

// GetDB 返回 CooldownService 使用的数据库实例。
func (s *CooldownService) GetDB() *gorm.DB {
	return s.db
}

// GetDryRun 返回当前 dry-run 模式状态。
func (s *CooldownService) GetDryRun() bool {
	return s.dryRun
}
