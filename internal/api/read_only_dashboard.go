package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
)

// The read-only dashboard uses the same statistical DTOs as the existing
// dashboard, but never serializes credential names or masked key fragments.
type readOnlyDashboardLabels struct {
	keys      map[string]analysisAPIKeyInfo
	keyLabels map[string]string
	accounts  map[string]string
}

func loadReadOnlyDashboardLabels(c *gin.Context, keys service.CPAAPIKeyProvider, identities service.UsageIdentityProvider) (readOnlyDashboardLabels, error) {
	result := readOnlyDashboardLabels{keys: map[string]analysisAPIKeyInfo{}, keyLabels: map[string]string{}, accounts: map[string]string{}}
	if keys != nil {
		rows, err := listReadOnlyKeys(c, keys)
		if err != nil {
			return result, err
		}
		for _, row := range rows {
			id, label := row.ID, readOnlyKeyLabel(row)
			result.keys[row.APIKey] = analysisAPIKeyInfo{ID: id, Label: label}
			result.keyLabels[id] = label
		}
	}
	if identities != nil {
		rows, err := identities.ListActiveUsageIdentities(c.Request.Context())
		if err != nil {
			return result, err
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		counts := map[string]int{}
		for _, row := range rows {
			if row.IsDeleted {
				continue
			}
			name := viewerProviderName(row)
			if row.Disabled != nil && *row.Disabled {
				result.accounts[row.Identity] = fmt.Sprintf("%s account %d (disabled)", name, row.ID)
				continue
			}
			counts[name]++
			result.accounts[row.Identity] = fmt.Sprintf("%s account %d", name, counts[name])
		}
	}
	return result, nil
}

func (labels readOnlyDashboardLabels) key(key string) (string, string) {
	if label, ok := labels.keyLabels[key]; ok {
		return key, label
	}
	// Some existing builders already hash historical keys. Hashing their opaque
	// identifier again is safe and keeps all pages consistent with one another.
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("historical:%x", digest[:12]), fmt.Sprintf("Historical key %x", digest[:6])
}

func (labels readOnlyDashboardLabels) includeKey(raw string) {
	if _, ok := labels.keys[raw]; ok {
		return
	}
	digest := sha256.Sum256([]byte(raw))
	id, label := fmt.Sprintf("historical:%x", digest[:12]), fmt.Sprintf("Historical key %x", digest[:6])
	labels.keys[raw] = analysisAPIKeyInfo{ID: id, Label: label}
	labels.keyLabels[id] = label
}

func (labels readOnlyDashboardLabels) account(key string) (string, string) {
	digest := sha256.Sum256([]byte(key))
	label := labels.accounts[key]
	if label == "" {
		label = fmt.Sprintf("Historical account %x", digest[:6])
	}
	return fmt.Sprintf("account:%x", digest[:12]), label
}

func sanitizeReadOnlyComparisons(payload *usageOverviewComparisons, labels readOnlyDashboardLabels) {
	for i := range payload.APIKeys {
		payload.APIKeys[i].Key, payload.APIKeys[i].Label = labels.key(payload.APIKeys[i].Key)
	}
	for _, items := range [][]usageOverviewComparisonItem{payload.AuthFiles, payload.AIProviders} {
		for i := range items {
			items[i].Key, items[i].Label = labels.account(items[i].Key)
		}
	}
}

func sanitizeReadOnlyAnalysis(payload *analysisResponse, labels readOnlyDashboardLabels, snapshot *servicedto.AnalysisSnapshot) {
	for i := range payload.APIKeyComposition {
		payload.APIKeyComposition[i].Key, payload.APIKeyComposition[i].Label = labels.key(payload.APIKeyComposition[i].Key)
	}
	if snapshot != nil {
		for i := range payload.AuthFilesComposition {
			payload.AuthFilesComposition[i].Key, payload.AuthFilesComposition[i].Label = labels.account(snapshot.AuthFilesComposition[i].Key)
		}
		for i := range payload.AIProviderComposition {
			payload.AIProviderComposition[i].Key, payload.AIProviderComposition[i].Label = labels.account(snapshot.AIProviderComposition[i].Key)
		}
	}
	safeLabels := map[string]string{}
	for i, key := range payload.Heatmap.APIKeys {
		safe, label := labels.key(key)
		payload.Heatmap.APIKeys[i] = safe
		safeLabels[safe] = label
	}
	payload.Heatmap.APIKeyLabels = safeLabels
	for i := range payload.Heatmap.Cells {
		payload.Heatmap.Cells[i].APIKey, _ = labels.key(payload.Heatmap.Cells[i].APIKey)
	}
}

func sanitizeReadOnlyRealtime(payload *usageOverviewRealtime, labels readOnlyDashboardLabels) {
	for i := range payload.CurrentUsage.APIKeys {
		payload.CurrentUsage.APIKeys[i].Key, payload.CurrentUsage.APIKeys[i].Label = labels.key(payload.CurrentUsage.APIKeys[i].Key)
	}
	for _, items := range [][]usageOverviewRealtimeUsageTopItem{payload.CurrentUsage.AuthFiles, payload.CurrentUsage.AIProviders} {
		for i := range items {
			items[i].Key, items[i].Label = labels.account(items[i].Key)
		}
	}
}

func readOnlyTimeRequest(c *gin.Context) *http.Request {
	request := c.Request.Clone(c.Request.Context())
	query := c.Request.URL.Query()
	query.Del("api_key_id")
	request.URL = &url.URL{RawQuery: query.Encode()}
	return request
}

func registerReadOnlyDashboardRoutes(router *gin.RouterGroup, provider service.UsageProvider, keys service.CPAAPIKeyProvider, identities service.UsageIdentityProvider) {
	// Only explicit GET routes are registered. Admin middleware/routes remain
	// unchanged, so this role cannot reach a write even via the LAN listener.
	group := router.Group("/read-only")
	group.GET("/keys", func(c *gin.Context) {
		setNoStoreHeaders(c)
		options := []gin.H{}
		if keys == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage data is unavailable"})
			return
		}
		rows, err := listReadOnlyKeys(c, keys)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage data is unavailable"})
			return
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		for _, row := range rows {
			options = append(options, gin.H{"id": row.ID, "label": readOnlyKeyLabel(row)})
		}
		c.JSON(http.StatusOK, gin.H{"keys": options})
	})
	for _, path := range []string{"/key-overview", "/key-overview/comparisons", "/key-overview/realtime", "/key-activity", "/key-analysis", "/key-analysis/latency"} {
		route := path
		group.GET(route, func(c *gin.Context) {
			setNoStoreHeaders(c)
			unavailable := func() { c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Usage data is unavailable"}) }
			if provider == nil {
				unavailable()
				return
			}
			now := time.Now()
			var filter servicedto.UsageFilter
			var err error
			switch route {
			case "/key-overview/realtime":
				filter, err = parseKeyUsageRealtimeFilterQuery(readOnlyTimeRequest(c), now)
			case "/key-activity":
				filter, err = parseKeyUsageActivityFilterQuery(readOnlyTimeRequest(c), now)
			case "/key-analysis", "/key-analysis/latency":
				filter, err = parseKeyUsageAnalysisTimeFilterQuery(readOnlyTimeRequest(c), now)
			default:
				filter, err = parseKeyUsageOverviewTimeFilterQuery(readOnlyTimeRequest(c), now)
			}
			if err != nil {
				writeUsageFilterParseError(c, err)
				return
			}
			// Only this reporting role accepts an optional key scope. Individual
			// key viewers continue to receive their session scope on separate routes.

			labels, err := loadReadOnlyDashboardLabels(c, keys, identities)
			if err != nil {
				unavailable()
				return
			}
			if !applyReadOnlyKeyFilter(c, &filter, labels) {
				return
			}
			switch route {
			case "/key-overview":
				snapshot, err := provider.GetUsageOverview(c.Request.Context(), filter)
				if err != nil {
					unavailable()
					return
				}
				payload := usageOverviewResponse{Usage: buildUsageOverviewPayload(nil), Summary: buildUsageOverviewSummary(snapshot), Series: buildUsageOverviewSeries(snapshot), Timezone: time.Local.String()}
				if snapshot != nil {
					payload.Usage = buildUsageOverviewPayload(snapshot.Usage)
				}
				c.JSON(http.StatusOK, payload)
			case "/key-overview/comparisons":
				comparisons, ok := provider.(service.UsageComparisonProvider)
				if !ok {
					unavailable()
					return
				}
				snapshot, err := comparisons.GetUsageOverviewComparisons(c.Request.Context(), filter)
				if err != nil {
					unavailable()
					return
				}
				if snapshot != nil && snapshot.Comparisons != nil {
					for raw := range snapshot.Comparisons.APIKeys {
						labels.includeKey(raw)
					}
				}
				payload := buildUsageOverviewComparisons(snapshot, labels.keys)
				sanitizeReadOnlyComparisons(payload, labels)
				c.JSON(http.StatusOK, payload)
			case "/key-overview/realtime":
				snapshot, err := provider.GetUsageOverviewRealtime(c.Request.Context(), filter)
				if err != nil {
					unavailable()
					return
				}
				if snapshot != nil {
					for _, item := range snapshot.CurrentUsage.APIKeys {
						labels.includeKey(item.Key)
					}
				}
				payload := buildUsageOverviewRealtime(snapshot, filter.RealtimeWindow, labels.keys)
				sanitizeReadOnlyRealtime(&payload, labels)
				c.JSON(http.StatusOK, payload)
			case "/key-activity":
				snapshot, err := provider.GetUsageActivity(c.Request.Context(), filter)
				if err != nil {
					unavailable()
					return
				}
				c.JSON(http.StatusOK, buildUsageActivityResponse(snapshot))
			case "/key-analysis":
				snapshot, err := provider.GetAnalysis(c.Request.Context(), filter)
				if err != nil {
					unavailable()
					return
				}
				if snapshot != nil {
					for _, item := range snapshot.APIKeyComposition {
						labels.includeKey(item.Key)
					}
					for _, item := range snapshot.Heatmap {
						labels.includeKey(item.APIKey)
					}
				}
				payload := buildAnalysisPayload(snapshot, labels.keys)
				sanitizeReadOnlyAnalysis(&payload, labels, snapshot)
				c.JSON(http.StatusOK, payload)
			case "/key-analysis/latency":
				snapshot, err := provider.GetAnalysisLatency(c.Request.Context(), filter)
				if err != nil {
					unavailable()
					return
				}
				if snapshot == nil {
					c.JSON(http.StatusOK, emptyAnalysisLatencyDiagnosticsResponse())
					return
				}
				c.JSON(http.StatusOK, buildAnalysisLatencyDiagnosticsPayload(*snapshot))
			}
		})
	}
}
