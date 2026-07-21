package api

import (
	"fmt"
	"net/http"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

type usageActivityResponse struct {
	Window        string               `json:"window"`
	Grain         string               `json:"grain"`
	Timezone      string               `json:"timezone"`
	Rows          int                  `json:"rows"`
	Columns       int                  `json:"columns"`
	BucketSeconds int64                `json:"bucket_seconds"`
	WindowStart   time.Time            `json:"window_start"`
	WindowEnd     time.Time            `json:"window_end"`
	TotalSuccess  int64                `json:"total_success"`
	TotalFailure  int64                `json:"total_failure"`
	SuccessRate   float64              `json:"success_rate"`
	Blocks        []usageActivityBlock `json:"blocks"`
}

type usageActivityBlock struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Success   int64     `json:"success"`
	Failure   int64     `json:"failure"`
	Rate      float64   `json:"rate"`
}

func parseUsageActivityFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	queryNow := timeutil.NormalizeStorageTime(anchor)
	filter, err := parseUsageTimeFilterQuery(req, queryNow)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	filter.QueryNow = &queryNow
	return filter, nil
}

func parseKeyUsageActivityFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	queryNow := timeutil.NormalizeStorageTime(anchor)
	filter, err := parseKeyUsageTimeFilterQuery(req, queryNow)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	filter.QueryNow = &queryNow
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
			StartTime: block.StartTime,
			EndTime:   block.EndTime,
			Success:   block.Success,
			Failure:   block.Failure,
			Rate:      block.Rate,
		}
	}
	return usageActivityResponse{
		Window:        string(activity.Window),
		Grain:         activity.Grain,
		Timezone:      time.Local.String(),
		Rows:          activity.Rows,
		Columns:       activity.Columns,
		BucketSeconds: activity.BucketSeconds,
		WindowStart:   activity.WindowStart,
		WindowEnd:     activity.WindowEnd,
		TotalSuccess:  activity.TotalSuccess,
		TotalFailure:  activity.TotalFailure,
		SuccessRate:   activity.SuccessRate,
		Blocks:        blocks,
	}
}
