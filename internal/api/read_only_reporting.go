package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/ranking"
	rankingapi "cpa-usage-keeper/internal/ranking/httpapi"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
)

func listReadOnlyKeys(c *gin.Context, keys service.CPAAPIKeyProvider) ([]service.ReportingAPIKey, error) {
	if reader, ok := keys.(service.ReportingAPIKeyProvider); ok {
		return reader.ListReportingAPIKeys(c.Request.Context())
	}
	rows, err := keys.ListCPAAPIKeys(c.Request.Context())
	result := []service.ReportingAPIKey{}
	for _, row := range rows {
		result = append(result, service.ReportingAPIKey{ID: strconv.FormatInt(row.ID, 10), APIKey: row.APIKey, Historical: row.IsDeleted})
	}
	return result, err
}

func readOnlyKeyLabel(row service.ReportingAPIKey) string {
	if row.Historical {
		if strings.HasPrefix(row.ID, "historical:") {
			return "Historical key " + strings.TrimPrefix(row.ID, "historical:")[:12]
		}
		return "Historical key " + row.ID
	}
	return "Client key " + row.ID
}

func applyReadOnlyKeyFilter(c *gin.Context, filter *servicedto.UsageFilter, labels readOnlyDashboardLabels) bool {
	id := strings.TrimSpace(c.Query("api_key_id"))
	if id == "" {
		return true
	}
	if _, err := parseUsageAPIKeyID(id); err != nil && !regexp.MustCompile(`^historical:[a-f0-9]{24}$`).MatchString(id) {
		c.JSON(400, gin.H{"error": "invalid api_key_id"})
		return false
	}
	for raw, key := range labels.keys {
		if key.ID == id {
			if !strings.HasPrefix(id, "historical:") {
				filter.APIKeyID = id
			}
			filter.ReportingAPIGroupKey = raw
			return true
		}
	}
	c.JSON(404, gin.H{"error": "API key not found"})
	return false
}

func readOnlyProviderType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "codex", "claude", "gemini", "gemini-cli", "antigravity", "qwen", "iflow", "kimi", "xai", "openai", "openai-compatible":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "other"
	}
}

func safeReadOnlyIdentity(row entities.UsageIdentity, health *service.UsageCredentialHealthSnapshot, labels readOnlyDashboardLabels) usageIdentityResponse {
	// Sanitize the entity before the shared mapper, so future DTO fields cannot
	// accidentally reintroduce stored credential metadata.
	safe, label := labels.account(row.Identity)
	row.Name = label
	row.Alias = nil
	row.Identity = safe
	row.LookupKey = ""
	row.Prefix = ""
	row.BaseURL = ""
	row.FileName = nil
	row.FilePath = nil
	row.Note = nil
	row.AccountID = nil
	row.ProjectID = nil
	row.XAIUserID = nil
	row.Provider = viewerProviderName(row)
	row.Type = readOnlyProviderType(row.Type)
	row.Priority = nil
	row.PlanType = nil
	row.AuthTypeName = "API provider"
	if row.AuthType == entities.UsageIdentityAuthTypeAuthFile {
		row.AuthTypeName = "Auth file"
	}
	payload := mapUsageIdentityResponseWithHealth(row, health)
	payload.Subscription = nil
	return payload
}

func safeReadOnlyEvents(rows []servicedto.UsageEventRecord, labels readOnlyDashboardLabels) []usageEventPayload {
	result := make([]usageEventPayload, 0, len(rows))
	for _, row := range rows {
		labels.includeKey(row.APIGroupKey)
		key := labels.keys[row.APIGroupKey].Label
		account, label := labels.account(row.AuthIndex)
		result = append(result, usageEventPayload{ID: strconv.FormatInt(row.ID, 10), Timestamp: row.Timestamp.Format(time.RFC3339Nano), APIKey: key,
			Model: row.Model, ModelAlias: row.ModelAlias, ResponseModel: row.ResponseModel, ReasoningEffort: row.ReasoningEffort, ServiceTier: row.ServiceTier, ResponseServiceTier: row.ResponseServiceTier,
			Source: label, SourceRaw: account, Failed: row.Failed, StatusCode: row.StatusCode, Stream: row.Stream, LatencyMS: row.LatencyMS, TTFTMS: row.TTFTMS, SpeedTPS: usageEventSpeedTPS(row),
			Tokens: usageEventTokenPayload{InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens, CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, TotalTokens: row.TotalTokens}, CostUSD: row.CostUSD, CostAvailable: row.CostAvailable})
	}
	return result
}

