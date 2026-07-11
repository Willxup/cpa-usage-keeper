package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/accessscope"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type usageEventsResponse struct {
	Events     []usageEventPayload `json:"events"`
	TotalCount int64               `json:"total_count"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	TotalPages int                 `json:"total_pages"`
}

type usageSourceFilterOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	DisplayName string `json:"displayName"`
}

type usageEventFilterOptionsResponse struct {
	Models  []string                  `json:"models"`
	Sources []usageSourceFilterOption `json:"sources"`
}

type usageEventPayload struct {
	ID              string                 `json:"id,omitempty"`
	Timestamp       string                 `json:"timestamp"`
	APIKey          string                 `json:"api_key,omitempty"`
	Model           string                 `json:"model"`
	ModelAlias      string                 `json:"model_alias,omitempty"`
	ReasoningEffort string                 `json:"reasoning_effort,omitempty"`
	ServiceTier     string                 `json:"service_tier,omitempty"`
	ExecutorType    string                 `json:"executor_type,omitempty"`
	Endpoint        string                 `json:"endpoint,omitempty"`
	Source          string                 `json:"source"`
	SourceRaw       string                 `json:"source_raw,omitempty"`
	SourceType      string                 `json:"source_type,omitempty"`
	AuthIndex       string                 `json:"auth_index,omitempty"`
	RequestID       string                 `json:"request_id,omitempty"`
	IsDelete        bool                   `json:"isDelete,omitempty"`
	Failed          bool                   `json:"failed"`
	LatencyMS       int64                  `json:"latency_ms"`
	TTFTMS          *int64                 `json:"ttft_ms,omitempty"`
	SpeedTPS        *float64               `json:"speed_tps,omitempty"`
	Tokens          usageEventTokenPayload `json:"tokens"`
	CostUSD         float64                `json:"cost_usd"`
	CostAvailable   bool                   `json:"cost_available"`
	PricingStyle    string                 `json:"pricing_style,omitempty"`
}

type usageEventTokenPayload struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

type usageEventRequestLogPayload struct {
	EventID      string                        `json:"event_id"`
	RequestID    string                        `json:"request_id,omitempty"`
	Filename     string                        `json:"filename,omitempty"`
	Available    bool                          `json:"available"`
	Previewable  bool                          `json:"previewable"`
	TooLarge     bool                          `json:"too_large,omitempty"`
	Downloadable bool                          `json:"downloadable,omitempty"`
	Sections     []usageEventRequestLogSection `json:"sections"`
}

type usageEventRequestLogSection struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type usageEventRequestLogDownloadTokenPayload struct {
	DownloadURL string `json:"download_url"`
}

type usageEventExportPayload struct {
	ID                  string   `json:"id"`
	Timestamp           string   `json:"timestamp"`
	APIKey              string   `json:"api_key"`
	CPAAPIKeyID         string   `json:"cpa_api_key_id"`
	Source              string   `json:"source"`
	SourceType          string   `json:"source_type"`
	AuthIndex           string   `json:"auth_index"`
	IsIdentityDeleted   bool     `json:"is_identity_deleted"`
	Model               string   `json:"model"`
	ModelAlias          string   `json:"model_alias"`
	ReasoningEffort     string   `json:"reasoning_effort"`
	ServiceTier         string   `json:"service_tier"`
	ExecutorType        string   `json:"executor_type"`
	Result              string   `json:"result"`
	Endpoint            string   `json:"endpoint"`
	TTFTMS              *int64   `json:"ttft_ms"`
	LatencyMS           int64    `json:"latency_ms"`
	SpeedTPS            *float64 `json:"speed_tps"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	ReasoningTokens     int64    `json:"reasoning_tokens"`
	CachedTokens        int64    `json:"cached_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	CacheRate           *float64 `json:"cache_rate"`
	TotalTokens         int64    `json:"total_tokens"`
	CostUSD             float64  `json:"cost_usd"`
}

type usageEventStreamFunc func(func(servicedto.UsageEventRecord) error) error

func registerUsageEventsRoute(
	router gin.IRoutes,
	usageProvider service.UsageProvider,
	usageIdentityProvider service.UsageIdentityProvider,
	cpaAPIKeyProvider service.CPAAPIKeyProvider,
	requestLogProvider service.RequestLogProvider,
	requestLogDownloadTokens *requestLogDownloadTokenStore,
	requestLogAccessEnabled bool,
) {
	router.GET("/usage/events/filters/models", func(c *gin.Context) {
		models, err := loadUsageEventModelFilterOptions(c, usageProvider)
		if err != nil {
			writeInternalError(c, "list usage event model filter options failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": models})
	})

	router.GET("/usage/events/filters/sources", func(c *gin.Context) {
		sources, err := loadUsageEventSourceFilterOptions(c, usageIdentityProvider)
		if err != nil {
			writeInternalError(c, "list usage event source filter options failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"sources": sources})
	})

	router.GET("/usage/events", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageEventsResponse{Events: []usageEventPayload{}, Page: 1, PageSize: servicedto.DefaultUsageEventsLimit})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := applyUsageEventsSourceFilter(&filter); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		rows, err := usageProvider.ListUsageEvents(c.Request.Context(), filter)
		if err != nil {
			writeInternalError(c, "list usage events failed", err)
			return
		}

		identities, err := loadUsageResolutionData(c, usageIdentityProvider)
		if err != nil {
			writeInternalError(c, "load usage resolution data failed", err)
			return
		}
		resolver := newUsageIdentityResolver(identities)
		apiKeyInfos, err := loadCPAAPIKeyInfos(c, cpaAPIKeyProvider)
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, usageEventsResponse{
			Events:     buildUsageEventsPayload(rows.Events, resolver, apiKeyInfos),
			TotalCount: rows.TotalCount,
			Page:       rows.Page,
			PageSize:   rows.PageSize,
			TotalPages: rows.TotalPages,
		})
	})

	router.GET("/usage/events/:id/request-log", func(c *gin.Context) {
		if !requestLogAccessEnabled {
			writeUsageEventRequestLogAccessDisabled(c)
			return
		}
		if rejectViewerRequestLogAccess(c) {
			return
		}
		if requestLogProvider == nil {
			writeInternalError(c, "request log provider is not configured", nil)
			return
		}
		eventID, ok := parseUsageEventRequestLogEventID(c)
		if !ok {
			return
		}
		response, err := requestLogProvider.GetUsageEventRequestLog(c.Request.Context(), eventID)
		if err != nil {
			writeUsageEventRequestLogError(c, err)
			return
		}
		setNoStoreHeaders(c)
		c.JSON(http.StatusOK, buildUsageEventRequestLogPayload(response))
	})

	router.POST("/usage/events/:id/request-log/download-token", func(c *gin.Context) {
		if !requestLogAccessEnabled {
			writeUsageEventRequestLogAccessDisabled(c)
			return
		}
		if rejectViewerRequestLogAccess(c) {
			return
		}
		if requestLogProvider == nil {
			writeInternalError(c, "request log provider is not configured", nil)
			return
		}
		if requestLogDownloadTokens == nil {
			writeInternalError(c, "request log download token store is not configured", nil)
			return
		}
		eventID, ok := parseUsageEventRequestLogEventID(c)
		if !ok {
			return
		}
		token, err := requestLogDownloadTokens.issue(eventID)
		if err != nil {
			writeInternalError(c, "issue request log download token failed", err)
			return
		}
		downloadURL := strings.TrimSuffix(c.Request.URL.Path, "/download-token") + "/download-file?token=" + url.QueryEscape(token)
		setNoStoreHeaders(c)
		c.JSON(http.StatusOK, usageEventRequestLogDownloadTokenPayload{DownloadURL: downloadURL})
	})
	router.GET("/usage/events/export", func(c *gin.Context) {
		format := strings.ToLower(strings.TrimSpace(c.Query("format")))
		if format == "" {
			format = "csv"
		}
		if format != "csv" && format != "json" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid export format"})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := applyUsageEventsSourceFilter(&filter); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		filter.Limit = 0
		filter.Page = 0
		filter.PageSize = 0
		filter.Offset = 0

		identities, err := loadUsageResolutionData(c, usageIdentityProvider)
		if err != nil {
			writeInternalError(c, "load usage resolution data failed", err)
			return
		}
		resolver := newUsageIdentityResolver(identities)
		apiKeyInfos, err := loadCPAAPIKeyInfos(c, cpaAPIKeyProvider)
		if err != nil {
			return
		}
		streamEvents := func(emit func(servicedto.UsageEventRecord) error) error {
			if usageProvider == nil {
				return nil
			}
			return usageProvider.StreamUsageEvents(c.Request.Context(), filter, emit)
		}
		if format == "json" {
			if err := writeUsageEventsJSONExport(c, streamEvents, resolver, apiKeyInfos); err != nil {
				writeUsageEventsExportError(c, err)
			}
			return
		}
		if err := writeUsageEventsCSVExport(c, streamEvents, resolver, apiKeyInfos); err != nil {
			writeUsageEventsExportError(c, err)
		}
	})
}

// 请求日志可能包含原始请求体和令牌片段，Viewer 即使能查看事件列表也不应读取该敏感内容。
func rejectViewerRequestLogAccess(c *gin.Context) bool {
	if _, ok := accessscope.ViewerScopeFromContext(c.Request.Context()); !ok {
		return false
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	return true
}

func registerUsageEventRequestLogDownloadTokenRoutes(
	router gin.IRoutes,
	requestLogProvider service.RequestLogProvider,
	requestLogDownloadTokens *requestLogDownloadTokenStore,
	requestLogAccessEnabled bool,
) {
	router.GET("/usage/events/:id/request-log/download-file", func(c *gin.Context) {
		if !requestLogAccessEnabled {
			writeUsageEventRequestLogAccessDisabled(c)
			return
		}
		if requestLogProvider == nil {
			writeInternalError(c, "request log provider is not configured", nil)
			return
		}
		if requestLogDownloadTokens == nil {
			writeInternalError(c, "request log download token store is not configured", nil)
			return
		}
		eventID, ok := parseUsageEventRequestLogEventID(c)
		if !ok {
			return
		}
		if !requestLogDownloadTokens.consume(strings.TrimSpace(c.Query("token")), eventID) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired request log download token"})
			return
		}
		streamUsageEventRequestLogDownload(c, requestLogProvider, eventID)
	})
}

func writeUsageEventRequestLogAccessDisabled(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"error": "CPA request log access is disabled"})
}

func parseUsageEventRequestLogEventID(c *gin.Context) (int64, bool) {
	eventID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || eventID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid usage event id"})
		return 0, false
	}
	return eventID, true
}

func streamUsageEventRequestLogDownload(c *gin.Context, requestLogProvider service.RequestLogProvider, eventID int64) {
	response, err := requestLogProvider.DownloadUsageEventRequestLog(c.Request.Context(), eventID)
	if err != nil {
		writeUsageEventRequestLogError(c, err)
		return
	}
	contentType := strings.TrimSpace(response.ContentType)
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	filename := strings.TrimSpace(response.Filename)
	if filename == "" {
		requestID := strings.TrimSpace(response.RequestID)
		if requestID == "" {
			requestID = strconv.FormatInt(eventID, 10)
		}
		filename = "request-log-" + requestID + ".log"
	}
	if response.Body == nil {
		writeInternalError(c, "request log download body is empty", nil)
		return
	}
	defer response.Body.Close()
	contentLength := int64(-1)
	if response.ContentLength > 0 {
		contentLength = response.ContentLength
	}
	setNoStoreHeaders(c)
	c.Header("Content-Disposition", requestLogAttachmentDisposition(filename))
	c.DataFromReader(http.StatusOK, contentLength, contentType, response.Body, nil)
}

// Source 下拉提交的是 usage identity；为了兼容前端命名，API 收 source，但进入仓储前只转换成 auth_index 查询。
func applyUsageEventsSourceFilter(filter *servicedto.UsageFilter) error {
	if filter == nil {
		return nil
	}
	source := strings.TrimSpace(filter.Source)
	if source == "" {
		return nil
	}
	filter.AuthIndex = source
	filter.Source = ""
	return nil
}

// 列表结果先按 auth_index 解析展示名，再组装前端需要的事件 payload。
func buildUsageEventsPayload(rows []servicedto.UsageEventRecord, resolver usageIdentityResolver, apiKeyInfos map[string]analysisAPIKeyInfo) []usageEventPayload {
	if len(rows) == 0 {
		return []usageEventPayload{}
	}
	payload := make([]usageEventPayload, 0, len(rows))
	for _, row := range rows {
		identity, matched := resolver.resolveByAuthIndex(row.AuthIndex)
		source, isDelete := usageEventPublicSource(row, identity, matched)
		id := ""
		if row.ID != 0 {
			id = strconv.FormatInt(row.ID, 10)
		}
		payload = append(payload, usageEventPayload{
			ID:              id,
			Timestamp:       timeutil.FormatStorageTime(row.Timestamp),
			APIKey:          usageEventAPIKeyLabel(row.APIGroupKey, apiKeyInfos),
			Model:           row.Model,
			ModelAlias:      strings.TrimSpace(row.ModelAlias),
			ReasoningEffort: strings.TrimSpace(row.ReasoningEffort),
			ServiceTier:     strings.TrimSpace(row.ServiceTier),
			ExecutorType:    strings.TrimSpace(row.ExecutorType),
			Endpoint:        strings.TrimSpace(row.Endpoint),
			Source:          source,
			SourceType:      identity.Type,
			AuthIndex:       row.AuthIndex,
			RequestID:       strings.TrimSpace(row.RequestID),
			IsDelete:        isDelete,
			Failed:          row.Failed,
			LatencyMS:       row.LatencyMS,
			TTFTMS:          row.TTFTMS,
			SpeedTPS:        usageEventSpeedTPS(row),
			CostUSD:         row.CostUSD,
			CostAvailable:   row.CostAvailable,
			PricingStyle:    strings.TrimSpace(row.PricingStyle),
			Tokens: usageEventTokenPayload{
				InputTokens:         row.InputTokens,
				OutputTokens:        row.OutputTokens,
				ReasoningTokens:     row.ReasoningTokens,
				CachedTokens:        row.CachedTokens,
				CacheReadTokens:     row.CacheReadTokens,
				CacheCreationTokens: row.CacheCreationTokens,
				TotalTokens:         row.TotalTokens,
			},
		})
	}
	return payload
}

func buildUsageEventRequestLogPayload(response service.RequestLogResponse) usageEventRequestLogPayload {
	eventID := ""
	if response.EventID != 0 {
		eventID = strconv.FormatInt(response.EventID, 10)
	}
	sections := make([]usageEventRequestLogSection, 0, len(response.Sections))
	for _, section := range response.Sections {
		sections = append(sections, usageEventRequestLogSection{
			Title:   strings.TrimSpace(section.Title),
			Content: section.Content,
		})
	}
	return usageEventRequestLogPayload{
		EventID:      eventID,
		RequestID:    strings.TrimSpace(response.RequestID),
		Filename:     strings.TrimSpace(response.Filename),
		Available:    response.Available,
		Previewable:  response.Previewable,
		TooLarge:     response.TooLarge,
		Downloadable: response.Downloadable,
		Sections:     sections,
	}
}

func writeUsageEventRequestLogError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "usage event not found"})
	case errors.Is(err, service.ErrRequestLogMissingID):
		c.JSON(http.StatusNotFound, gin.H{"error": "usage event request id missing"})
	case errors.Is(err, service.ErrRequestLogUnavailable):
		c.JSON(http.StatusNotFound, gin.H{"error": "request log unavailable"})
	default:
		writeInternalError(c, "load usage event request log failed", err)
	}
}

func buildUsageEventExportPayload(row servicedto.UsageEventRecord, resolver usageIdentityResolver, apiKeyInfos map[string]analysisAPIKeyInfo) usageEventExportPayload {
	identity, matched := resolver.resolveByAuthIndex(row.AuthIndex)
	source, isIdentityDeleted := usageEventPublicSource(row, identity, matched)
	id := ""
	if row.ID != 0 {
		id = strconv.FormatInt(row.ID, 10)
	}
	result := "success"
	if row.Failed {
		result = "failed"
	}
	return usageEventExportPayload{
		ID:                  id,
		Timestamp:           timeutil.FormatStorageTime(row.Timestamp),
		APIKey:              usageEventAPIKeyLabel(row.APIGroupKey, apiKeyInfos),
		CPAAPIKeyID:         usageEventCPAAPIKeyID(row.APIGroupKey, apiKeyInfos),
		Source:              source,
		SourceType:          identity.Type,
		AuthIndex:           strings.TrimSpace(row.AuthIndex),
		IsIdentityDeleted:   isIdentityDeleted,
		Model:               row.Model,
		ModelAlias:          strings.TrimSpace(row.ModelAlias),
		ReasoningEffort:     strings.TrimSpace(row.ReasoningEffort),
		ServiceTier:         strings.TrimSpace(row.ServiceTier),
		ExecutorType:        strings.TrimSpace(row.ExecutorType),
		Result:              result,
		Endpoint:            strings.TrimSpace(row.Endpoint),
		TTFTMS:              row.TTFTMS,
		LatencyMS:           row.LatencyMS,
		SpeedTPS:            usageEventSpeedTPS(row),
		InputTokens:         row.InputTokens,
		OutputTokens:        row.OutputTokens,
		ReasoningTokens:     row.ReasoningTokens,
		CachedTokens:        row.CachedTokens,
		CacheReadTokens:     row.CacheReadTokens,
		CacheCreationTokens: row.CacheCreationTokens,
		CacheRate:           usageEventCacheRate(row),
		TotalTokens:         row.TotalTokens,
		CostUSD:             row.CostUSD,
	}
}

func usageEventSpeedTPS(row servicedto.UsageEventRecord) *float64 {
	visibleOutputTokens := row.OutputTokens - row.ReasoningTokens
	if visibleOutputTokens < 0 {
		visibleOutputTokens = 0
	}
	if row.TTFTMS == nil || *row.TTFTMS <= 0 || row.LatencyMS <= *row.TTFTMS || visibleOutputTokens <= 1 {
		return nil
	}
	// Speed 只衡量首字后可见输出 token 的平均生成速度，避免把等待首字的时间重复计入。
	speed := float64(visibleOutputTokens-1) / (float64(row.LatencyMS-*row.TTFTMS) / 1000)
	return &speed
}

func usageEventCacheRate(row servicedto.UsageEventRecord) *float64 {
	if row.InputTokens <= 0 {
		return nil
	}
	rate := float64(row.CachedTokens) / float64(row.InputTokens) * 100
	return &rate
}

var usageEventsExportCSVHeader = []string{
	"id",
	"timestamp",
	"api_key",
	"cpa_api_key_id",
	"source",
	"source_type",
	"auth_index",
	"is_identity_deleted",
	"model",
	"model_alias",
	"reasoning_effort",
	"service_tier",
	"executor_type",
	"result",
	"endpoint",
	"ttft_ms",
	"latency_ms",
	"speed_tps",
	"input_tokens",
	"output_tokens",
	"reasoning_tokens",
	"cached_tokens",
	"cache_read_tokens",
	"cache_creation_tokens",
	"cache_rate",
	"total_tokens",
	"cost_usd",
}

func writeUsageEventsJSONExport(c *gin.Context, stream usageEventStreamFunc, resolver usageIdentityResolver, apiKeyInfos map[string]analysisAPIKeyInfo) error {
	encoder := json.NewEncoder(c.Writer)
	encoder.SetEscapeHTML(false)
	started := false
	begin := func() error {
		if started {
			return nil
		}
		c.Header("Content-Disposition", `attachment; filename="`+usageEventsExportFilename("json")+`"`)
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.Status(http.StatusOK)
		if _, err := c.Writer.Write([]byte(`{"events":[`)); err != nil {
			return err
		}
		started = true
		return nil
	}
	var count int64
	if err := stream(func(row servicedto.UsageEventRecord) error {
		if err := begin(); err != nil {
			return err
		}
		if count > 0 {
			if _, err := c.Writer.Write([]byte(",")); err != nil {
				return err
			}
		}
		if err := encoder.Encode(buildUsageEventExportPayload(row, resolver, apiKeyInfos)); err != nil {
			return err
		}
		count++
		return nil
	}); err != nil {
		return err
	}
	if err := begin(); err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte(`],"total_count":` + strconv.FormatInt(count, 10) + `}`)); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func writeUsageEventsCSVExport(c *gin.Context, stream usageEventStreamFunc, resolver usageIdentityResolver, apiKeyInfos map[string]analysisAPIKeyInfo) error {
	var writer *csv.Writer
	started := false
	begin := func() error {
		if started {
			return nil
		}
		c.Header("Content-Disposition", `attachment; filename="`+usageEventsExportFilename("csv")+`"`)
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Status(http.StatusOK)
		writer = csv.NewWriter(c.Writer)
		if err := writer.Write(usageEventsExportCSVHeader); err != nil {
			return err
		}
		started = true
		return nil
	}
	var count int64
	if err := stream(func(row servicedto.UsageEventRecord) error {
		if err := begin(); err != nil {
			return err
		}
		if err := writer.Write(usageEventExportCSVRecord(buildUsageEventExportPayload(row, resolver, apiKeyInfos))); err != nil {
			return err
		}
		count++
		if count%100 == 0 {
			writer.Flush()
			if err := writer.Error(); err != nil {
				return err
			}
			c.Writer.Flush()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := begin(); err != nil {
		return err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func writeUsageEventsExportError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if c.Writer.Written() {
		_ = c.Error(err)
		return
	}
	c.Writer.Header().Del("Content-Disposition")
	writeInternalError(c, "export usage events failed", err)
}

func usageEventsExportFilename(format string) string {
	timestamp := timeutil.NormalizeStorageTime(time.Now()).Format("20060102-150405")
	return "usage-events-" + timestamp + "." + format
}

func sanitizeAttachmentFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	var builder strings.Builder
	for _, r := range filename {
		if r < 0x20 || r == 0x7f || r > 0x7e {
			builder.WriteByte('_')
			continue
		}
		switch r {
		case '\\', '/', '"', ';', ':':
			builder.WriteByte('_')
		default:
			builder.WriteRune(r)
		}
	}
	sanitized := strings.TrimSpace(builder.String())
	if sanitized == "" {
		return "request-log.log"
	}
	return sanitized
}

func sanitizeAttachmentFilenameStar(filename string) string {
	filename = strings.TrimSpace(filename)
	var builder strings.Builder
	for _, r := range filename {
		if r < 0x20 || r == 0x7f {
			builder.WriteByte('_')
			continue
		}
		switch r {
		case '\\', '/', '"', ';', ':':
			builder.WriteByte('_')
		default:
			builder.WriteRune(r)
		}
	}
	sanitized := strings.TrimSpace(builder.String())
	if sanitized == "" {
		return "request-log.log"
	}
	return sanitized
}

func requestLogAttachmentDisposition(filename string) string {
	original := strings.TrimSpace(filename)
	fallback := sanitizeAttachmentFilename(original)
	if original == "" {
		original = fallback
	}
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + url.PathEscape(sanitizeAttachmentFilenameStar(original))
}

func usageEventExportCSVRecord(event usageEventExportPayload) []string {
	return []string{
		event.ID,
		event.Timestamp,
		event.APIKey,
		event.CPAAPIKeyID,
		event.Source,
		event.SourceType,
		event.AuthIndex,
		strconv.FormatBool(event.IsIdentityDeleted),
		event.Model,
		event.ModelAlias,
		event.ReasoningEffort,
		event.ServiceTier,
		event.ExecutorType,
		event.Result,
		event.Endpoint,
		formatOptionalInt64(event.TTFTMS),
		strconv.FormatInt(event.LatencyMS, 10),
		formatOptionalFloat64(event.SpeedTPS),
		strconv.FormatInt(event.InputTokens, 10),
		strconv.FormatInt(event.OutputTokens, 10),
		strconv.FormatInt(event.ReasoningTokens, 10),
		strconv.FormatInt(event.CachedTokens, 10),
		strconv.FormatInt(event.CacheReadTokens, 10),
		strconv.FormatInt(event.CacheCreationTokens, 10),
		formatOptionalFloat64(event.CacheRate),
		strconv.FormatInt(event.TotalTokens, 10),
		strconv.FormatFloat(event.CostUSD, 'f', -1, 64),
	}
}

func formatOptionalInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func formatOptionalFloat64(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func usageEventAPIKeyLabel(apiGroupKey string, apiKeyInfos map[string]analysisAPIKeyInfo) string {
	apiKey := strings.TrimSpace(apiGroupKey)
	if apiKey == "" {
		return ""
	}
	return analysisAPIKeyLabel(apiKey, apiKeyInfos)
}

func usageEventCPAAPIKeyID(apiGroupKey string, apiKeyInfos map[string]analysisAPIKeyInfo) string {
	apiKey := strings.TrimSpace(apiGroupKey)
	if apiKey == "" {
		return ""
	}
	if info, ok := apiKeyInfos[apiKey]; ok {
		return info.ID
	}
	return ""
}

func usageEventPublicSource(row servicedto.UsageEventRecord, identity resolvedUsageIdentity, matched bool) (string, bool) {
	if matched {
		return identity.DisplayName, false
	}
	isDelete := strings.TrimSpace(row.AuthIndex) != ""
	switch strings.TrimSpace(row.AuthType) {
	case "apikey":
		return strings.TrimSpace(row.Provider), isDelete
	case "oauth":
		return strings.TrimSpace(row.Source), isDelete
	default:
		return strings.TrimSpace(row.Provider), isDelete
	}
}

func loadUsageEventModelFilterOptions(c *gin.Context, usageProvider service.UsageProvider) ([]string, error) {
	if usageProvider == nil {
		return []string{}, nil
	}
	options, err := usageProvider.ListUsageEventFilterOptions(c.Request.Context(), servicedto.UsageFilter{})
	if err != nil {
		return nil, err
	}
	return options.Models, nil
}

func loadUsageEventSourceFilterOptions(c *gin.Context, usageIdentityProvider service.UsageIdentityProvider) ([]usageSourceFilterOption, error) {
	identities, err := loadUsageResolutionData(c, usageIdentityProvider)
	if err != nil {
		return nil, err
	}
	return buildUsageSourceFilterOptions(identities), nil
}

// Source 筛选项从活跃身份生成，避免把 usage_events.source 当成可选项暴露给页面。
func buildUsageSourceFilterOptions(identities []entities.UsageIdentity) []usageSourceFilterOption {
	if len(identities) == 0 {
		return []usageSourceFilterOption{}
	}
	options := make([]usageSourceFilterOption, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		// Source 下拉只展示活跃且有流量的身份，避免已删除身份继续出现在筛选项里。
		if identity.IsDeleted || identity.TotalRequests == 0 {
			continue
		}
		option, ok := usageSourceFilterOptionFromIdentity(identity)
		if !ok {
			continue
		}
		if _, exists := seen[option.Value]; exists {
			continue
		}
		seen[option.Value] = struct{}{}
		options = append(options, option)
	}
	return options
}

func usageSourceFilterOptionFromIdentity(identity entities.UsageIdentity) (usageSourceFilterOption, bool) {
	switch identity.AuthType {
	case entities.UsageIdentityAuthTypeAuthFile, entities.UsageIdentityAuthTypeAIProvider:
		value := strings.TrimSpace(identity.Identity)
		if value == "" {
			return usageSourceFilterOption{}, false
		}
		displayName := helper.UsageIdentityDisplayName(identity)
		return usageSourceFilterOption{Value: value, Label: displayName, DisplayName: displayName}, true
	default:
		return usageSourceFilterOption{}, false
	}
}
