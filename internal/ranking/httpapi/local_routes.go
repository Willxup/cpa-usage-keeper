package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/ranking"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LocalLeaderboardProvider interface {
	Leaderboard(context.Context, ranking.LeaderboardPeriod, ranking.LeaderboardMetric) (ranking.Leaderboard, error)
}

type LocalProvider interface {
	LocalLeaderboardProvider
	UpdateProfile(context.Context, int64, string, uint8) (ranking.LocalProfile, error)
}

type updateLocalProfileRequest struct {
	KeyAlias string `json:"key_alias"`
	AvatarID uint8  `json:"avatar_id"`
}

// RegisterLocalRoutes 挂载管理员本地榜单读取和展示资料编辑，不复用 Community 的参与动作。
func RegisterLocalRoutes(router gin.IRoutes, provider LocalProvider) {
	registerLocalLeaderboardRoute(router, "/ranking/local/leaderboards", provider)

	router.PATCH("/ranking/local/profiles/:id", func(c *gin.Context) {
		setNoStoreHeaders(c)
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local_ranking_unavailable"})
			return
		}
		apiKeyID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil || apiKeyID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_local_ranking_profile"})
			return
		}
		var request updateLocalProfileRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_local_ranking_profile"})
			return
		}
		profile, err := provider.UpdateProfile(c.Request.Context(), apiKeyID, request.KeyAlias, request.AvatarID)
		if err != nil {
			switch {
			case errors.Is(err, ranking.ErrInvalidLocalProfile):
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_local_ranking_profile"})
			case errors.Is(err, gorm.ErrRecordNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "local_ranking_key_not_found"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "local_ranking_profile_update_failed"})
			}
			return
		}
		c.JSON(http.StatusOK, profile)
	})
}

// RegisterKeyViewerLocalRoutes 只在显式配置开启时挂载本地榜单读取。
func RegisterKeyViewerLocalRoutes(router gin.IRoutes, provider LocalLeaderboardProvider) {
	registerLocalLeaderboardRoute(router, "/key-ranking/local/leaderboards", provider)
}

func registerLocalLeaderboardRoute(router gin.IRoutes, route string, provider LocalLeaderboardProvider) {
	router.GET(route, func(c *gin.Context) {
		setNoStoreHeaders(c)
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local_ranking_unavailable"})
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
			if errors.Is(err, ranking.ErrInvalidLeaderboard) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_leaderboard_selection"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "local_ranking_failed"})
			return
		}
		c.JSON(http.StatusOK, board)
	})
}
