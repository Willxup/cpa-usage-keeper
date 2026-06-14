package cooldown

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// AuthFilesProvider 提供已解析的 auth file DTO 列表。
type AuthFilesProvider interface {
	Files() []AuthFileDTO
}

// AuthFileDTO 是 cooldown 使用的简化的 auth file DTO。
type AuthFileDTO struct {
	Name      string
	AuthIndex string
	Provider  string
	Disabled  bool
	Path      string
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

// HandleUsageLimit429 处理单条 Codex 429 usage_limit_reached 事件。
// 1. 验证事件是否为 codex + 429 + usage_limit_reached
// 2. 解析 recover_at
// 3. 使用 auth_index 匹配 auth file
// 4. 判断当前 disabled 状态
// 5. Upsert cooldown（只延长不缩短）
// 6. 如需禁用，调用 CPA API
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
	var authFileName string
	var authFilePath string
	var previousDisabled bool

	authFilesResult, err := s.client.FetchAuthFiles(ctx)
	if err != nil {
		logrus.WithError(err).WithField("auth_index", tel.AuthIndex).
			Error("failed to fetch auth files for cooldown")
		cooldown := s.buildCooldown(tel, *recoverAt, "", "", false, false)
		cooldown.LastError = fmt.Sprintf("fetch auth files: %v", err)
		if _, upsertErr := repository.UpsertCooldownExtendOnly(s.db, cooldown); upsertErr != nil {
			logrus.WithError(upsertErr).Error("failed to upsert cooldown after fetch error")
		}
		return fmt.Errorf("fetch auth files for cooldown: %w", err)
	}

	if authFilesResult == nil || authFilesResult.Payload == nil {
		logrus.WithField("auth_index", tel.AuthIndex).
			Error("FetchAuthFiles returned nil result or nil payload")
		cooldown := s.buildCooldown(tel, *recoverAt, "", "", false, false)
		cooldown.LastError = "FetchAuthFiles returned nil result"
		if _, upsertErr := repository.UpsertCooldownExtendOnly(s.db, cooldown); upsertErr != nil {
			logrus.WithError(upsertErr).Error("failed to upsert cooldown after nil result")
		}
		return fmt.Errorf("FetchAuthFiles returned nil result for cooldown")
	}

	for _, file := range authFilesResult.Payload.Files {
		if strings.TrimSpace(file.Provider) == "codex" && strings.TrimSpace(file.AuthIndex) == tel.AuthIndex {
			authFileName = strings.TrimSpace(file.Name)
			authFilePath = strings.TrimSpace(file.Path)
			if file.Disabled != nil {
				previousDisabled = *file.Disabled
			}
			break
		}
	}

	if authFileName == "" {
		logrus.WithField("auth_index", tel.AuthIndex).Warn("no matching auth file found for cooldown")
		cooldown := s.buildCooldown(tel, *recoverAt, "", "", false, false)
		cooldown.LastError = fmt.Sprintf("no matching auth file for auth_index %s", tel.AuthIndex)
		if _, upsertErr := repository.UpsertCooldownExtendOnly(s.db, cooldown); upsertErr != nil {
			logrus.WithError(upsertErr).Error("failed to upsert cooldown when auth file not found")
		}
		return nil
	}

	// 4. 判断当前 disabled 状态
	disabledByKeeper := !previousDisabled

	// 5. 构建并 upsert cooldown
	cooldown := s.buildCooldown(tel, *recoverAt, authFileName, authFilePath, previousDisabled, disabledByKeeper)
	if tel.RawErrorBody != "" {
		cooldown.LastErrorBody = tel.RawErrorBodyTruncated()
	}

	upserted, err := repository.UpsertCooldownExtendOnly(s.db, cooldown)
	if err != nil {
		logrus.WithError(err).WithField("auth_index", tel.AuthIndex).
			Error("failed to upsert cooldown record")
		return fmt.Errorf("upsert cooldown: %w", err)
	}
	if !upserted {
		logrus.WithField("auth_index", tel.AuthIndex).
			Debug("cooldown already exists with later or equal recover_at, skipped")
		return nil
	}

	// 6. 如果用户已手动禁用，不调用 CPA API
	if !disabledByKeeper {
		logrus.WithFields(logrus.Fields{
			"auth_file":  authFileName,
			"auth_index": tel.AuthIndex,
		}).Info("auth file already disabled, keeper will not auto-restore")
		if cooldown.ID > 0 {
			_ = repository.MarkSkippedManual(s.db, cooldown.ID)
		}
		return nil
	}

	if !previousDisabled {
		logrus.WithFields(logrus.Fields{
			"auth_file":  authFileName,
			"auth_index": tel.AuthIndex,
		}).Info("cooldown: disabling auth file due to usage limit")

		if cooldown.ID == 0 {
			return fmt.Errorf("cooldown ID is 0 after upsert, cannot disable auth file")
		}
		if err := s.DisableAuthFile(ctx, cooldown.ID, authFileName, tel.AuthIndex); err != nil {
			return err
		}
	}

	return nil
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

// ValidateRecoverAtEx 从巡检 API 调用响应中解析 recover_at，支持多种字段名。
// 按优先级：resets_at > reset_at > reset_time > resets_in_seconds > reset_after_seconds > retry_after。
// 返回 nil 表示无法解析。
func ValidateRecoverAtEx(now time.Time, raw map[string]any) *time.Time {
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
