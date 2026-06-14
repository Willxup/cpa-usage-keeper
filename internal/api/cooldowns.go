package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// CooldownDisabler 提供巡检手动禁用接口所需的方法。
type CooldownDisabler interface {
	GetDB() *gorm.DB
	GetDryRun() bool
	BuildInspectionCooldown(authIndex, authFileName, authFilePath string, recoverAt time.Time, previousDisabled bool, disabledByKeeper bool, upstreamMessage string, sourceRequestID string) *entities.AuthFileCooldown
	DisableAuthFile(ctx context.Context, cooldownID int64, authFileName, authIndex string) error
}

type cooldownListResponse struct {
	Cooldowns   []cooldownItemResponse `json:"cooldowns"`
	ActiveCount int64                  `json:"active_count"`
	TotalCount  int                    `json:"total_count"`
}

type cooldownItemResponse struct {
	ID                 int64   `json:"id"`
	Provider           string  `json:"provider"`
	AuthIndex          string  `json:"auth_index"`
	AuthFileName       string  `json:"auth_file_name"`
	AuthFilePath       string  `json:"auth_file_path"`
	State              string  `json:"state"`
	Source             string  `json:"source,omitempty"`
	RecoverAt          *string `json:"recover_at,omitempty"`
	RecoverInSeconds   *int64  `json:"recover_in_seconds,omitempty"`
	Reason             string  `json:"reason"`
	Owner              string  `json:"owner"`
	DisabledByKeeper   bool    `json:"disabled_by_keeper"`
	PreviousDisabled   bool    `json:"previous_disabled"`
	UpstreamStatusCode int     `json:"upstream_status_code,omitempty"`
	UpstreamMessage    string  `json:"upstream_message,omitempty"`
	LastError          string  `json:"last_error,omitempty"`
	RestoreAttempts    int     `json:"restore_attempts"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	DisabledAt         *string `json:"disabled_at,omitempty"`
	RecoveredAt        *string `json:"recovered_at,omitempty"`
}

// CooldownStatusDTO 是嵌入到 Auth File / identity 响应中的精简 cooldown 状态。
type CooldownStatusDTO struct {
	State              string `json:"state,omitempty"`
	Source             string `json:"source,omitempty"`
	Owner              string `json:"owner,omitempty"`
	Reason             string `json:"reason,omitempty"`
	RecoverAt          string `json:"recover_at,omitempty"`
	RecoverInSeconds   int64  `json:"recover_in_seconds,omitempty"`
	UpstreamStatusCode int    `json:"upstream_status_code,omitempty"`
	UpstreamMessage    string `json:"upstream_message,omitempty"`
	DisabledByKeeper   bool   `json:"disabled_by_keeper"`
	RestoreAttempts    int    `json:"restore_attempts,omitempty"`
	LastError          string `json:"last_error,omitempty"`
}

// disableLimitedRequest 是巡检临时禁用接口的请求体。
type disableLimitedRequest struct {
	AuthIndexes []string                    `json:"auth_indexes"`
	Items       []disableLimitedRequestItem `json:"items,omitempty"`
	DryRun      *bool                       `json:"dry_run,omitempty"`
}

type disableLimitedRequestItem struct {
	AuthIndex string `json:"auth_index"`
	// RecoverAt 是 RFC3339 格式的恢复时间，前端优先从巡检结果里带上。
	RecoverAt string `json:"recover_at,omitempty"`
	// ResetsAt 兼容上游字段名，与 RecoverAt 语义相同，按 resets_at > recover_at 优先级解析。
	ResetsAt        string `json:"resets_at,omitempty"`
	ResetsInSeconds *int64 `json:"resets_in_seconds,omitempty"`
	UpstreamMessage string `json:"upstream_message,omitempty"`
	// SourceRequestID 是触发本次禁用的请求 ID，便于追溯。
	SourceRequestID string `json:"request_id,omitempty"`
}

// disableLimitedResponse 是巡检临时禁用接口的响应体。
type disableLimitedResponse struct {
	Total    int                          `json:"total"`
	Disabled int                          `json:"disabled"`
	Extended int                          `json:"extended"`
	Skipped  int                          `json:"skipped"`
	Failed   int                          `json:"failed"`
	Items    []disableLimitedResponseItem `json:"items"`
}

type disableLimitedResponseItem struct {
	AuthIndex    string `json:"auth_index"`
	AuthFileName string `json:"auth_file_name,omitempty"`
	Status       string `json:"status"`
	RecoverAt    string `json:"recover_at,omitempty"`
	Message      string `json:"message,omitempty"`
}

func registerCooldownRoutes(router gin.IRoutes, db *gorm.DB, disabler CooldownDisabler) {
	router.GET("/cooldowns", func(c *gin.Context) {
		if db == nil {
			c.JSON(http.StatusOK, cooldownListResponse{Cooldowns: []cooldownItemResponse{}, ActiveCount: 0, TotalCount: 0})
			return
		}

		cooldowns, err := repository.ListAllCooldowns(db, 200)
		if err != nil {
			writeInternalError(c, "list cooldowns failed", err)
			return
		}

		activeCount, err := repository.CountActiveCooldowns(db)
		if err != nil {
			activeCount = 0
		}

		now := time.Now()
		items := make([]cooldownItemResponse, 0, len(cooldowns))
		for _, cd := range cooldowns {
			item := buildCooldownItem(cd, now)
			items = append(items, item)
		}

		c.JSON(http.StatusOK, cooldownListResponse{
			Cooldowns:   items,
			ActiveCount: activeCount,
			TotalCount:  len(items),
		})
	})

	// POST /api/v1/cooldowns/inspection/disable-limited
	// 管理员手动禁用巡检发现的限额账号。
	router.POST("/cooldowns/inspection/disable-limited", func(c *gin.Context) {
		if db == nil {
			writeInternalError(c, "database not available", nil)
			return
		}
		if disabler == nil {
			writeInternalError(c, "cooldown disabler not configured", nil)
			return
		}

		var req disableLimitedRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		// 合并 auth_indexes 和 items 中的索引
		authIndexSet := make(map[string]*disableLimitedRequestItem, len(req.AuthIndexes)+len(req.Items))
		for _, idx := range req.AuthIndexes {
			trimmed := strings.TrimSpace(idx)
			if trimmed != "" {
				authIndexSet[trimmed] = &disableLimitedRequestItem{AuthIndex: trimmed}
			}
		}
		for i := range req.Items {
			trimmed := strings.TrimSpace(req.Items[i].AuthIndex)
			if trimmed != "" {
				authIndexSet[trimmed] = &req.Items[i]
				authIndexSet[trimmed].AuthIndex = trimmed
			}
		}

		if len(authIndexSet) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes is required"})
			return
		}
		if len(authIndexSet) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "too many auth_indexes, max 100"})
			return
		}

		authIndexes := make([]string, 0, len(authIndexSet))
		for idx := range authIndexSet {
			authIndexes = append(authIndexes, idx)
		}

		// 查询现有的 active cooldown
		existingCooldowns, err := repository.ListActiveCooldownsByAuthIndexes(db, authIndexes, 100)
		if err != nil {
			writeInternalError(c, "query existing cooldowns failed", err)
			return
		}

		// 从 usage_identities 表查询 auth file 信息（比调用 CPA FetchAuthFiles 更高效）
		type authFileInfo struct {
			Name     string
			Path     string
			Disabled bool
		}
		authFileMap := make(map[string]authFileInfo)
		var identities []entities.UsageIdentity
		if err := db.Where("auth_type = ? AND identity IN ? AND is_deleted = ? AND type = ?",
			entities.UsageIdentityAuthTypeAuthFile, authIndexes, false, "codex",
		).Find(&identities).Error; err != nil {
			logrus.WithError(err).Error("failed to query usage identities for cooldown")
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
			authFileMap[idx] = authFileInfo{
				Name:     name,
				Path:     path,
				Disabled: disabled,
			}
		}

		now := time.Now()
		result := disableLimitedResponse{
			Total: len(authIndexes),
			Items: make([]disableLimitedResponseItem, 0, len(authIndexes)),
		}

		for _, authIndex := range authIndexes {
			item := disableLimitedResponseItem{AuthIndex: authIndex}
			reqItem := authIndexSet[authIndex]

			// 检查 auth file 是否存在
			afi, found := authFileMap[authIndex]
			if !found {
				item.Status = "not_found"
				item.Message = "auth file not found"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}

			item.AuthFileName = afi.Name

			// 检查是否已有 active cooldown
			if existing, ok := existingCooldowns[authIndex]; ok && existing.State == entities.AuthFileCooldownActive {
				item.Status = "already_active"
				item.RecoverAt = timeutil.FormatStorageTime(existing.RecoverAt)
				item.Message = "already in cooldown"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}

			// 解析 recover_at：优先 resets_at > recover_at（RFC3339），其次 resets_in_seconds。
			// 解析逻辑统一走 ResolveCooldownRecoverAt，保证校验一致（必须晚于 now）。
			var recoverAt time.Time
			var resolveErr error
			if reqItem != nil {
				if t, err := time.Parse(time.RFC3339, reqItem.ResetsAt); err == nil && !t.IsZero() {
					recoverAt, resolveErr = repository.ResolveCooldownRecoverAt(now, &t, nil)
				} else if t, err := time.Parse(time.RFC3339, reqItem.RecoverAt); err == nil && !t.IsZero() {
					recoverAt, resolveErr = repository.ResolveCooldownRecoverAt(now, &t, nil)
				} else if reqItem.ResetsInSeconds != nil && *reqItem.ResetsInSeconds > 0 {
					sec := *reqItem.ResetsInSeconds
					recoverAt, resolveErr = repository.ResolveCooldownRecoverAt(now, nil, &sec)
				} else {
					resolveErr = fmt.Errorf("no recover_at / resets_at / resets_in_seconds in request item")
				}
			} else {
				resolveErr = fmt.Errorf("no recover_at info (fallback auth_indexes has no recover time)")
			}
			if resolveErr != nil {
				item.Status = "skipped_missing_recover_at"
				item.Message = "cannot resolve recover_at: " + resolveErr.Error()
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}

			// 如果当前 auth file 已被手动禁用，跳过
			if afi.Disabled {
				item.Status = "skipped_manual_disabled"
				item.Message = "auth file already disabled manually"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}

			// 构建 cooldown
			upstreamMessage := ""
			if reqItem != nil {
				upstreamMessage = reqItem.UpstreamMessage
			}
			if upstreamMessage == "" {
				upstreamMessage = "usage_limit_reached (inspection)"
			}

			previousDisabled := afi.Disabled
			disabledByKeeper := !previousDisabled

			sourceRequestID := ""
			if reqItem != nil {
				sourceRequestID = reqItem.SourceRequestID
			}
			cooldown := disabler.BuildInspectionCooldown(
				authIndex, afi.Name, afi.Path,
				recoverAt, previousDisabled, disabledByKeeper,
				upstreamMessage, sourceRequestID,
			)

			upsertResult, err := repository.UpsertOrExtendActiveCooldown(db, cooldown)
			if err != nil {
				item.Status = "failed"
				item.Message = "upsert cooldown: " + err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}

			item.RecoverAt = timeutil.FormatStorageTime(recoverAt)

			// 已存在 active cooldown 且 recover_at 未被更新（unchanged）：账号已经在禁用流程里，跳过重复禁用。
			if upsertResult == repository.CooldownUpsertUnchanged {
				item.Status = "extended"
				item.Message = "already in cooldown, recover_at unchanged"
				result.Extended++
				result.Items = append(result.Items, item)
				continue
			}

			// 调用 CPA API 禁用
			if cooldown.ID == 0 {
				item.Status = "failed"
				item.Message = "cooldown ID is 0 after upsert"
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			if err := disabler.DisableAuthFile(c.Request.Context(), cooldown.ID, afi.Name, authIndex); err != nil {
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

		c.JSON(http.StatusOK, result)
	})
}

func buildCooldownItem(cd entities.AuthFileCooldown, now time.Time) cooldownItemResponse {
	item := cooldownItemResponse{
		ID:                 cd.ID,
		Provider:           cd.Provider,
		AuthIndex:          cd.AuthIndex,
		AuthFileName:       cd.AuthFileName,
		AuthFilePath:       cd.AuthFilePath,
		State:              string(cd.State),
		Source:             string(cd.Source),
		Reason:             string(cd.Reason),
		Owner:              string(cd.Owner),
		DisabledByKeeper:   cd.DisabledByKeeper,
		PreviousDisabled:   cd.PreviousDisabled,
		UpstreamStatusCode: cd.UpstreamStatusCode,
		UpstreamMessage:    cd.UpstreamMessage,
		LastError:          cd.LastError,
		RestoreAttempts:    cd.RestoreAttempts,
		CreatedAt:          timeutil.FormatStorageTime(cd.CreatedAt),
		UpdatedAt:          timeutil.FormatStorageTime(cd.UpdatedAt),
	}
	if !cd.RecoverAt.IsZero() {
		s := timeutil.FormatStorageTime(cd.RecoverAt)
		item.RecoverAt = &s
		remaining := cd.RecoverAt.Sub(now).Seconds()
		if remaining > 0 {
			sec := int64(remaining)
			item.RecoverInSeconds = &sec
		}
	}
	if cd.DisabledAt != nil && !cd.DisabledAt.IsZero() {
		s := timeutil.FormatStorageTime(*cd.DisabledAt)
		item.DisabledAt = &s
	}
	if cd.RecoveredAt != nil && !cd.RecoveredAt.IsZero() {
		s := timeutil.FormatStorageTime(*cd.RecoveredAt)
		item.RecoveredAt = &s
	}
	return item
}

// BuildCooldownStatusDTO 从 cooldown 记录构建精简的 DTO 供 identity API 使用。
func BuildCooldownStatusDTO(cd *entities.AuthFileCooldown, now time.Time) *CooldownStatusDTO {
	if cd == nil {
		return nil
	}
	dto := &CooldownStatusDTO{
		State:              string(cd.State),
		Source:             string(cd.Source),
		Owner:              string(cd.Owner),
		Reason:             string(cd.Reason),
		UpstreamStatusCode: cd.UpstreamStatusCode,
		UpstreamMessage:    cd.UpstreamMessage,
		DisabledByKeeper:   cd.DisabledByKeeper,
		RestoreAttempts:    cd.RestoreAttempts,
		LastError:          cd.LastError,
	}
	if !cd.RecoverAt.IsZero() {
		dto.RecoverAt = timeutil.FormatStorageTime(cd.RecoverAt)
		remaining := cd.RecoverAt.Sub(now).Seconds()
		if remaining > 0 {
			dto.RecoverInSeconds = int64(remaining)
		}
	}
	return dto
}
