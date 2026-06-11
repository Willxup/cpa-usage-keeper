package api

import (
	"fmt"
	"net/http"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/helper"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

type usageOverviewResponse struct {
	Usage         usageOverviewPayload       `json:"usage"`
	Summary       usageOverviewSummary       `json:"summary"`
	Series        usageOverviewSeries        `json:"series"`
	ServiceHealth usageOverviewServiceHealth `json:"service_health"`
	Timezone      string                     `json:"timezone"`
	RangeStart    *time.Time                 `json:"range_start,omitempty"`
	RangeEnd      *time.Time                 `json:"range_end,omitempty"`
}

type usageOverviewPayload struct {
	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	FailureCount  int64 `json:"failure_count"`
	TotalTokens   int64 `json:"total_tokens"`
}

type usageOverviewSummary struct {
	RequestCount    int64   `json:"request_count"`
	TokenCount      int64   `json:"token_count"`
	WindowMinutes   int64   `json:"window_minutes"`
	RPM             float64 `json:"rpm"`
	TPM             float64 `json:"tpm"`
	TotalCost       float64 `json:"total_cost"`
	CostAvailable   bool    `json:"cost_available"`
	InputTokens     int64   `json:"input_tokens"`
	CachedTokens    int64   `json:"cached_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
}

type usageOverviewSeries struct {
	Requests  map[string]int64    `json:"requests"`
	Tokens    map[string]int64    `json:"tokens"`
	RPM       map[string]float64  `json:"rpm"`
	TPM       map[string]float64  `json:"tpm"`
	Cost      map[string]float64  `json:"cost"`
	CacheRate map[string]*float64 `json:"cache_rate"`
}

type usageOverviewServiceHealth struct {
	TotalSuccess  int64                             `json:"total_success"`
	TotalFailure  int64                             `json:"total_failure"`
	SuccessRate   float64                           `json:"success_rate"`
	Rows          int                               `json:"rows"`
	Columns       int                               `json:"columns"`
	BucketSeconds int64                             `json:"bucket_seconds"`
	WindowStart   time.Time                         `json:"window_start"`
	WindowEnd     time.Time                         `json:"window_end"`
	BlockDetails  []usageOverviewServiceHealthBlock `json:"block_details"`
}

type usageOverviewServiceHealthBlock struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Success   int64     `json:"success"`
	Failure   int64     `json:"failure"`
	Rate      float64   `json:"rate"`
}

type usageOverviewRealtime struct {
	Window               string                            `json:"window"`
	Timezone             string                            `json:"timezone"`
	BucketSeconds        int64                             `json:"bucket_seconds"`
	TokenVelocity        []usageOverviewTokenVelocityPoint `json:"token_velocity"`
	ResponseLevel        []usageOverviewResponseLevelPoint `json:"response_level"`
	ResponseDistribution usageOverviewResponseDistribution `json:"response_distribution"`
	CurrentUsage         usageOverviewRealtimeCurrentUsage `json:"current_usage"`
	RequestLevel         []usageOverviewRequestLevelPoint  `json:"request_level"`
	CacheLevel           []usageOverviewCacheLevelPoint    `json:"cache_level"`
}

type keyUsageOverviewRealtime struct {
	Window               string                               `json:"window"`
	Timezone             string                               `json:"timezone"`
	BucketSeconds        int64                                `json:"bucket_seconds"`
	TokenVelocity        []usageOverviewTokenVelocityPoint    `json:"token_velocity"`
	ResponseLevel        []usageOverviewResponseLevelPoint    `json:"response_level"`
	ResponseDistribution usageOverviewResponseDistribution    `json:"response_distribution"`
	CurrentUsage         keyUsageOverviewRealtimeCurrentUsage `json:"current_usage"`
	RequestLevel         []usageOverviewRequestLevelPoint     `json:"request_level"`
	CacheLevel           []usageOverviewCacheLevelPoint       `json:"cache_level"`
}

type usageOverviewTokenVelocityPoint struct {
	Bucket          string   `json:"bucket"`
	TokensPerMinute float64  `json:"tokens_per_minute"`
	Tokens          int64    `json:"tokens"`
	Cost            *float64 `json:"cost,omitempty"`
}

type usageOverviewResponseLevelPoint struct {
	Bucket       string `json:"bucket"`
	TTFTP50MS    *int64 `json:"ttft_p50_ms,omitempty"`
	TTFTP95MS    *int64 `json:"ttft_p95_ms,omitempty"`
	LatencyP50MS *int64 `json:"latency_p50_ms,omitempty"`
	LatencyP95MS *int64 `json:"latency_p95_ms,omitempty"`
}

type usageOverviewResponseAveragePoint struct {
	Bucket string   `json:"bucket"`
	AvgMS  *float64 `json:"avg_ms,omitempty"`
}

type usageOverviewResponseParticle struct {
	Bucket string `json:"bucket"`
	MS     int64  `json:"ms"`
	Count  int64  `json:"count"`
}

type usageOverviewResponseDistributionSeries struct {
	AverageLine []usageOverviewResponseAveragePoint `json:"average_line"`
	Particles   []usageOverviewResponseParticle     `json:"particles"`
}

type usageOverviewResponseDistribution struct {
	TTFT    usageOverviewResponseDistributionSeries `json:"ttft"`
	Latency usageOverviewResponseDistributionSeries `json:"latency"`
}

type usageOverviewRealtimeCurrentUsage struct {
	Models      []usageOverviewRealtimeUsageTopItem `json:"models"`
	APIKeys     []usageOverviewRealtimeUsageTopItem `json:"api_keys"`
	AuthFiles   []usageOverviewRealtimeUsageTopItem `json:"auth_files"`
	AIProviders []usageOverviewRealtimeUsageTopItem `json:"ai_providers"`
}

type keyUsageOverviewRealtimeCurrentUsage struct {
	Models []usageOverviewRealtimeUsageTopItem `json:"models"`
}

type usageOverviewRealtimeBase struct {
	Window               string
	Timezone             string
	BucketSeconds        int64
	TokenVelocity        []usageOverviewTokenVelocityPoint
	ResponseLevel        []usageOverviewResponseLevelPoint
	ResponseDistribution usageOverviewResponseDistribution
	RequestLevel         []usageOverviewRequestLevelPoint
	CacheLevel           []usageOverviewCacheLevelPoint
}

type usageOverviewRealtimeUsageTopItem struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Tokens   int64    `json:"tokens"`
	Requests int64    `json:"requests"`
	Cost     *float64 `json:"cost,omitempty"`
	Share    float64  `json:"share"`
}

type usageOverviewRequestLevelPoint struct {
	Bucket            string  `json:"bucket"`
	RequestsPerMinute float64 `json:"requests_per_minute"`
	Requests          int64   `json:"requests"`
}

type usageOverviewCacheLevelPoint struct {
	Bucket       string   `json:"bucket"`
	CacheRate    *float64 `json:"cache_rate,omitempty"`
	CachedTokens int64    `json:"cached_tokens"`
	InputTokens  int64    `json:"input_tokens"`
}

var allowedKeyOverviewRanges = map[string]struct{}{
	"4h": {}, "8h": {}, "12h": {}, "24h": {}, "today": {}, "yesterday": {}, "7d": {}, "30d": {},
}

func registerKeyOverviewRoute(router gin.IRoutes, usageProvider service.UsageProvider, cpaAPIKeyProvider service.CPAAPIKeyProvider, authHandler *authHandler) {
	router.GET("/key-overview", func(c *gin.Context) {
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
				clearSessionCookie(c, authHandler.config.BasePath)
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		filter, err := parseKeyOverviewFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if authHandler != nil && !authHandler.allowKeyOverviewRequest(fmt.Sprint(token)) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		filter.APIKeyID = fmt.Sprintf("%d", session.CPAAPIKeyID)
		writeUsageOverviewResponse(c, usageProvider, filter)
	})
	router.GET("/key-overview/realtime", func(c *gin.Context) {
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
				clearSessionCookie(c, authHandler.config.BasePath)
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		filter, err := parseUsageRealtimeFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if authHandler != nil && !authHandler.allowKeyOverviewRequest(fmt.Sprint(token), "realtime") {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		filter.APIKeyID = fmt.Sprintf("%d", session.CPAAPIKeyID)
		writeKeyUsageOverviewRealtimeResponse(c, usageProvider, filter)
	})
}

func registerUsageOverviewRoute(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage/overview", func(c *gin.Context) {
		if usageProvider == nil {
			writeUsageOverviewResponse(c, usageProvider, servicedto.UsageFilter{})
			return
		}
		filter, err := parseUsageFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		writeUsageOverviewResponse(c, usageProvider, filter)
	})
	router.GET("/usage/overview/realtime", func(c *gin.Context) {
		filter, err := parseUsageRealtimeFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		writeUsageOverviewRealtimeResponse(c, usageProvider, filter)
	})
}

func parseKeyOverviewFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	query := req.URL.Query()
	if query.Get("start") != "" || query.Get("end") != "" {
		return servicedto.UsageFilter{}, fmt.Errorf("custom ranges are not supported")
	}
	rangeValue := query.Get("range")
	if _, ok := allowedKeyOverviewRanges[rangeValue]; !ok {
		return servicedto.UsageFilter{}, fmt.Errorf("unsupported key overview range %q", rangeValue)
	}
	return parseUsageFilterQuery(req, anchor)
}

func writeUsageOverviewResponse(c *gin.Context, usageProvider service.UsageProvider, filter servicedto.UsageFilter) {
	if usageProvider == nil {
		c.JSON(http.StatusOK, usageOverviewResponse{
			Usage:         buildUsageOverviewPayload(nil),
			Summary:       usageOverviewSummary{},
			Series:        emptyUsageOverviewSeries(),
			ServiceHealth: usageOverviewServiceHealth{BlockDetails: []usageOverviewServiceHealthBlock{}},
			Timezone:      time.Local.String(),
			RangeStart:    filter.StartTime,
			RangeEnd:      filter.EndTime,
		})
		return
	}

	overview, err := usageProvider.GetUsageOverview(c.Request.Context(), filter)
	if err != nil {
		writeInternalError(c, "get usage overview failed", err)
		return
	}

	var usage *repodto.StatisticsSnapshot
	if overview != nil {
		usage = overview.Usage
	}
	c.JSON(http.StatusOK, usageOverviewResponse{
		Usage:         buildUsageOverviewPayload(usage),
		Summary:       buildUsageOverviewSummary(overview),
		Series:        buildUsageOverviewSeries(overview),
		ServiceHealth: buildUsageOverviewServiceHealth(overview),
		Timezone:      time.Local.String(),
		RangeStart:    filter.StartTime,
		RangeEnd:      filter.EndTime,
	})
}

func writeUsageOverviewRealtimeResponse(c *gin.Context, usageProvider service.UsageProvider, filter servicedto.UsageFilter) {
	if usageProvider == nil {
		c.JSON(http.StatusOK, emptyUsageOverviewRealtime(filter.RealtimeWindow))
		return
	}
	realtime, err := usageProvider.GetUsageOverviewRealtime(c.Request.Context(), filter)
	if err != nil {
		writeInternalError(c, "get usage overview realtime failed", err)
		return
	}
	c.JSON(http.StatusOK, buildUsageOverviewRealtime(realtime, filter.RealtimeWindow))
}

func writeKeyUsageOverviewRealtimeResponse(c *gin.Context, usageProvider service.UsageProvider, filter servicedto.UsageFilter) {
	if usageProvider == nil {
		c.JSON(http.StatusOK, emptyKeyUsageOverviewRealtime(filter.RealtimeWindow))
		return
	}
	realtime, err := usageProvider.GetUsageOverviewRealtime(c.Request.Context(), filter)
	if err != nil {
		writeInternalError(c, "get usage overview realtime failed", err)
		return
	}
	c.JSON(http.StatusOK, buildKeyUsageOverviewRealtime(realtime, filter.RealtimeWindow))
}

func buildUsageOverviewPayload(snapshot *repodto.StatisticsSnapshot) usageOverviewPayload {
	if snapshot == nil {
		return usageOverviewPayload{}
	}

	payload := usageOverviewPayload{
		TotalRequests: snapshot.TotalRequests,
		SuccessCount:  snapshot.SuccessCount,
		FailureCount:  snapshot.FailureCount,
		TotalTokens:   snapshot.TotalTokens,
	}

	return payload
}

func buildUsageOverviewSummary(overview *servicedto.UsageOverviewSnapshot) usageOverviewSummary {
	if overview == nil {
		return usageOverviewSummary{}
	}
	return usageOverviewSummary{
		RequestCount:    overview.Summary.RequestCount,
		TokenCount:      overview.Summary.TokenCount,
		WindowMinutes:   overview.Summary.WindowMinutes,
		RPM:             overview.Summary.RPM,
		TPM:             overview.Summary.TPM,
		TotalCost:       overview.Summary.TotalCost,
		CostAvailable:   overview.Summary.CostAvailable,
		InputTokens:     overview.Summary.InputTokens,
		CachedTokens:    overview.Summary.CachedTokens,
		ReasoningTokens: overview.Summary.ReasoningTokens,
	}
}

func emptyUsageOverviewSeries() usageOverviewSeries {
	return usageOverviewSeries{
		Requests:  map[string]int64{},
		Tokens:    map[string]int64{},
		RPM:       map[string]float64{},
		TPM:       map[string]float64{},
		Cost:      map[string]float64{},
		CacheRate: map[string]*float64{},
	}
}

func mapUsageOverviewSeries(series servicedto.UsageOverviewSeries) usageOverviewSeries {
	return usageOverviewSeries{
		Requests:  cloneInt64Map(series.Requests),
		Tokens:    cloneInt64Map(series.Tokens),
		RPM:       cloneFloat64Map(series.RPM),
		TPM:       cloneFloat64Map(series.TPM),
		Cost:      cloneFloat64Map(series.Cost),
		CacheRate: cloneFloat64PtrMap(series.CacheRate),
	}
}

func buildUsageOverviewSeries(overview *servicedto.UsageOverviewSnapshot) usageOverviewSeries {
	if overview == nil {
		return emptyUsageOverviewSeries()
	}
	return mapUsageOverviewSeries(overview.Series)
}

func buildUsageOverviewServiceHealth(overview *servicedto.UsageOverviewSnapshot) usageOverviewServiceHealth {
	if overview == nil {
		return usageOverviewServiceHealth{BlockDetails: []usageOverviewServiceHealthBlock{}}
	}
	blocks := make([]usageOverviewServiceHealthBlock, 0, len(overview.Health.BlockDetails))
	for _, block := range overview.Health.BlockDetails {
		blocks = append(blocks, usageOverviewServiceHealthBlock{
			StartTime: block.StartTime,
			EndTime:   block.EndTime,
			Success:   block.Success,
			Failure:   block.Failure,
			Rate:      block.Rate,
		})
	}
	return usageOverviewServiceHealth{
		TotalSuccess:  overview.Health.TotalSuccess,
		TotalFailure:  overview.Health.TotalFailure,
		SuccessRate:   overview.Health.SuccessRate,
		Rows:          overview.Health.Rows,
		Columns:       overview.Health.Columns,
		BucketSeconds: overview.Health.BucketSeconds,
		WindowStart:   overview.Health.WindowStart,
		WindowEnd:     overview.Health.WindowEnd,
		BlockDetails:  blocks,
	}
}

func emptyUsageOverviewRealtime(window string) usageOverviewRealtime {
	base := emptyUsageOverviewRealtimeBase(window)
	return usageOverviewRealtime{
		Window:               base.Window,
		Timezone:             base.Timezone,
		BucketSeconds:        base.BucketSeconds,
		TokenVelocity:        base.TokenVelocity,
		ResponseLevel:        base.ResponseLevel,
		ResponseDistribution: base.ResponseDistribution,
		CurrentUsage: usageOverviewRealtimeCurrentUsage{
			Models:      []usageOverviewRealtimeUsageTopItem{},
			APIKeys:     []usageOverviewRealtimeUsageTopItem{},
			AuthFiles:   []usageOverviewRealtimeUsageTopItem{},
			AIProviders: []usageOverviewRealtimeUsageTopItem{},
		},
		RequestLevel: base.RequestLevel,
		CacheLevel:   base.CacheLevel,
	}
}

func emptyKeyUsageOverviewRealtime(window string) keyUsageOverviewRealtime {
	base := emptyUsageOverviewRealtimeBase(window)
	return keyUsageOverviewRealtime{
		Window:               base.Window,
		Timezone:             base.Timezone,
		BucketSeconds:        base.BucketSeconds,
		TokenVelocity:        base.TokenVelocity,
		ResponseLevel:        base.ResponseLevel,
		ResponseDistribution: base.ResponseDistribution,
		CurrentUsage: keyUsageOverviewRealtimeCurrentUsage{
			Models: []usageOverviewRealtimeUsageTopItem{},
		},
		RequestLevel: base.RequestLevel,
		CacheLevel:   base.CacheLevel,
	}
}

func emptyUsageOverviewRealtimeBase(window string) usageOverviewRealtimeBase {
	if window == "" {
		window = "15m"
	}
	bucketSeconds := realtimeBucketSeconds(window)
	return usageOverviewRealtimeBase{
		Window:        window,
		Timezone:      time.Local.String(),
		BucketSeconds: bucketSeconds,
		TokenVelocity: []usageOverviewTokenVelocityPoint{},
		ResponseLevel: []usageOverviewResponseLevelPoint{},
		ResponseDistribution: usageOverviewResponseDistribution{
			TTFT: usageOverviewResponseDistributionSeries{
				AverageLine: []usageOverviewResponseAveragePoint{},
				Particles:   []usageOverviewResponseParticle{},
			},
			Latency: usageOverviewResponseDistributionSeries{
				AverageLine: []usageOverviewResponseAveragePoint{},
				Particles:   []usageOverviewResponseParticle{},
			},
		},
		RequestLevel: []usageOverviewRequestLevelPoint{},
		CacheLevel:   []usageOverviewCacheLevelPoint{},
	}
}

func realtimeBucketSeconds(window string) int64 {
	switch window {
	case "30m":
		return 60
	case "60m":
		return 120
	default:
		return 30
	}
}

func buildUsageOverviewRealtime(realtime *servicedto.UsageOverviewRealtime, window string) usageOverviewRealtime {
	if realtime == nil {
		return emptyUsageOverviewRealtime(window)
	}
	result := usageOverviewRealtime{
		Window:               realtime.Window,
		Timezone:             time.Local.String(),
		BucketSeconds:        realtime.BucketSeconds,
		TokenVelocity:        make([]usageOverviewTokenVelocityPoint, 0, len(realtime.TokenVelocity)),
		ResponseLevel:        make([]usageOverviewResponseLevelPoint, 0, len(realtime.ResponseLevel)),
		ResponseDistribution: mapUsageOverviewResponseDistribution(realtime.ResponseDistribution),
		CurrentUsage: usageOverviewRealtimeCurrentUsage{
			Models:      mapUsageOverviewRealtimeTopItems(realtime.CurrentUsage.Models, false),
			APIKeys:     mapUsageOverviewRealtimeTopItems(realtime.CurrentUsage.APIKeys, true),
			AuthFiles:   mapUsageOverviewRealtimeTopItems(realtime.CurrentUsage.AuthFiles, false),
			AIProviders: mapUsageOverviewRealtimeTopItems(realtime.CurrentUsage.AIProviders, false),
		},
		RequestLevel: make([]usageOverviewRequestLevelPoint, 0, len(realtime.RequestLevel)),
		CacheLevel:   make([]usageOverviewCacheLevelPoint, 0, len(realtime.CacheLevel)),
	}
	if result.Window == "" {
		result.Window = window
	}
	if result.Window == "" {
		result.Window = "15m"
	}
	for _, point := range realtime.TokenVelocity {
		result.TokenVelocity = append(result.TokenVelocity, usageOverviewTokenVelocityPoint{
			Bucket:          point.Bucket,
			TokensPerMinute: point.TokensPerMinute,
			Tokens:          point.Tokens,
			Cost:            point.CostUSD,
		})
	}
	for _, point := range realtime.ResponseLevel {
		result.ResponseLevel = append(result.ResponseLevel, usageOverviewResponseLevelPoint{
			Bucket:       point.Bucket,
			TTFTP50MS:    point.TTFTP50MS,
			TTFTP95MS:    point.TTFTP95MS,
			LatencyP50MS: point.LatencyP50MS,
			LatencyP95MS: point.LatencyP95MS,
		})
	}
	for _, point := range realtime.RequestLevel {
		result.RequestLevel = append(result.RequestLevel, usageOverviewRequestLevelPoint{
			Bucket:            point.Bucket,
			RequestsPerMinute: point.RequestsPerMinute,
			Requests:          point.Requests,
		})
	}
	for _, point := range realtime.CacheLevel {
		result.CacheLevel = append(result.CacheLevel, usageOverviewCacheLevelPoint{
			Bucket:       point.Bucket,
			CacheRate:    point.CacheRate,
			CachedTokens: point.CachedTokens,
			InputTokens:  point.InputTokens,
		})
	}
	return result
}

func buildKeyUsageOverviewRealtime(realtime *servicedto.UsageOverviewRealtime, window string) keyUsageOverviewRealtime {
	if realtime == nil {
		return emptyKeyUsageOverviewRealtime(window)
	}
	result := keyUsageOverviewRealtime{
		Window:               realtime.Window,
		Timezone:             time.Local.String(),
		BucketSeconds:        realtime.BucketSeconds,
		TokenVelocity:        make([]usageOverviewTokenVelocityPoint, 0, len(realtime.TokenVelocity)),
		ResponseLevel:        make([]usageOverviewResponseLevelPoint, 0, len(realtime.ResponseLevel)),
		ResponseDistribution: mapUsageOverviewResponseDistribution(realtime.ResponseDistribution),
		CurrentUsage: keyUsageOverviewRealtimeCurrentUsage{
			Models: mapUsageOverviewRealtimeTopItems(realtime.CurrentUsage.Models, false),
		},
		RequestLevel: make([]usageOverviewRequestLevelPoint, 0, len(realtime.RequestLevel)),
		CacheLevel:   make([]usageOverviewCacheLevelPoint, 0, len(realtime.CacheLevel)),
	}
	if result.Window == "" {
		result.Window = window
	}
	if result.Window == "" {
		result.Window = "15m"
	}
	for _, point := range realtime.TokenVelocity {
		result.TokenVelocity = append(result.TokenVelocity, usageOverviewTokenVelocityPoint{
			Bucket:          point.Bucket,
			TokensPerMinute: point.TokensPerMinute,
			Tokens:          point.Tokens,
			Cost:            point.CostUSD,
		})
	}
	for _, point := range realtime.ResponseLevel {
		result.ResponseLevel = append(result.ResponseLevel, usageOverviewResponseLevelPoint{
			Bucket:       point.Bucket,
			TTFTP50MS:    point.TTFTP50MS,
			TTFTP95MS:    point.TTFTP95MS,
			LatencyP50MS: point.LatencyP50MS,
			LatencyP95MS: point.LatencyP95MS,
		})
	}
	for _, point := range realtime.RequestLevel {
		result.RequestLevel = append(result.RequestLevel, usageOverviewRequestLevelPoint{
			Bucket:            point.Bucket,
			RequestsPerMinute: point.RequestsPerMinute,
			Requests:          point.Requests,
		})
	}
	for _, point := range realtime.CacheLevel {
		result.CacheLevel = append(result.CacheLevel, usageOverviewCacheLevelPoint{
			Bucket:       point.Bucket,
			CacheRate:    point.CacheRate,
			CachedTokens: point.CachedTokens,
			InputTokens:  point.InputTokens,
		})
	}
	return result
}

func mapUsageOverviewResponseDistribution(distribution servicedto.RealtimeResponseDistribution) usageOverviewResponseDistribution {
	return usageOverviewResponseDistribution{
		TTFT:    mapUsageOverviewResponseDistributionSeries(distribution.TTFT),
		Latency: mapUsageOverviewResponseDistributionSeries(distribution.Latency),
	}
}

func mapUsageOverviewResponseDistributionSeries(series servicedto.RealtimeResponseDistributionSeries) usageOverviewResponseDistributionSeries {
	return usageOverviewResponseDistributionSeries{
		AverageLine: mapUsageOverviewResponseAveragePoints(series.AverageLine),
		Particles:   mapUsageOverviewResponseParticles(series.Particles),
	}
}

func mapUsageOverviewResponseAveragePoints(points []servicedto.RealtimeResponseAveragePoint) []usageOverviewResponseAveragePoint {
	result := make([]usageOverviewResponseAveragePoint, 0, len(points))
	for _, point := range points {
		result = append(result, usageOverviewResponseAveragePoint{
			Bucket: point.Bucket,
			AvgMS:  point.AvgMS,
		})
	}
	return result
}

func mapUsageOverviewResponseParticles(points []servicedto.RealtimeResponseParticle) []usageOverviewResponseParticle {
	result := make([]usageOverviewResponseParticle, 0, len(points))
	for _, point := range points {
		result = append(result, usageOverviewResponseParticle{
			Bucket: point.Bucket,
			MS:     point.MS,
			Count:  point.Count,
		})
	}
	return result
}

func mapUsageOverviewRealtimeTopItems(items []servicedto.RealtimeUsageTopItem, redactAPIKey bool) []usageOverviewRealtimeUsageTopItem {
	result := make([]usageOverviewRealtimeUsageTopItem, 0, len(items))
	for _, item := range items {
		key := item.Key
		label := item.Label
		if label == "" {
			label = key
		}
		if redactAPIKey {
			key = helper.RedactSensitiveValue(key)
			label = helper.RedactSensitiveValue(label)
		}
		result = append(result, usageOverviewRealtimeUsageTopItem{
			Key:      key,
			Label:    label,
			Tokens:   item.Tokens,
			Requests: item.Requests,
			Cost:     item.CostUSD,
			Share:    item.Share,
		})
	}
	return result
}

func cloneInt64Map(source map[string]int64) map[string]int64 {
	if len(source) == 0 {
		return map[string]int64{}
	}
	cloned := make(map[string]int64, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneFloat64Map(source map[string]float64) map[string]float64 {
	if len(source) == 0 {
		return map[string]float64{}
	}
	cloned := make(map[string]float64, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneFloat64PtrMap(source map[string]*float64) map[string]*float64 {
	if len(source) == 0 {
		return map[string]*float64{}
	}
	cloned := make(map[string]*float64, len(source))
	for key, value := range source {
		if value == nil {
			cloned[key] = nil
			continue
		}
		copied := *value
		cloned[key] = &copied
	}
	return cloned
}
