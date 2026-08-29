package httpapi

import (
	"context"
	"errors"
	"net/http"

	"cpa-usage-keeper/internal/ranking"
	"github.com/gin-gonic/gin"
)

type Provider interface {
	Status(context.Context) (ranking.LocalStatus, error)
	Join(context.Context, string, uint8) (ranking.LocalStatus, error)
	SyncNow(context.Context) error
	Pause(context.Context) (ranking.LocalStatus, error)
	Resume(context.Context) (ranking.LocalStatus, error)
	Exit(context.Context) (ranking.LocalStatus, error)
	Leaderboard(context.Context, ranking.LeaderboardPeriod, ranking.LeaderboardMetric) (ranking.Leaderboard, error)
	LeaderboardMetadata(context.Context) (ranking.LeaderboardMetadata, error)
}

func RegisterRoutes(router gin.IRoutes, provider Provider) {
	router.GET("/ranking/status", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		status, err := provider.Status(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ranking_status_failed"})
			return
		}
		c.JSON(http.StatusOK, status)
	})

	router.POST("/ranking/join", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		var request struct {
			DisplayName string `json:"display_name"`
			AvatarID    uint8  `json:"avatar_id"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_ranking_profile"})
			return
		}
		status, err := provider.Join(c.Request.Context(), request.DisplayName, request.AvatarID)
		if err != nil {
			writeProviderError(c, err)
			return
		}
		c.JSON(http.StatusOK, status)
	})

	router.POST("/ranking/sync", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		if err := provider.SyncNow(c.Request.Context()); err != nil {
			writeProviderError(c, err)
			return
		}
		status, err := provider.Status(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ranking_status_failed"})
			return
		}
		c.JSON(http.StatusOK, status)
	})

	router.POST("/ranking/pause", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		status, err := provider.Pause(c.Request.Context())
		if err != nil {
			writeProviderError(c, err)
			return
		}
		c.JSON(http.StatusOK, status)
	})

	router.POST("/ranking/resume", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		status, err := provider.Resume(c.Request.Context())
		if err != nil {
			writeProviderError(c, err)
			return
		}
		c.JSON(http.StatusOK, status)
	})

	router.DELETE("/ranking", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		status, err := provider.Exit(c.Request.Context())
		if err != nil {
			writeProviderError(c, err)
			return
		}
		c.JSON(http.StatusOK, status)
	})

	router.GET("/ranking/leaderboards", func(c *gin.Context) {
		setNoStoreHeaders(c)
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		query := c.Request.URL.Query()
		if len(query) != 2 || len(query["period"]) != 1 || len(query["metric"]) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_leaderboard_selection"})
			return
		}
		period := ranking.LeaderboardPeriod(query.Get("period"))
		metric := ranking.LeaderboardMetric(query.Get("metric"))
		if !validPeriod(period) || !validMetric(metric) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_leaderboard_selection"})
			return
		}
		board, err := provider.Leaderboard(c.Request.Context(), period, metric)
		if err != nil {
			writeProviderError(c, err)
			return
		}
		c.JSON(http.StatusOK, board)
	})

	router.GET("/ranking/leaderboards/metadata", func(c *gin.Context) {
		setNoStoreHeaders(c)
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ranking_unavailable"})
			return
		}
		if len(c.Request.URL.Query()) != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_metadata_request"})
			return
		}
		metadata, err := provider.LeaderboardMetadata(c.Request.Context())
		if err != nil {
			writeProviderError(c, err)
			return
		}
		c.JSON(http.StatusOK, metadata)
	})
}

func setNoStoreHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

func writeProviderError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ranking.ErrInvalidProfile):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_ranking_profile"})
	case errors.Is(err, ranking.ErrDeletedState), errors.Is(err, ranking.ErrParticipantDeleted):
		c.JSON(http.StatusGone, gin.H{"error": "ranking_participant_deleted"})
	case errors.Is(err, ranking.ErrProfileLocked):
		c.JSON(http.StatusConflict, gin.H{"error": "ranking_profile_locked"})
	case errors.Is(err, ranking.ErrSyncInProgress):
		c.JSON(http.StatusConflict, gin.H{"error": "ranking_sync_in_progress"})
	case errors.Is(err, ranking.ErrParticipation):
		c.JSON(http.StatusConflict, gin.H{"error": "ranking_participation_state_conflict"})
	case errors.Is(err, ranking.ErrIncompatibleCenter):
		c.JSON(http.StatusBadGateway, gin.H{"error": "ranking_center_incompatible"})
	default:
		var centerError *ranking.CenterError
		if errors.As(err, &centerError) && (centerError.StatusCode == http.StatusNotFound || centerError.StatusCode == http.StatusTooManyRequests || centerError.StatusCode == http.StatusServiceUnavailable) {
			if centerError.RetryAfter != "" {
				c.Header("Retry-After", centerError.RetryAfter)
			}
			c.JSON(centerError.StatusCode, gin.H{"error": "ranking_center_" + centerError.Code})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "ranking_center_request_failed"})
	}
}

func validPeriod(period ranking.LeaderboardPeriod) bool {
	switch period {
	case ranking.LeaderboardToday, ranking.LeaderboardYesterday, ranking.LeaderboardCurrentMonth, ranking.LeaderboardPreviousMonth:
		return true
	default:
		return false
	}
}

func validMetric(metric ranking.LeaderboardMetric) bool {
	switch metric {
	case ranking.MetricTotalTokens, ranking.MetricRequestCount, ranking.MetricCacheReadRate, ranking.MetricTTFTAverage, ranking.MetricLatencyAverage, ranking.MetricPeakTPM, ranking.MetricPeakRPM, ranking.MetricOverall:
		return true
	default:
		return false
	}
}
