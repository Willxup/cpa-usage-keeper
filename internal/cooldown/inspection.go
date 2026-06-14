package cooldown

import (
	"context"
	"fmt"
	"time"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
)

// DisableLimitedRequest 是巡检临时禁用接口的请求。
type DisableLimitedRequest struct {
	AuthIndexes []string
	Items       []DisableLimitedRequestItem
	DryRun      *bool
}

// DisableLimitedRequestItem 是单个禁用项。
type DisableLimitedRequestItem struct {
	AuthIndex         string
	RecoverAt         string
	ResetsAt          string
	ResetAt           string
	ResetTime         string
	ResetsInSeconds   *int64
	ResetAfterSeconds *int64
	RetryAfter        *int64
	UpstreamMessage   string
	SourceRequestID   string
}

// DisableLimitedResult 是巡检临时禁用的汇总结果。
type DisableLimitedResult struct {
	Total    int
	Disabled int
	Extended int
	Skipped  int
	Failed   int
	DryRunCount int
	Items    []DisableLimitedResultItem
}

// DisableLimitedResultItem 是单个账号的处理结果。
type DisableLimitedResultItem struct {
	AuthIndex    string
	AuthFileName string
	Status       string
	RecoverAt    string
	Message      string
}

// cooldownAuthFileInfo 是 usage_identities 表中 auth file 的精简信息。
type cooldownAuthFileInfo struct {
	Name     string
	Path     string
	Disabled bool
}