func readOnlyEventsFilter(c *gin.Context, labels readOnlyDashboardLabels, identities service.UsageIdentityProvider) (servicedto.UsageFilter, bool) {
	request := readOnlyTimeRequest(c)
	query := request.URL.Query()
	query.Del("source")
	query.Del("auth_index")
	query.Del("auth_type")
	if query.Get("range") == "" {
		query.Set("range", "30d")
	}
	request.URL = &url.URL{RawQuery: query.Encode()}
	filter, err := parseUsageFilterQuery(request, time.Now())
	if err != nil {
		writeUsageFilterParseError(c, err)
		return filter, false
	}
	if !applyReadOnlyKeyFilter(c, &filter, labels) {
		return filter, false
	}
	source := strings.TrimSpace(c.Query("source"))
	if source != "" {
		if identities == nil {
			c.JSON(503, gin.H{"error": "Account data unavailable"})
			return filter, false
		}
		rows, err := identities.ListUsageIdentities(c.Request.Context())
		if err != nil {
			c.JSON(503, gin.H{"error": "Account data unavailable"})
			return filter, false
		}
		found := false
		for _, row := range rows {
			safe, _ := labels.account(row.Identity)
			if safe == source {
				filter.AuthIndex = row.Identity
				filter.AuthType = strconv.Itoa(int(row.AuthType))
				found = true
				break
			}
		}
		if !found {
			c.JSON(404, gin.H{"error": "Account not found"})
			return filter, false
		}
	}
	return filter, true
}

