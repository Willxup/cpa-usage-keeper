package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

func (h *authHandler) readOnlyEnabled() bool {
	return h != nil && h.config.Enabled && len(h.config.ReadOnlyPassword) >= 16 &&
		h.config.ReadOnlyPassword != h.config.LoginPassword
}

func (h *authHandler) readOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.readOnlyEnabled() {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		h.roleMiddleware(auth.RoleReadOnly, auth.RoleAdmin)(c)
	}
}

func (h *authHandler) readOnlyLogin(c *gin.Context) {
	if !h.readOnlyEnabled() || h.sessions == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	clientKey := loginClientKey(c)
	if !h.allowLoginAttempt(c, clientKey) {
		return
	}
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		if isRequestEntityTooLarge(err) {
			writeRequestEntityTooLarge(c)
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	expected := sha256.Sum256([]byte(h.config.ReadOnlyPassword))
	actual := sha256.Sum256([]byte(request.Password))
	if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	h.loginAttempts.Reset(clientKey)
	resolved := resolveSessionToken(c)
	token, expiresAt, err := h.sessions.CreateReadOnlyWithSourceAndMetadata(resolved.Source, sessionClientMetadata(c))
	if err != nil {
		writeInternalError(c, "create read-only session failed", err)
		return
	}
	setSessionCookie(c, h.config.BasePath, resolved.CookieKind, token, expiresAt, rememberLogin(request.RememberMe))
	writeLoginSuccess(c, resolved, token)
}

type readOnlyKeyUsage struct {
	Label       string   `json:"label"`
	Requests    int64    `json:"requests"`
	Failures    int64    `json:"failures"`
	TotalTokens int64    `json:"total_tokens"`
	Cost        *float64 `json:"cost"`
}

func registerReadOnlyOverviewRoute(router gin.IRoutes, provider service.UsageProvider, keys service.CPAAPIKeyProvider) {
	router.GET("/read-only/overview", func(c *gin.Context) {
		setNoStoreHeaders(c)
		comparisons, ok := provider.(service.UsageComparisonProvider)
		if provider == nil || !ok || keys == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage data is unavailable"})
			return
		}
		// Accept only bounded time ranges. Identity, API-key, model and custom
		// range parameters cannot widen the reporting surface or reveal metadata.
		period := c.DefaultQuery("range", "7d")
		if period != "24h" && period != "7d" && period != "30d" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Choose 24h, 7d or 30d"})
			return
		}
		request := c.Request.Clone(c.Request.Context())
		request.URL = &url.URL{RawQuery: url.Values{"range": {period}}.Encode()}
		filter, err := parseKeyUsageOverviewTimeFilterQuery(request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid time range"})
			return
		}
		snapshot, err := provider.GetUsageOverview(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage data is unavailable"})
			return
		}
		grouped, err := comparisons.GetUsageOverviewComparisons(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage data is unavailable"})
			return
		}
		active, err := keys.ListCPAAPIKeys(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage data is unavailable"})
			return
		}
		labels := map[string]string{}
		// All labels are generated server-side. Raw keys, key fragments, provider
		// credential names and user-entered aliases never enter this response.
		for _, key := range active {
			labels[key.APIKey] = fmt.Sprintf("Client key %d", key.ID)
		}
		rows := map[string]readOnlyKeyUsage{}
		for raw, label := range labels {
			rows[raw] = readOnlyKeyUsage{Label: label}
		}
		if grouped != nil && grouped.Comparisons != nil {
			for raw, item := range grouped.Comparisons.APIKeys {
				if item == nil {
					continue
				}
				label := labels[raw]
				if label == "" {
					digest := sha256.Sum256([]byte(raw))
					label = fmt.Sprintf("Historical key %x", digest[:6])
				}
				row := readOnlyKeyUsage{Label: label, Requests: item.Requests, Failures: item.Failures, TotalTokens: item.TotalTokens}
				if item.CostAvailable {
					value := item.CostUSD
					row.Cost = &value
				}
				rows[raw] = row
			}
		}
		result := make([]readOnlyKeyUsage, 0, len(rows))
		for _, row := range rows {
			result = append(result, row)
		}
		sort.Slice(result, func(i, j int) bool {
			if result[i].Requests == result[j].Requests {
				return result[i].Label < result[j].Label
			}
			return result[i].Requests > result[j].Requests
		})
		overview := usageOverviewResponse{Usage: buildUsageOverviewPayload(nil), Summary: buildUsageOverviewSummary(snapshot), Series: buildUsageOverviewSeries(snapshot), Timezone: time.Local.String()}
		if snapshot != nil {
			overview.Usage = buildUsageOverviewPayload(snapshot.Usage)
		}
		c.JSON(http.StatusOK, gin.H{"overview": overview, "keys": result})
	})
}
