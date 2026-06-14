package cooldown

import (
	"context"
	"fmt"
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

// authFileInfo 是 usage_identities 表中 auth file 的精简信息。
type authFileInfo struct {
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
	authFileMap := s.queryAuthFileMap(authIndexes)

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

		// 2. 解析 recover_at（在检查 active cooldown 之前，因为 extend 需要新的 recover_at）
		recoverAt, resolveErr := ParseRecoverAtFromRequest(
			now,
			reqItem.ResetsAt, reqItem.RecoverAt, reqItem.ResetAt, reqItem.ResetTime,
			reqItem.ResetsInSeconds, reqItem.ResetAfterSeconds, reqItem.RetryAfter,
		)
		if resolveErr != nil {
			item.Status = "skipped_missing_recover_at"
			item.Message = "cannot resolve recover_at: " + resolveErr.Error()
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}

		// 3. 检查 auth file 是否已被手动禁用
		if afi.Disabled {
			item.Status = "skipped_manual_disabled"
			item.Message = "auth file already disabled manually"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}

		// 4. 构建 cooldown 并 upsert
		upstreamMessage := reqItem.UpstreamMessage
		if upstreamMessage == "" {
			upstreamMessage = "usage_limit_reached (inspection)"
		}
		previousDisabled := afi.Disabled
		disabledByKeeper := !previousDisabled

		cd := BuildCooldown(CooldownBuildOptions{
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
			SourceRequestID:  reqItem.SourceRequestID,
		})

		upsertResult, err := repository.UpsertOrExtendActiveCooldown(s.db, cd)
		if err != nil {
			item.Status = "failed"
			item.Message = "upsert cooldown: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.RecoverAt = timeutil.FormatStorageTime(recoverAt)

		// 5. 根据 upsert 结果返回
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

		if dryRun {
			logrus.WithFields(logrus.Fields{
				"auth_file":   afi.Name,
				"auth_index":  authIndex,
				"dry_run":     true,
				"cooldown_id": cd.ID,
			}).Info("DRY RUN: would disable auth file (inspection)")
			item.Status = "dry_run"
			item.Message = "dry run, auth file not actually disabled"
			result.Disabled++
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
func (s *CooldownService) queryAuthFileMap(authIndexes []string) map[string]authFileInfo {
	fileMap := make(map[string]authFileInfo)
	if len(authIndexes) == 0 {
		return fileMap
	}
	var identities []entities.UsageIdentity
	if err := s.db.Where("auth_type = ? AND identity IN ? AND is_deleted = ? AND type = ?",
		entities.UsageIdentityAuthTypeAuthFile, authIndexes, false, "codex",
	).Find(&identities).Error; err != nil {
		logrus.WithError(err).Error("failed to query usage identities for cooldown")
		return fileMap
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
		fileMap[idx] = authFileInfo{
			Name:     name,
			Path:     path,
			Disabled: disabled,
		}
	}
	return fileMap
}