func registerReadOnlyReportingRoutes(router *gin.RouterGroup, usage service.UsageProvider, keys service.CPAAPIKeyProvider, identities service.UsageIdentityProvider, quotas QuotaProvider, local rankingapi.LocalProvider) {
	group := router.Group("/read-only")
	labelsFor := func(c *gin.Context) (readOnlyDashboardLabels, bool) {
		setNoStoreHeaders(c)
		labels, err := loadReadOnlyDashboardLabels(c, keys, identities)
		if err != nil {
			c.JSON(503, gin.H{"error": "Reporting data unavailable"})
			return labels, false
		}
		return labels, true
	}
	group.GET("/accounts", func(c *gin.Context) {
		labels, ok := labelsFor(c)
		if !ok {
			return
		}
		if identities == nil {
			c.JSON(503, gin.H{"error": "Account data unavailable"})
			return
		}
		req, ok := parseUsageIdentitiesPageRequest(c)
		if !ok {
			return
		}
		page, err := identities.ListActiveUsageIdentitiesPage(c.Request.Context(), req)
		if err != nil {
			c.JSON(503, gin.H{"error": "Account data unavailable"})
			return
		}
		items := []usageIdentityResponse{}
		for i, row := range page.Items {
			var health *service.UsageCredentialHealthSnapshot
			if i < len(page.CredentialHealth) {
				health = &page.CredentialHealth[i]
			}
			items = append(items, safeReadOnlyIdentity(row, health, labels))
		}
		counts := []usageIdentityTypeCount{}
		for _, row := range page.TypeCounts {
			// Custom provider types may contain administrator-supplied metadata.
			// Keep only canonical filter options; unknown types remain visible in All.
			if kind := readOnlyProviderType(row.Type); kind != "other" {
				counts = append(counts, usageIdentityTypeCount{Type: kind, Count: row.Count})
			}
		}
		c.JSON(200, usageIdentitiesPageResponse{Identities: items, TotalCount: page.Total, Page: req.Page, PageSize: req.PageSize, TotalPages: int((page.Total + int64(req.PageSize) - 1) / int64(req.PageSize)), TypeCounts: counts})
	})
	group.GET("/accounts/:id", func(c *gin.Context) {
		labels, ok := labelsFor(c)
		if !ok {
			return
		}
		reader, ok := identities.(service.UsageIdentityReader)
		if !ok {
			c.JSON(503, gin.H{"error": "Account data unavailable"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid account"})
			return
		}
		detail, err := reader.GetUsageIdentity(c.Request.Context(), id)
		if err != nil {
			c.JSON(404, gin.H{"error": "Account not found"})
			return
		}
		c.JSON(200, safeReadOnlyIdentity(detail.Identity, &detail.CredentialHealth, labels))
	})
	group.GET("/accounts/quota-cache", func(c *gin.Context) {
		labels, ok := labelsFor(c)
		if !ok {
			return
		}
		if identities == nil || quotas == nil {
			c.JSON(503, gin.H{"error": "Quota unavailable"})
			return
		}
		rows, err := identities.ListActiveUsageIdentities(c.Request.Context())
		if err != nil {
			c.JSON(503, gin.H{"error": "Quota unavailable"})
			return
		}
		indexes := []string{}
		for _, row := range rows {
			if row.AuthType == entities.UsageIdentityAuthTypeAuthFile {
				indexes = append(indexes, row.Identity)
			}
		}
		if len(indexes) == 0 {
			c.JSON(200, gin.H{"items": []gin.H{}})
			return
		}
		cache, err := quotas.GetCachedQuota(c.Request.Context(), quota.CacheRequest{AuthIndexes: indexes})
		if err != nil {
			c.JSON(503, gin.H{"error": "Quota unavailable"})
			return
		}
		items := []gin.H{}
		for _, item := range cache.Items {
			safe, _ := labels.account(item.AuthIndex)
			windows := []quota.QuotaRow{}
			if item.Quota != nil {
				for i, row := range item.Quota.Quota {
					windows = append(windows, quota.QuotaRow{Key: fmt.Sprintf("window-%d", i), Label: row.Label, Metric: row.Metric, Used: row.Used, Limit: row.Limit, Remaining: row.Remaining, UsedPercent: row.UsedPercent, RemainingFraction: row.RemainingFraction, Allowed: row.Allowed, LimitReached: row.LimitReached, Window: row.Window, CapturedAt: row.CapturedAt, Source: safeQuotaSource(row.Source), ResetAt: row.ResetAt, ResetAfterSeconds: row.ResetAfterSeconds})
				}
			}
			items = append(items, gin.H{"auth_index": safe, "status": item.Status, "refreshed_at": item.RefreshedAt, "quota": gin.H{"quota": windows}})
		}
		c.JSON(200, gin.H{"items": items})
	})
	group.GET("/events/filters", func(c *gin.Context) {
		labels, ok := labelsFor(c)
		if !ok {
			return
		}
		if usage == nil {
			c.JSON(503, gin.H{"error": "Usage unavailable"})
			return
		}
		filter, ok := readOnlyEventsFilter(c, labels, identities)
		if !ok {
			return
		}
		options, err := usage.ListUsageEventFilterOptions(c.Request.Context(), filter)
		if err != nil {
			c.JSON(503, gin.H{"error": "Usage unavailable"})
			return
		}
		sources := []usageSourceFilterOption{}
		if identities != nil {
			rows, err := identities.ListUsageIdentities(c.Request.Context())
			if err != nil {
				c.JSON(503, gin.H{"error": "Accounts unavailable"})
				return
			}
			for _, row := range rows {
				id, label := labels.account(row.Identity)
				sources = append(sources, usageSourceFilterOption{Value: id, Label: label, DisplayName: label})
			}
		}
		models := []string{}
		if options != nil {
			models = options.Models
		}
		c.JSON(200, usageEventFilterOptionsResponse{Models: models, Sources: sources})
	})
	group.GET("/events", func(c *gin.Context) {
		labels, ok := labelsFor(c)
		if !ok {
			return
		}
		if usage == nil {
			c.JSON(503, gin.H{"error": "Usage unavailable"})
			return
		}
		filter, ok := readOnlyEventsFilter(c, labels, identities)
		if !ok {
			return
		}
		page, err := usage.ListUsageEvents(c.Request.Context(), filter)
		if err != nil {
			c.JSON(503, gin.H{"error": "Usage unavailable"})
			return
		}
		next := ""
		if page.HasMore && len(page.Events) > 0 {
			last := page.Events[len(page.Events)-1]
			next = encodeUsageEventsCursor(last.Timestamp, last.ID)
		}
		c.JSON(200, usageEventsResponse{Events: safeReadOnlyEvents(page.Events, labels), TotalCount: page.TotalCount, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages, HasMore: page.HasMore, NextCursor: next})
	})
	slots := make(chan struct{}, 2)
	group.GET("/events/export", func(c *gin.Context) {
		labels, ok := labelsFor(c)
		if !ok {
			return
		}
		if usage == nil {
			c.JSON(503, gin.H{"error": "Usage unavailable"})
			return
		}
		filter, ok := readOnlyEventsFilter(c, labels, identities)
		if !ok {
			return
		}
		format := c.DefaultQuery("format", "csv")
		if format != "csv" && format != "json" {
			c.JSON(400, gin.H{"error": "Invalid export format"})
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			c.JSON(429, gin.H{"error": "Export capacity full"})
			return
		}
		output, err := os.CreateTemp("", "keeper-reporting-*")
		if err != nil {
			c.JSON(503, gin.H{"error": "Export unavailable"})
			return
		}
		defer os.Remove(output.Name())
		defer output.Close()
		contentType := "text/csv; charset=utf-8"

		if format == "json" {
			contentType = "application/json"
			if _, err = output.WriteString("["); err != nil {
				c.JSON(503, gin.H{"error": "Export unavailable"})
				return
			}
			first := true
			err = usage.StreamUsageEvents(c.Request.Context(), filter, func(row servicedto.UsageEventRecord) error {
				if !first {
					if _, err := output.WriteString(","); err != nil {
						return err
					}
				}
				first = false
				return json.NewEncoder(output).Encode(safeReadOnlyEvents([]servicedto.UsageEventRecord{row}, labels)[0])
			})
			if err == nil {
				_, err = output.WriteString("]")
			}
		} else {
			writer := csv.NewWriter(output)
			if err = writer.Write([]string{"timestamp", "api_key", "account", "model", "model_alias", "failed", "status_code", "input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens", "cost_usd", "cost_available", "latency_ms", "ttft_ms"}); err != nil {
				c.JSON(503, gin.H{"error": "Export unavailable"})
				return
			}
			err = usage.StreamUsageEvents(c.Request.Context(), filter, func(row servicedto.UsageEventRecord) error {
				safe := safeReadOnlyEvents([]servicedto.UsageEventRecord{row}, labels)[0]
				status, ttft := "", ""
				if safe.StatusCode != nil {
					status = strconv.Itoa(*safe.StatusCode)
				}
				if safe.TTFTMS != nil {
					ttft = fmt.Sprint(*safe.TTFTMS)
				}
				return writer.Write([]string{safe.Timestamp, safeCSVValue(safe.APIKey), safeCSVValue(safe.Source), safeCSVValue(safe.Model), safeCSVValue(safe.ModelAlias), fmt.Sprint(safe.Failed), status, fmt.Sprint(safe.Tokens.InputTokens), fmt.Sprint(safe.Tokens.OutputTokens), fmt.Sprint(safe.Tokens.ReasoningTokens), fmt.Sprint(safe.Tokens.CacheReadTokens), fmt.Sprint(safe.Tokens.CacheCreationTokens), fmt.Sprint(safe.Tokens.TotalTokens), fmt.Sprint(safe.CostUSD), fmt.Sprint(safe.CostAvailable), fmt.Sprint(safe.LatencyMS), ttft})
			})
			writer.Flush()
			if err == nil {
				err = writer.Error()
			}
		}
		if err != nil {
			c.JSON(503, gin.H{"error": "Export unavailable"})
			return
		}
		info, err := output.Stat()
		if err != nil {
			c.JSON(503, gin.H{"error": "Export unavailable"})
			return
		}
		if _, err = output.Seek(0, 0); err != nil {
			c.JSON(503, gin.H{"error": "Export unavailable"})
			return
		}
		c.Header("Content-Disposition", `attachment; filename="keeper-reporting.`+format+`"`)
		c.DataFromReader(200, info.Size(), contentType, output, nil)
	})
	group.GET("/ranking/local/leaderboards", func(c *gin.Context) {
		labels, ok := labelsFor(c)
		if !ok {
			return
		}
		if local == nil {
			c.JSON(503, gin.H{"error": "Local ranking unavailable"})
			return
		}
		period := ranking.LeaderboardPeriod(c.Query("period"))
		metric := ranking.LeaderboardMetric(c.Query("metric"))
		if !readOnlyRankingSelection(period, metric) {
			c.JSON(400, gin.H{"error": "Invalid leaderboard selection"})
			return
		}
		board, err := local.Leaderboard(c.Request.Context(), period, metric)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid leaderboard selection"})
			return
		}
		safe := board
		safe.Entries = append([]ranking.LeaderboardEntry{}, board.Entries...)
		safe.ScoreExplanation = nil
		for i := range safe.Entries {
			entry := &safe.Entries[i]
			id := strings.TrimPrefix(entry.ParticipantID, "key:")
			label := labels.keyLabels[id]
			if label == "" {
				_, label = labels.key(entry.ParticipantID)
			}
			entry.DisplayName = label
			entry.KeyAlias = ""
		}
		c.JSON(http.StatusOK, safe)
	})
}

func safeCSVValue(value string) string {
	if strings.ContainsAny(strings.TrimLeft(value, " \t\r\n")[:min(1, len(strings.TrimLeft(value, " \t\r\n")))], "=+-@") {
		return "'" + value
	}
	return value
}

func readOnlyRankingSelection(period ranking.LeaderboardPeriod, metric ranking.LeaderboardMetric) bool {
	switch period {
	case ranking.LeaderboardToday, ranking.LeaderboardYesterday, ranking.LeaderboardCurrentMonth, ranking.LeaderboardPreviousMonth:
	default:
		return false
	}
	switch metric {
	case ranking.MetricOverall, ranking.MetricTotalTokens, ranking.MetricRequestCount, ranking.MetricCacheReadRate, ranking.MetricTTFTAverage, ranking.MetricLatencyAverage, ranking.MetricPeakTPM, ranking.MetricPeakRPM:
		return true
	}
	return false
}
