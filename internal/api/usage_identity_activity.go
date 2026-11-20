package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type usageIdentityActivityEventResponse struct {
	Timestamp  time.Time `json:"timestamp"`
	Failed     bool      `json:"failed"`
	StatusCode *int      `json:"status_code,omitempty"`
}

type usageIdentityActivityResponse struct {
	IdentityID    string                              `json:"identity_id"`
	Timezone      string                              `json:"timezone"`
	WindowStart   time.Time                           `json:"window_start"`
	WindowEnd     time.Time                           `json:"window_end"`
	Latest        *usageIdentityActivityEventResponse `json:"latest"`
	LastSuccess   *usageIdentityActivityEventResponse `json:"last_success"`
	LastFailure   *usageIdentityActivityEventResponse `json:"last_failure"`
	Streak        int                                 `json:"streak"`
	StreakAtLeast bool                                `json:"streak_at_least"`
}

func registerUsageIdentityActivityRoute(router gin.IRoutes, provider service.UsageIdentityProvider) {
	router.GET("/usage/identities/:id/activity", func(c *gin.Context) {
		reader, ok := provider.(service.UsageIdentityActivityReader)
		if !ok {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "usage identity activity is not configured"})
			return
		}
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid usage identity id"})
			return
		}
		activity, err := reader.GetUsageIdentityActivity(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "usage identity not found"})
				return
			}
			writeInternalError(c, "get usage identity activity failed", err)
			return
		}
		c.JSON(http.StatusOK, usageIdentityActivityResponse{
			IdentityID: strconv.FormatInt(id, 10), Timezone: time.Local.String(),
			WindowStart: activity.WindowStart, WindowEnd: activity.WindowEnd,
			Latest: mapUsageIdentityActivityEvent(activity.Latest), LastSuccess: mapUsageIdentityActivityEvent(activity.LastSuccess),
			LastFailure: mapUsageIdentityActivityEvent(activity.LastFailure), Streak: activity.Streak, StreakAtLeast: activity.StreakAtLeast,
		})
	})
}

func mapUsageIdentityActivityEvent(event *repository.UsageIdentityActivityEvent) *usageIdentityActivityEventResponse {
	if event == nil {
		return nil
	}
	return &usageIdentityActivityEventResponse{Timestamp: event.Timestamp, Failed: event.Failed, StatusCode: event.StatusCode}
}
