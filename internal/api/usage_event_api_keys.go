package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

func registerUsageEventAPIKeyFilterRoute(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage/events/filters/api-keys", func(c *gin.Context) {
		setNoStoreHeaders(c)
		// Use the event window and dimensions, excluding the key itself and all
		// pagination controls, even when callers include stale/invalid values.
		filter, err := parseUsageTimeFilterQueryWithOptions(c.Request, timeutil.NormalizeStorageTime(time.Now()), false,
			timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: usageEventsCustomDayRangeMaxDays})
		if err != nil {
			writeUsageFilterParseError(c, err)
			return
		}
		filter, err = parseUsageEventDimensionsQuery(c.Request, filter)
		if err != nil {
			writeUsageFilterParseError(c, err)
			return
		}
		if err := applyUsageEventsSourceFilter(&filter); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		options := make([]cpaAPIKeyOption, 0)
		if provider, ok := usageProvider.(service.UsageEventAPIKeyFilterProvider); ok {
			rows, err := provider.ListUsageEventAPIKeyFilterOptions(c.Request.Context(), filter)
			if err != nil {
				writeInternalError(c, "list usage event API key filter options failed", err)
				return
			}
			for _, row := range rows {
				options = append(options, toCPAAPIKeyOption(row))
			}
		}
		c.JSON(http.StatusOK, cpaAPIKeyOptionsResponse{Options: options})
	})
}