// DisableLimitedInspectionAccounts 处理巡检发现的限额账号临时禁用。
func (s *CooldownService) DisableLimitedInspectionAccounts(ctx context.Context, req DisableLimitedRequest) (DisableLimitedResult, error) {
	if s.db == nil {
		return DisableLimitedResult{}, fmt.Errorf("database not available")
	}

	// 合并 auth_indexes 和 items 中的索引
	authIndexSet := make(map[string]*DisableLimitedRequestItem, len(req.AuthIndexes)+len(req.Items))
	for _, idx := range req.AuthIndexes {
		trimmed := strings.TrimSpace(idx)
		if trimmed != "" {
			authIndexSet[trimmed] = &DisableLimitedRequestItem{AuthIndex: trimmed}
		}
	}
	for i := range req.Items {
		trimmed := strings.TrimSpace(req.Items[i].AuthIndex)
		if trimmed != "" {
			authIndexSet[trimmed] = &req.Items[i]
			authIndexSet[trimmed].AuthIndex = trimmed
		}
	}

	authIndexes := make([]string, 0, len(authIndexSet))
	for idx := range authIndexSet {
		authIndexes = append(authIndexes, idx)
	}

	// 从 usage_identities 表查询 auth file 信息
	authFileMap, err := s.queryAuthFileMap(authIndexes)
	if err != nil {
		return DisableLimitedResult{}, err
	}

	// 判断本次请求的 dry_run（request-level 覆盖 service-level）
	dryRun := s.dryRun
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}

	now := s.now()
	result := DisableLimitedResult{
		Total: len(authIndexes),
		Items: make([]DisableLimitedResultItem, 0, len(authIndexes)),
	}

	for _, authIndex := range authIndexes {
		item := DisableLimitedResultItem{AuthIndex: authIndex}
		reqItem := authIndexSet[authIndex]

		// 1. 检查 auth file 是否存在
		afi, found := authFileMap[authIndex]
		if !found {
			item.Status = "not_found"
			item.Message = "auth file not found"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		item.AuthFileName = afi.Name

		// 2. 检查是否已有 active cooldown。
		// 这是 disable-limited 的关键：巡检结果本身不携带 recover_at，
		// 如果该账号已有 active cooldown（之前被 429 自动禁用），则应允许 extend，
		// 而不是因为没有 recover_at 就 skipped。
		existingCooldown, err := repository.GetActiveCooldownByAuthIndex(s.db, "codex", authIndex)
		if err != nil {
			item.Status = "failed"
			item.Message = "get active cooldown: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		// 3. 解析 recover_at：优先用请求项里的值，没有则用 existing cooldown 兜底。
		//    巡检结果不含 recover_at，所以大多数情况下走 existing 兜底路径。
		var recoverAt time.Time
		var resolveErr error
		if reqItem != nil {
			recoverAt, resolveErr = ResolveRecoverAtFromInput(now, RecoverAtInput{
				ResetsAt:        reqItem.ResetsAt,
				RecoverAt:       reqItem.RecoverAt,
				ResetAt:         reqItem.ResetAt,
				ResetTime:       reqItem.ResetTime,
				ResetsInSeconds: reqItem.ResetsInSeconds,
				ResetAfterSec:   reqItem.ResetAfterSeconds,
				RetryAfter:      reqItem.RetryAfter,
			})
		} else {
			resolveErr = fmt.Errorf("no request item")
		}
		if resolveErr != nil {
			if existingCooldown != nil {
				// 请求没带 recover_at，但已有 active cooldown：用 existing 的 recover_at。
				// UpsertOrExtendActiveCooldown 会判断是否需要 extend（新值不晚于旧值则 unchanged）。
				recoverAt = existingCooldown.RecoverAt
			} else {
				// 既没有请求 recover_at，也没有现有 cooldown：无法创建，明确提示。
				item.Status = "skipped_missing_recover_at"
				item.Message = "cannot resolve recover_at (not in request, no existing cooldown): " + resolveErr.Error()
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
		}

		// 4. auth file 已禁用的处理：
		//    - 有 active cooldown（keeper 禁用的）：允许 extend/unchanged，不跳过。
		//    - 无 active cooldown（用户手动禁用的）：跳过，不干预。
		if afi.Disabled && existingCooldown == nil {
			item.Status = "skipped_manual_disabled"
			item.Message = "auth file already disabled manually"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}

		// 5. 构建 cooldown 并 upsert（reqItem 可能为 nil，安全访问）
		upstreamMessage := ""
		sourceRequestID := ""
		if reqItem != nil {
			upstreamMessage = reqItem.UpstreamMessage
			sourceRequestID = reqItem.SourceRequestID
		}
		if upstreamMessage == "" {
			upstreamMessage = "usage_limit_reached (inspection)"
		}
		previousDisabled := afi.Disabled
		disabledByKeeper := !previousDisabled

		cd := buildAuthFileCooldown(authFileCooldownBuildOptions{
			AuthFileName:     afi.Name,
			AuthFilePath:     afi.Path,
			AuthIndex:        authIndex,
			RecoverAt:        recoverAt,
			Reason:           entities.AuthFileCooldownReasonInspection429,
			Owner:            entities.AuthFileCooldownOwnerInspection429,
			Source:           entities.AuthFileCooldownSourceInspection,
			PreviousDisabled: previousDisabled,
			DisabledByKeeper: disabledByKeeper,
			UpstreamCode:     429,
			UpstreamMessage:  upstreamMessage,
			SourceRequestID:  sourceRequestID,
		})

		// dry_run 时只 log，不写入 active cooldown，避免 restore worker 把 dry-run 记录
		// 当成真实 cooldown 来处理（到期后尝试恢复一个从未真正禁用的账号）。
		if dryRun {
			logrus.WithFields(logrus.Fields{
				"auth_file":  afi.Name,
				"auth_index": authIndex,
				"dry_run":    true,
				"recover_at": recoverAt,
			}).Info("DRY RUN: would upsert cooldown and disable auth file (inspection)")
			item.Status = "dry_run"
			item.Message = "dry run, no cooldown written, auth file not disabled"
			item.RecoverAt = timeutil.FormatStorageTime(recoverAt)
			result.DryRunCount++
			result.Items = append(result.Items, item)
			continue
		}

		upsertResult, err := repository.UpsertOrExtendActiveCooldown(s.db, cd)
		if err != nil {
			item.Status = "failed"
			item.Message = "upsert cooldown: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.RecoverAt = timeutil.FormatStorageTime(recoverAt)

		// 6. 根据 upsert 结果返回
		switch upsertResult {
		case repository.CooldownUpsertUnchanged:
			item.Status = "unchanged"
			item.Message = "already in cooldown, recover_at unchanged"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		case repository.CooldownUpsertExtended:
			item.Status = "extended"
			item.Message = "cooldown recover_at extended"
			result.Extended++
			result.Items = append(result.Items, item)
			continue
		}

		// CooldownUpsertCreated — 新建 cooldown，需要调 CPA API 禁用
		if cd.ID == 0 {
			item.Status = "failed"
			item.Message = "cooldown ID is 0 after upsert"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		if err := s.DisableAuthFile(ctx, cd.ID, afi.Name, authIndex); err != nil {
			item.Status = "failed"
			item.Message = "disable failed: " + err.Error()
			result.Failed++
		} else {
			item.Status = "disabled"
			item.Message = "auth file disabled"
			result.Disabled++
		}
		result.Items = append(result.Items, item)
	}

	return result, nil
}

// queryAuthFileMap 从 usage_identities 表查询 auth file 信息。
// 查询失败时返回 error，避免调用方把所有账号误判为 not_found。
func (s *CooldownService) queryAuthFileMap(authIndexes []string) (map[string]cooldownAuthFileInfo, error) {
	fileMap := make(map[string]cooldownAuthFileInfo)
	if len(authIndexes) == 0 {
		return fileMap, nil
	}
	var identities []entities.UsageIdentity
	if err := s.db.Where("auth_type = ? AND identity IN ? AND is_deleted = ? AND type = ?",
		entities.UsageIdentityAuthTypeAuthFile, authIndexes, false, "codex",
	).Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("query usage identities for cooldown: %w", err)
	}
	for _, id := range identities {
		idx := strings.TrimSpace(id.Identity)
		disabled := false
		if id.Disabled != nil {
			disabled = *id.Disabled
		}
		name := ""
		if id.FileName != nil {
			name = *id.FileName
		}
		path := ""
		if id.FilePath != nil {
			path = *id.FilePath
		}
		fileMap[idx] = cooldownAuthFileInfo{
			Name:     name,
			Path:     path,
			Disabled: disabled,
		}
	}
	return fileMap, nil
}
