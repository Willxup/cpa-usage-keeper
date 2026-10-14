package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

const (
	// usageSummaryMinWindow/usageSummaryMaxWindow 是 windows 参数的闭区间边界。
	// 上界与最近事件缓存的覆盖范围（70 分钟）保持余量，保证固定 cache 数据源永远完整。
	usageSummaryMinWindow = time.Second
	usageSummaryMaxWindow = 60 * time.Minute
)

type usageSummaryWindowPayload struct {
	Window              string    `json:"window"`
	WindowSeconds       int64     `json:"window_seconds"`
	WindowStart         time.Time `json:"window_start"`
	WindowEnd           time.Time `json:"window_end"`
	Requests            int64     `json:"requests"`
	Failures            int64     `json:"failures"`
	TokenRequests       int64     `json:"token_requests"`
	CacheHitRequests    int64     `json:"cache_hit_requests"`
	TotalTokens         int64     `json:"total_tokens"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	ReasoningTokens     int64     `json:"reasoning_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	// CacheRate 是 token 加权的缓存读取率（0-100），InputTokens 为 0 时缺省。
	CacheRate *float64 `json:"cache_rate,omitempty"`
	// CacheHitRate 是请求加权的缓存命中率（0-100），无 token 请求时缺省。
	CacheHitRate *float64 `json:"cache_hit_rate,omitempty"`
	// CostUSD 仅在全部事件都有价格时给出，否则缺省并置 CostAvailable=false。
	CostUSD       *float64 `json:"cost_usd,omitempty"`
	CostAvailable bool     `json:"cost_available"`
	TPM           float64  `json:"tpm"`
	RPM           float64  `json:"rpm"`
}

type usageSummaryQuotaPayload struct {
	Status         string               `json:"status"`
	FileName       *string              `json:"file_name,omitempty"`
	Quota          *quota.CheckResponse `json:"quota,omitempty"`
	Error          string               `json:"error,omitempty"`
	HTTPStatusCode *int                 `json:"http_status_code,omitempty"`
	RefreshedAt    *time.Time           `json:"refreshed_at,omitempty"`
	ExpiresAt      *time.Time           `json:"expires_at,omitempty"`
}

type usageSummaryCredentialPayload struct {
	ID           string                    `json:"id"`
	AuthIndex    string                    `json:"auth_index"`
	Name         string                    `json:"name"`
	Alias        *string                   `json:"alias"`
	DisplayName  string                    `json:"display_name"`
	AuthType     int                       `json:"auth_type"`
	AuthTypeName string                    `json:"auth_type_name"`
	Type         string                    `json:"type,omitempty"`
	Provider     string                    `json:"provider,omitempty"`
	Disabled     bool                      `json:"disabled"`
	Priority     *int                      `json:"priority,omitempty"`
	Quota        *usageSummaryQuotaPayload `json:"quota,omitempty"`
}

type usageSummaryResponse struct {
	GeneratedAt time.Time                       `json:"generated_at"`
	Timezone    string                          `json:"timezone"`
	Windows     []usageSummaryWindowPayload     `json:"windows,omitempty"`
	Credentials []usageSummaryCredentialPayload `json:"credentials,omitempty"`
}

