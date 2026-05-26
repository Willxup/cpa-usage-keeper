package api

import (
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/gin-gonic/gin"
)

func registerQuotaCycleRoutes(router gin.IRoutes, provider service.CycleCostProvider) {
	if provider == nil {
		return
	}

	router.GET("/quota/cycles/providers", func(c *gin.Context) {
		summaries, err := provider.ListProviders(c.Request.Context())
		if err != nil {
			writeInternalError(c, "quota cycle provider list failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": summaries})
	})

	router.GET("/quota/cycles/current", func(c *gin.Context) {
		providerName := normalizeProviderQuery(c.Query("provider"))
		summaries, err := provider.GetCurrentCycles(c.Request.Context(), providerName)
		if err != nil {
			writeInternalError(c, "quota cycle current lookup failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"provider": providerName,
			"items":    summaries,
		})
	})

	router.GET("/quota/cycles/history", func(c *gin.Context) {
		providerName := normalizeProviderQuery(c.Query("provider"))
		if providerName == "" {
			providerName = "codex"
		}
		authIndex := strings.TrimSpace(c.Query("auth_index"))
		if authIndex == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			return
		}
		limit := parsePositiveQueryInt(c, "limit", 12, 60)
		summaries, err := provider.GetHistoricalCycles(c.Request.Context(), providerName, authIndex, limit)
		if err != nil {
			writeInternalError(c, "quota cycle history lookup failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"provider":  providerName,
			"authIndex": authIndex,
			"items":     summaries,
		})
	})

	router.GET("/quota/cycles/breakdown", func(c *gin.Context) {
		providerName := normalizeProviderQuery(c.Query("provider"))
		if providerName == "" {
			providerName = "codex"
		}
		authIndex := strings.TrimSpace(c.Query("auth_index"))
		if authIndex == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			return
		}
		cycleEndRaw := strings.TrimSpace(c.Query("cycle_end"))
		if cycleEndRaw == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cycle_end is required"})
			return
		}
		cycleEnd, ok := parseCycleEnd(cycleEndRaw)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cycle_end is not a valid timestamp"})
			return
		}
		breakdown, err := provider.GetCycleBreakdown(c.Request.Context(), providerName, authIndex, cycleEnd)
		if err != nil {
			writeInternalError(c, "quota cycle breakdown lookup failed", err)
			return
		}
		c.JSON(http.StatusOK, breakdown)
	})
}

// normalizeProviderQuery 把 "" / "all" / 大写都归一化成空字符串 (= 全部 provider).
func normalizeProviderQuery(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" || value == "all" || value == "*" {
		return ""
	}
	return value
}

func parseCycleEnd(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := timeutil.ParseStorageTime(raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func parsePositiveQueryInt(c *gin.Context, key string, defaultValue, maxValue int) int {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return defaultValue
	}
	parsed := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return defaultValue
		}
		parsed = parsed*10 + int(r-'0')
		if parsed > maxValue {
			return maxValue
		}
	}
	if parsed <= 0 {
		return defaultValue
	}
	return parsed
}
