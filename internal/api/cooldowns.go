package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/cooldown"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

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

// disableLimitedAPIRequest 是巡检临时禁用接口的 JSON 请求体。
type disableLimitedAPIRequest struct {
	AuthIndexes []string                       `json:"auth_indexes"`
	Items       []disableLimitedAPIRequestItem `json:"items,omitempty"`
	DryRun      *bool                          `json:"dry_run,omitempty"`
}

type disableLimitedAPIRequestItem struct {
	AuthIndex         string `json:"auth_index"`
	RecoverAt         string `json:"recover_at,omitempty"`
	ResetsAt          string `json:"resets_at,omitempty"`
	ResetAt           string `json:"reset_at,omitempty"`
	ResetTime         string `json:"reset_time,omitempty"`
	ResetsInSeconds   *int64 `json:"resets_in_seconds,omitempty"`
	ResetAfterSeconds *int64 `json:"reset_after_seconds,omitempty"`
	RetryAfter        *int64 `json:"retry_after,omitempty"`
	UpstreamMessage   string `json:"upstream_message,omitempty"`
	SourceRequestID   string `json:"request_id,omitempty"`
}

// disableLimitedAPIResponse 是巡检临时禁用接口的 JSON 响应体。
type disableLimitedAPIResponse struct {
	Total    int                             `json:"total"`
	Disabled int                             `json:"disabled"`
	Extended int                             `json:"extended"`
	Skipped  int                             `json:"skipped"`
	Failed   int                             `json:"failed"`
	DryRun   int                             `json:"dry_run"`
	Items    []disableLimitedAPIResponseItem `json:"items"`
}

type disableLimitedAPIResponseItem struct {
	AuthIndex    string `json:"auth_index"`
	AuthFileName string `json:"auth_file_name,omitempty"`
	Status       string `json:"status"`
	RecoverAt    string `json:"recover_at,omitempty"`
	Message      string `json:"message,omitempty"`
}

func registerCooldownRoutes(router gin.IRoutes, db *gorm.DB, cooldownService *cooldown.CooldownService) {
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
	router.POST("/cooldowns/inspection/disable-limited", func(c *gin.Context) {
		if cooldownService == nil {
			writeInternalError(c, "cooldown service not configured", nil)
			return
		}

		var req disableLimitedAPIRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		if len(req.AuthIndexes) == 0 && len(req.Items) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes or items is required"})
			return
		}
		if len(req.AuthIndexes)+len(req.Items) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "too many auth_indexes, max 100"})
			return
		}

		// 转换为 service 层请求
		svcReq := cooldown.DisableLimitedRequest{
			AuthIndexes: req.AuthIndexes,
			DryRun:      req.DryRun,
		}
		for _, item := range req.Items {
			svcReq.Items = append(svcReq.Items, cooldown.DisableLimitedRequestItem{
				AuthIndex:         item.AuthIndex,
				RecoverAt:         item.RecoverAt,
				ResetsAt:          item.ResetsAt,
				ResetAt:           item.ResetAt,
				ResetTime:         item.ResetTime,
				ResetsInSeconds:   item.ResetsInSeconds,
				ResetAfterSeconds: item.ResetAfterSeconds,
				RetryAfter:        item.RetryAfter,
				UpstreamMessage:   item.UpstreamMessage,
				SourceRequestID:   item.SourceRequestID,
			})
		}

		result, err := cooldownService.DisableLimitedInspectionAccounts(c.Request.Context(), svcReq)
		if err != nil {
			writeInternalError(c, "disable limited failed", err)
			return
		}

		resp := disableLimitedAPIResponse{
			Total:    result.Total,
			Disabled: result.Disabled,
			Extended: result.Extended,
			Skipped:  result.Skipped,
			Failed:   result.Failed,
			DryRun:   result.DryRun,
			Items:    make([]disableLimitedAPIResponseItem, 0, len(result.Items)),
		}
		for _, item := range result.Items {
			resp.Items = append(resp.Items, disableLimitedAPIResponseItem{
				AuthIndex:    item.AuthIndex,
				AuthFileName: item.AuthFileName,
				Status:       item.Status,
				RecoverAt:    item.RecoverAt,
				Message:      item.Message,
			})
		}

		c.JSON(http.StatusOK, resp)
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