// registerUsageSummaryRoute 注册 API-only 的窗口汇总端点。
// 参数全部经 Query Parameter 控制：windows 选窗口，credentials 选凭证；两个都不给时不输出任何数据段。
func registerUsageSummaryRoute(router gin.IRoutes, usageProvider service.UsageProvider, usageIdentityProvider service.UsageIdentityProvider, quotaProvider QuotaProvider) {
	router.GET("/usage/summary", func(c *gin.Context) {
		windowsRaw := strings.TrimSpace(c.Query("windows"))
		credentialsRaw := strings.TrimSpace(c.Query("credentials"))

		response := usageSummaryResponse{
			GeneratedAt: timeutil.NormalizeStorageTime(time.Now()),
			Timezone:    time.Local.String(),
		}
		// 两个参数都不给：什么都不出。
		if windowsRaw == "" && credentialsRaw == "" {
			c.JSON(http.StatusOK, response)
			return
		}

		if windowsRaw != "" {
			windows, labels, err := parseUsageSummaryWindows(windowsRaw)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if usageProvider == nil {
				writeInternalError(c, "usage provider is not configured", nil)
				return
			}
			rows, err := usageProvider.GetUsageSummaryWindows(c.Request.Context(), servicedto.UsageFilter{}, windows)
			if err != nil {
				// 数据源固定 cache；缓存不可用属于服务降级，必须显式告知而不是返回空窗口。
				if errors.Is(err, repository.ErrRecentUsageCacheUnavailable) {
					c.JSON(http.StatusServiceUnavailable, gin.H{"error": "recent usage event cache is unavailable"})
					return
				}
				writeInternalError(c, "get usage summary windows failed", err)
				return
			}
			response.Windows = buildUsageSummaryWindows(rows, labels)
		}

		if credentialsRaw != "" {
			all, tokens, err := parseUsageSummaryCredentials(credentialsRaw)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			credentials, err := buildUsageSummaryCredentials(c.Request.Context(), usageIdentityProvider, quotaProvider, all, tokens)
			if err != nil {
				writeInternalError(c, "get usage summary credentials failed", err)
				return
			}
			response.Credentials = credentials
		}

		c.JSON(http.StatusOK, response)
	})
}

// parseUsageSummaryWindows 解析逗号分隔的窗口列表：大小写不敏感、Go duration 语法，
// 只接受整秒且落在 [1s, 60m]；按秒数去重并保留首次出现的写法。
func parseUsageSummaryWindows(raw string) ([]time.Duration, []string, error) {
	segments := strings.Split(raw, ",")
	windows := make([]time.Duration, 0, len(segments))
	labels := make([]string, 0, len(segments))
	seen := make(map[int64]struct{}, len(segments))
	for _, segment := range segments {
		token := strings.ToLower(strings.TrimSpace(segment))
		if token == "" {
			continue
		}
		value, err := time.ParseDuration(token)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid window %q", strings.TrimSpace(segment))
		}
		if value%time.Second != 0 {
			return nil, nil, fmt.Errorf("window %q must be whole seconds", strings.TrimSpace(segment))
		}
		if value < usageSummaryMinWindow || value > usageSummaryMaxWindow {
			return nil, nil, fmt.Errorf("window %q must be between 1s and 60m", strings.TrimSpace(segment))
		}
		seconds := int64(value / time.Second)
		if _, exists := seen[seconds]; exists {
			continue
		}
		seen[seconds] = struct{}{}
		windows = append(windows, value)
		labels = append(labels, token)
	}
	if len(windows) == 0 {
		return nil, nil, fmt.Errorf("windows must not be empty")
	}
	return windows, labels, nil
}

