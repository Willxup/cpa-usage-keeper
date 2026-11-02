package api

import (
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func listScopedCPAAPIKeyOptions(c *gin.Context, usage service.UsageProvider, scope string) {
	var filter servicedto.UsageFilter
	var err error
	now := timeutil.NormalizeStorageTime(time.Now())
	// 候选集忽略 api_key_id/model/source/result，范围限制则与对应页面保持一致。
	switch scope {
	case "overview", "analysis":
		filter, err = parseUsageTimeFilterQueryWithOptions(c.Request, now, false, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: timeutil.LongCustomDayRangeMaxDays})
	case "request-events":
		filter, err = parseUsageTimeFilterQueryWithOptions(c.Request, now, false, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: usageEventsCustomDayRangeMaxDays})
	case "realtime":
		filter, err = parseUsageRealtimeFilterQueryWithClientAPIKey(c.Request, now, false)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid API key options scope"})
		return
	}
	if err != nil {
		writeUsageFilterParseError(c, err)
		return
	}
	provider, ok := usage.(service.UsageAPIKeyOptionsProvider)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "scoped API key options are not configured"})
		return
	}
	keys, err := provider.ListUsageAPIKeyOptions(c.Request.Context(), scope, filter)
	if err != nil {
		writeInternalError(c, "list scoped API key options failed", err)
		return
	}
	options := make([]cpaAPIKeyOption, 0, len(keys))
	for _, key := range keys {
		options = append(options, toCPAAPIKeyOption(key))
	}
	c.JSON(http.StatusOK, cpaAPIKeyOptionsResponse{Options: options})
}
