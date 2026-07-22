package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

type usageActivityResponse struct {
	Window              string               `json:"window"`
	Grain               string               `json:"grain"`
	Timezone            string               `json:"timezone"`
	Rows                int                  `json:"rows"`
	Columns             int                  `json:"columns"`
	BucketSeconds       int64                `json:"bucket_seconds"`
	WindowStart         time.Time            `json:"window_start"`
	WindowEnd           time.Time            `json:"window_end"`
	TotalSuccess        int64                `json:"total_success"`
	TotalFailure        int64                `json:"total_failure"`
	SuccessRate         float64              `json:"success_rate"`
	InputTokens         int64                `json:"input_tokens"`
	OutputTokens        int64                `json:"output_tokens"`
	ReasoningTokens     int64                `json:"reasoning_tokens"`
	CacheReadTokens     int64                `json:"cache_read_tokens"`
	CacheCreationTokens int64                `json:"cache_creation_tokens"`
	TotalTokens         int64                `json:"total_tokens"`
	Blocks              []usageActivityBlock `json:"blocks"`
}

type usageActivityBlock struct {
	StartTime           time.Time `json:"start_time"`
	EndTime             time.Time `json:"end_time"`
	Success             int64     `json:"success"`
	Failure             int64     `json:"failure"`
	Rate                float64   `json:"rate"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	ReasoningTokens     int64     `json:"reasoning_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	TotalTokens         int64     `json:"total_tokens"`
}

func parseUsageActivityFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageActivityFilterQueryWithClientAPIKey(req, anchor, true)
}

func parseUsageActivityFilterQueryWithClientAPIKey(req *http.Request, anchor time.Time, includeClientAPIKey bool) (servicedto.UsageFilter, error) {
	queryNow := timeutil.NormalizeStorageTime(anchor)
	if req == nil || strings.TrimSpace(req.URL.Query().Get("window")) == "" {
		// Activity 的长 Custom 日范围使用永久 daily 增量档位，并遵循统一的一年上限。
		filter, err := parseUsageTimeFilterQueryWithOptions(req, queryNow, includeClientAPIKey, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: timeutil.LongCustomDayRangeMaxDays})
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
		filter.QueryNow = &queryNow
		return filter, nil
	}

	// Activity 专属 window 使用标准四档，以及归入 Day 视图的自然日模式。
	window := servicedto.UsageActivityWindow(strings.TrimSpace(req.URL.Query().Get("window")))
	if window != servicedto.UsageActivityWindowDay &&
		window != servicedto.UsageActivityWindowWeek &&
		window != servicedto.UsageActivityWindowMonth &&
		window != servicedto.UsageActivityWindowYear &&
		window != servicedto.UsageActivityWindowToday &&
		window != servicedto.UsageActivityWindowYesterday {
		return servicedto.UsageFilter{}, fmt.Errorf("unsupported activity window %q", window)
	}
	apiKeyID := ""
	if includeClientAPIKey {
		var err error
		apiKeyID, err = parseUsageAPIKeyID(req.URL.Query().Get("api_key_id"))
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
	}
	if window == servicedto.UsageActivityWindowDay ||
		window == servicedto.UsageActivityWindowWeek ||
		window == servicedto.UsageActivityWindowMonth ||
		window == servicedto.UsageActivityWindowYear {
		return servicedto.UsageFilter{ActivityWindow: window, QueryNow: &queryNow, APIKeyID: apiKeyID}, nil
	}

	// today/yesterday 复用公共时间解析器，确保 Overview、Analysis 与 Activity 的自然日边界一致。
	normalizedRange, err := timeutil.ParseUsageQueryRange(string(window), "", "", "", queryNow)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	startTime := normalizedRange.StartTime
	endTime := normalizedRange.EndTime
	return servicedto.UsageFilter{
		Range:          normalizedRange.Range,
		RangeUnit:      string(normalizedRange.Unit),
		RangeCount:     normalizedRange.Count,
		StartTime:      &startTime,
		EndTime:        &endTime,
		EndExclusive:   normalizedRange.EndExclusive,
		ActivityWindow: window,
		QueryNow:       &queryNow,
		APIKeyID:       apiKeyID,
	}, nil
}

func parseKeyUsageActivityFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	filter, err := parseUsageActivityFilterQueryWithClientAPIKey(req, anchor, false)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	return filter, nil
}

func registerUsageActivityRoute(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage/activity", func(c *gin.Context) {
		filter, err := parseUsageActivityFilterQuery(c.Request, time.Now())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		writeUsageActivityResponse(c, usageProvider, filter)
	})
}

func registerKeyActivityRoute(router gin.IRoutes, usageProvider service.UsageProvider, cpaAPIKeyProvider service.CPAAPIKeyProvider, authHandler *authHandler) {
	router.GET("/key-activity", func(c *gin.Context) {
		token, _ := c.Get("auth_token")
		sessionValue, _ := c.Get("auth_session")
		session, ok := sessionValue.(auth.Session)
		if !ok || session.Role != auth.RoleAPIKeyViewer || session.CPAAPIKeyID <= 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		if cpaAPIKeyProvider == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		if _, err := cpaAPIKeyProvider.FindActiveCPAAPIKeyByID(c.Request.Context(), session.CPAAPIKeyID); err != nil {
			if authHandler != nil {
				authHandler.deleteSession(fmt.Sprint(token))
				clearSessionCookie(c, authHandler.config.BasePath, resolveSessionToken(c).CookieKind)
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		// Key Viewer 复用公共时间参数，但客户端 api_key_id 无论内容为何都不参与解析或过滤。
		filter, err := parseKeyUsageActivityFilterQuery(c.Request, time.Now())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if authHandler != nil && !authHandler.allowKeyOverviewRequest(fmt.Sprint(token), "activity") {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		filter.APIKeyID = fmt.Sprintf("%d", session.CPAAPIKeyID)
		writeUsageActivityResponse(c, usageProvider, filter)
	})
}

func writeUsageActivityResponse(c *gin.Context, usageProvider service.UsageProvider, filter servicedto.UsageFilter) {
	if usageProvider == nil {
		writeInternalError(c, "get usage activity failed", fmt.Errorf("usage provider is unavailable"))
		return
	}
	activity, err := usageProvider.GetUsageActivity(c.Request.Context(), filter)
	if err != nil {
		writeUsageProviderError(c, "get usage activity failed", err)
		return
	}
	c.JSON(http.StatusOK, buildUsageActivityResponse(activity))
}

func buildUsageActivityResponse(activity *servicedto.UsageActivitySnapshot) usageActivityResponse {
	if activity == nil {
		return usageActivityResponse{Timezone: time.Local.String(), Blocks: []usageActivityBlock{}}
	}
	blocks := make([]usageActivityBlock, len(activity.Blocks))
	for index, block := range activity.Blocks {
		blocks[index] = usageActivityBlock{
			StartTime:           block.StartTime,
			EndTime:             block.EndTime,
			Success:             block.Success,
			Failure:             block.Failure,
			Rate:                block.Rate,
			InputTokens:         block.InputTokens,
			OutputTokens:        block.OutputTokens,
			ReasoningTokens:     block.ReasoningTokens,
			CacheReadTokens:     block.CacheReadTokens,
			CacheCreationTokens: block.CacheCreationTokens,
			TotalTokens:         block.TotalTokens,
		}
	}
	return usageActivityResponse{
		Window:              string(activity.Window),
		Grain:               activity.Grain,
		Timezone:            time.Local.String(),
		Rows:                activity.Rows,
		Columns:             activity.Columns,
		BucketSeconds:       activity.BucketSeconds,
		WindowStart:         activity.WindowStart,
		WindowEnd:           activity.WindowEnd,
		TotalSuccess:        activity.TotalSuccess,
		TotalFailure:        activity.TotalFailure,
		SuccessRate:         activity.SuccessRate,
		InputTokens:         activity.InputTokens,
		OutputTokens:        activity.OutputTokens,
		ReasoningTokens:     activity.ReasoningTokens,
		CacheReadTokens:     activity.CacheReadTokens,
		CacheCreationTokens: activity.CacheCreationTokens,
		TotalTokens:         activity.TotalTokens,
		Blocks:              blocks,
	}
}