// parseUsageSummaryCredentials 解析 all 或逗号分隔的凭证列表（auth_index 或 usage_identities.id），去重。
func parseUsageSummaryCredentials(raw string) (bool, []string, error) {
	if strings.EqualFold(strings.TrimSpace(raw), "all") {
		return true, nil, nil
	}
	segments := strings.Split(raw, ",")
	tokens := make([]string, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		token := strings.TrimSpace(segment)
		if token == "" {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		return false, nil, fmt.Errorf("credentials must be \"all\" or a comma-separated list of ids")
	}
	return false, tokens, nil
}

func buildUsageSummaryWindows(rows []servicedto.UsageSummaryWindow, labels []string) []usageSummaryWindowPayload {
	result := make([]usageSummaryWindowPayload, 0, len(rows))
	for index, row := range rows {
		label := ""
		if index < len(labels) {
			label = labels[index]
		}
		payload := usageSummaryWindowPayload{
			Window:              label,
			WindowSeconds:       row.WindowSeconds,
			WindowStart:         row.WindowStart,
			WindowEnd:           row.WindowEnd,
			Requests:            row.Requests,
			Failures:            row.Failures,
			TokenRequests:       row.TokenRequests,
			CacheHitRequests:    row.CacheHitRequests,
			TotalTokens:         row.TotalTokens,
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			ReasoningTokens:     row.ReasoningTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
			CostAvailable:       row.CostAvailable,
			CacheRate:           usageSummaryRate(row.CacheReadTokens, row.InputTokens),
			CacheHitRate:        usageSummaryRate(row.CacheHitRequests, row.TokenRequests),
		}
		if row.CostAvailable {
			cost := row.CostUSD
			payload.CostUSD = &cost
		}
		// 窗口长度恒为正（最小 1s），这里仍保留保护避免除零。
		if minutes := float64(row.WindowSeconds) / 60; minutes > 0 {
			payload.TPM = float64(row.TotalTokens) / minutes
			payload.RPM = float64(row.Requests) / minutes
		}
		result = append(result, payload)
	}
	return result
}

// usageSummaryRate 返回百分比（0-100）；分母非正时缺省，而不是返回 0 误导调用方。
func usageSummaryRate(numerator, denominator int64) *float64 {
	if denominator <= 0 {
		return nil
	}
	value := (float64(numerator) / float64(denominator)) * 100
	return &value
}

func buildUsageSummaryCredentials(ctx context.Context, identityProvider service.UsageIdentityProvider, quotaProvider QuotaProvider, all bool, tokens []string) ([]usageSummaryCredentialPayload, error) {
	if identityProvider == nil {
		return nil, fmt.Errorf("usage identity provider is not configured")
	}
	identities, err := identityProvider.ListUsageIdentities(ctx)
	if err != nil {
		return nil, err
	}

	selected := make([]entities.UsageIdentity, 0, len(identities))
	if all {
		selected = identities
	} else {
		wanted := make(map[string]struct{}, len(tokens))
		for _, token := range tokens {
			wanted[token] = struct{}{}
		}
		for _, identity := range identities {
			// 先按稳定的 auth_index 匹配，再兼容本地的自增 id。
			if _, ok := wanted[strings.TrimSpace(identity.Identity)]; ok {
				selected = append(selected, identity)
				continue
			}
			if _, ok := wanted[strconv.FormatInt(identity.ID, 10)]; ok {
				selected = append(selected, identity)
			}
		}
	}

	// 限额只读缓存：GetCachedQuota 不触发新的 provider 请求。
	quotaByIndex := map[string]*usageSummaryQuotaPayload{}
	if quotaProvider != nil && len(selected) > 0 {
		indexes := make([]string, 0, len(selected))
		for _, identity := range selected {
			if authIndex := strings.TrimSpace(identity.Identity); authIndex != "" {
				indexes = append(indexes, authIndex)
			}
		}
		if len(indexes) > 0 {
			response, err := quotaProvider.GetCachedQuota(ctx, quota.CacheRequest{AuthIndexes: indexes})
			if err != nil {
				return nil, err
			}
			for _, item := range response.Items {
				quotaByIndex[item.AuthIndex] = mapUsageSummaryQuotaItem(item)
			}
		}
	}

	result := make([]usageSummaryCredentialPayload, 0, len(selected))
	for _, identity := range selected {
		disabled := false
		if identity.Disabled != nil {
			disabled = *identity.Disabled
		}
		authIndex := strings.TrimSpace(identity.Identity)
		result = append(result, usageSummaryCredentialPayload{
			ID:           strconv.FormatInt(identity.ID, 10),
			AuthIndex:    authIndex,
			Name:         identity.Name,
			Alias:        identity.Alias,
			DisplayName:  helper.UsageIdentityDisplayName(identity),
			AuthType:     int(identity.AuthType),
			AuthTypeName: identity.AuthTypeName,
			Type:         identity.Type,
			Provider:     identity.Provider,
			Disabled:     disabled,
			Priority:     identity.Priority,
			// 没有缓存过额度的凭证仍然列出，只是不带 quota 字段。
			Quota: quotaByIndex[authIndex],
		})
	}
	return result, nil
}

// mapUsageSummaryQuotaItem 只暴露稳定展示字段，刻意不包含上游原始响应（可能含账号数据）。
func mapUsageSummaryQuotaItem(item quota.CachedQuotaItem) *usageSummaryQuotaPayload {
	return &usageSummaryQuotaPayload{
		Status:         string(item.Status),
		FileName:       item.FileName,
		Quota:          item.Quota,
		Error:          item.Error,
		HTTPStatusCode: item.HTTPStatusCode,
		RefreshedAt:    item.RefreshedAt,
		ExpiresAt:      item.ExpiresAt,
	}
}
