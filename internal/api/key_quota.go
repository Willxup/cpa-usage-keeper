package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

// Deliberately separate DTOs: never serialize identities, raw provider payloads,
// auth indexes, upstream errors, reset credits or local billing statistics.
type viewerQuotaRow struct {
	Label            string   `json:"label"`
	Metric           string   `json:"metric,omitempty"`
	Remaining        *float64 `json:"remaining,omitempty"`
	Limit            *float64 `json:"limit,omitempty"`
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	ResetAt          string   `json:"reset_at,omitempty"`
	LimitReached     *bool    `json:"limit_reached,omitempty"`
}
type viewerQuotaAccount struct {
	Label     string           `json:"label"`
	Provider  string           `json:"provider"`
	Status    string           `json:"status"`
	UpdatedAt *time.Time       `json:"updated_at,omitempty"`
	Rows      []viewerQuotaRow `json:"rows"`
}

func viewerProviderName(identity entities.UsageIdentity) string {
	// Do not fall back to user-supplied names, URLs or credential labels.
	switch strings.ToLower(strings.TrimSpace(identity.Type)) {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude"
	case "gemini", "gemini-cli":
		return "Gemini"
	case "antigravity":
		return "Antigravity"
	case "qwen":
		return "Qwen"
	case "iflow":
		return "iFlow"
	case "kimi":
		return "Kimi"
	case "xai":
		return "xAI"
	default:
		return "Other provider"
	}
}

func registerKeyQuotaRoute(router gin.IRoutes, identities service.UsageIdentityProvider, provider QuotaProvider) {
	registerViewerQuotaRoute(router, "/key-quota", identities, provider)
}

func registerViewerQuotaRoute(router gin.IRoutes, path string, identities service.UsageIdentityProvider, provider QuotaProvider) {
	router.GET(path, func(c *gin.Context) {
		setNoStoreHeaders(c)
		if identities == nil || provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Quota data is unavailable"})
			return
		}
		// Account selection is always server-side. Query parameters cannot select
		// credentials or expand access beyond active, enabled configured accounts.
		active, err := identities.ListActiveUsageIdentities(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Quota data is unavailable"})
			return
		}
		sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
		selected := make([]entities.UsageIdentity, 0, len(active))
		indexes := make([]string, 0, len(active))
		for _, identity := range active {
			if identity.IsDeleted || (identity.Disabled != nil && *identity.Disabled) {
				continue
			}
			selected = append(selected, identity)
			if identity.AuthType == entities.UsageIdentityAuthTypeAuthFile && identity.Identity != "" {
				indexes = append(indexes, identity.Identity)
			}
		}
		cache := quota.CacheResponse{}
		if len(indexes) > 0 {
			cache, err = provider.GetCachedQuota(c.Request.Context(), quota.CacheRequest{AuthIndexes: indexes})
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Quota data is unavailable"})
				return
			}
		}
		byIndex := make(map[string]quota.CachedQuotaItem, len(cache.Items))
		for _, item := range cache.Items {
			byIndex[item.AuthIndex] = item
		}
		accounts := make([]viewerQuotaAccount, 0, len(selected))
		counts := make(map[string]int)
		now := time.Now()
		for _, identity := range selected {
			name := viewerProviderName(identity)
			counts[name]++
			account := viewerQuotaAccount{Label: fmt.Sprintf("%s account %d", name, counts[name]), Provider: name, Status: "unavailable", Rows: []viewerQuotaRow{}}
			item, ok := byIndex[identity.Identity]
			if identity.AuthType == entities.UsageIdentityAuthTypeAuthFile && ok && item.Status == quota.RefreshTaskStatusCompleted && item.Quota != nil && len(item.Quota.Quota) > 0 {
				account.UpdatedAt = item.RefreshedAt
				account.Status = "available"
				if item.RefreshedAt == nil || now.Sub(*item.RefreshedAt) > 15*time.Minute {
					account.Status = "stale"
				}
				for _, row := range item.Quota.Quota {
					// Labels describe normalized quota windows/models, never account identity.
					label := row.Label
					if label == "" {
						label = "Quota window"
					}
					safe := viewerQuotaRow{Label: label, Metric: row.Metric, Remaining: row.Remaining, Limit: row.Limit, ResetAt: row.ResetAt, LimitReached: row.LimitReached}
					if row.RemainingFraction != nil {
						value := *row.RemainingFraction * 100
						safe.RemainingPercent = &value
					} else if row.UsedPercent != nil {
						value := 100 - *row.UsedPercent
						safe.RemainingPercent = &value
					}
					if safe.ResetAt == "" && row.ResetAfterSeconds != nil && item.RefreshedAt != nil {
						safe.ResetAt = item.RefreshedAt.Add(time.Duration(*row.ResetAfterSeconds) * time.Second).UTC().Format(time.RFC3339)
					}
					if reset, err := time.Parse(time.RFC3339, safe.ResetAt); err == nil && !reset.After(now) {
						account.Status = "stale"
					}
					account.Rows = append(account.Rows, safe)
				}
			}
			accounts = append(accounts, account)
		}
		c.JSON(http.StatusOK, gin.H{"accounts": accounts})
	})
}
