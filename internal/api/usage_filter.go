package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
)

var presetUsageRangeDurations = map[string]time.Duration{
	"4h":  4 * time.Hour,
	"8h":  8 * time.Hour,
	"12h": 12 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

var allowedUsageEventsPageSizes = map[int]struct{}{
	20:   {},
	50:   {},
	100:  {},
	500:  {},
	1000: {},
}

func parseUsageTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	filter, err := parseUsageFilterQuery(req, anchor)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	filter.Limit = 0
	filter.Page = 0
	filter.PageSize = 0
	filter.Offset = 0
	filter.Model = ""
	filter.Source = ""
	filter.AuthIndex = ""
	filter.Result = ""
	return filter, nil
}

func parseUsageAPIKeyID(value string) (string, error) {
	apiKeyID := strings.TrimSpace(value)
	if apiKeyID == "" {
		return "", nil
	}
	parsedID, err := strconv.ParseInt(apiKeyID, 10, 64)
	if err != nil || parsedID <= 0 {
		return "", fmt.Errorf("invalid api_key_id %q", apiKeyID)
	}
	return apiKeyID, nil
}

func parseCustomUsageRangeBoundary(value string, endOfDay bool) (time.Time, error) {
	if date, err := time.ParseInLocation(time.DateOnly, value, time.Local); err == nil {
		if endOfDay {
			return date.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
		}
		return date, nil
	}
	return time.Parse(time.RFC3339, value)
}

func parseUsageFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	if req == nil {
		return servicedto.UsageFilter{}, nil
	}

	rangeValue := strings.TrimSpace(req.URL.Query().Get("range"))
	if rangeValue == "" {
		return servicedto.UsageFilter{}, fmt.Errorf("usage range is required")
	}

	filter := servicedto.UsageFilter{Range: rangeValue, Limit: servicedto.DefaultUsageEventsLimit, Page: 1, PageSize: servicedto.DefaultUsageEventsLimit}
	query := req.URL.Query()
	if pageValue := strings.TrimSpace(query.Get("page")); pageValue != "" {
		page, err := strconv.Atoi(pageValue)
		if err != nil || page < 1 {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid page %q", pageValue)
		}
		filter.Page = page
	}
	pageSizeValue := strings.TrimSpace(query.Get("page_size"))
	if pageSizeValue == "" {
		pageSizeValue = strings.TrimSpace(query.Get("limit"))
	}
	if pageSizeValue != "" {
		pageSize, err := strconv.Atoi(pageSizeValue)
		if err != nil {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid page_size %q", pageSizeValue)
		}
		if _, ok := allowedUsageEventsPageSizes[pageSize]; !ok {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid page_size %q", pageSizeValue)
		}
		filter.PageSize = pageSize
		filter.Limit = pageSize
	}
	filter.Offset = (filter.Page - 1) * filter.PageSize
	filter.Model = strings.TrimSpace(query.Get("model"))
	// Request Events 前端参数仍叫 source，但它的值是 usage identity；路由层会转换成 auth_index 查询。
	filter.Source = strings.TrimSpace(query.Get("source"))
	filter.AuthIndex = strings.TrimSpace(query.Get("auth_index"))
	apiKeyID, err := parseUsageAPIKeyID(query.Get("api_key_id"))
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	filter.APIKeyID = apiKeyID
	filter.Result = strings.TrimSpace(query.Get("result"))
	if filter.Result != "" && filter.Result != "success" && filter.Result != "failed" {
		return servicedto.UsageFilter{}, fmt.Errorf("invalid result %q", filter.Result)
	}
	switch rangeValue {
	case "today", "yesterday":
		localAnchor := timeutil.NormalizeStorageTime(anchor)
		localStart := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, time.Local)
		if rangeValue == "yesterday" {
			localStart = localStart.AddDate(0, 0, -1)
		}
		startTime := timeutil.NormalizeStorageTime(localStart)
		endTime := timeutil.NormalizeStorageTime(localStart.AddDate(0, 0, 1).Add(-time.Nanosecond))
		filter.StartTime = &startTime
		filter.EndTime = &endTime
		return filter, nil
	case "custom":
		startValue := strings.TrimSpace(req.URL.Query().Get("start"))
		endValue := strings.TrimSpace(req.URL.Query().Get("end"))
		if startValue == "" || endValue == "" {
			return servicedto.UsageFilter{}, fmt.Errorf("custom range requires start and end")
		}
		startTime, err := parseCustomUsageRangeBoundary(startValue, false)
		if err != nil {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid start: %w", err)
		}
		endTime, err := parseCustomUsageRangeBoundary(endValue, true)
		if err != nil {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid end: %w", err)
		}
		startTime = timeutil.NormalizeStorageTime(startTime)
		endTime = timeutil.NormalizeStorageTime(endTime)
		if startTime.After(endTime) {
			return servicedto.UsageFilter{}, fmt.Errorf("custom range start must be before end")
		}
		if retentionStart, ok := usageFilterRetentionStart(anchor); ok && startTime.Before(retentionStart) {
			return servicedto.UsageFilter{}, fmt.Errorf("custom range start must be on or after %s", retentionStart.Format(time.DateOnly))
		}
		filter.StartTime = &startTime
		filter.EndTime = &endTime
		return filter, nil
	default:
		duration, ok := presetUsageRangeDurations[rangeValue]
		if !ok {
			return servicedto.UsageFilter{}, fmt.Errorf("unsupported usage range %q", rangeValue)
		}
		endTime := timeutil.NormalizeStorageTime(anchor)
		startTime := timeutil.NormalizeStorageTime(endTime.Add(-duration))
		filter.StartTime = &startTime
		filter.EndTime = &endTime
		return filter, nil
	}
}

func usageFilterRetentionStart(anchor time.Time) (time.Time, bool) {
	if anchor.IsZero() {
		return time.Time{}, false
	}
	localAnchor := timeutil.NormalizeStorageTime(anchor)
	currentMonthStart := time.Date(localAnchor.Year(), localAnchor.Month(), 1, 0, 0, 0, 0, time.Local)
	return currentMonthStart.AddDate(0, -1, 0), true
}

func parseUsageRealtimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	if req == nil {
		return servicedto.UsageFilter{}, nil
	}
	query := req.URL.Query()
	realtimeWindow := strings.TrimSpace(query.Get("window"))
	if realtimeWindow == "" {
		realtimeWindow = strings.TrimSpace(query.Get("realtime_window"))
	}
	if realtimeWindow == "" {
		realtimeWindow = "15m"
	}
	apiKeyID, err := parseUsageAPIKeyID(query.Get("api_key_id"))
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	filter := servicedto.UsageFilter{
		RealtimeWindow: realtimeWindow,
		APIKeyID:       apiKeyID,
	}
	switch realtimeWindow {
	case "15m", "30m", "60m":
		realtimeEndTime := timeutil.NormalizeStorageTime(anchor)
		filter.RealtimeEndTime = &realtimeEndTime
		return filter, nil
	default:
		return servicedto.UsageFilter{}, fmt.Errorf("unsupported realtime window %q", realtimeWindow)
	}
}
