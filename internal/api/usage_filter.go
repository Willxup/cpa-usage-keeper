package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
)

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

const (
	customUsageRangeUnitHour = "hour"
	customUsageRangeUnitDay  = "day"
)

func parseCustomUsageDay(value string) (time.Time, error) {
	return time.ParseInLocation(time.DateOnly, value, time.Local)
}

func parseCustomUsageHour(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	parsed = timeutil.NormalizeStorageTime(parsed)
	if parsed.Minute() != 0 || parsed.Second() != 0 || parsed.Nanosecond() != 0 {
		return time.Time{}, fmt.Errorf("hour must align to the start of an hour")
	}
	return parsed, nil
}

func resolveCustomUsageRangeUnit(unitValue, startValue, endValue string) (string, error) {
	unit := strings.TrimSpace(unitValue)
	if unit == "" {
		_, startDateErr := parseCustomUsageDay(startValue)
		_, endDateErr := parseCustomUsageDay(endValue)
		if startDateErr == nil && endDateErr == nil {
			return customUsageRangeUnitDay, nil
		}
		unit = customUsageRangeUnitHour
	}
	if unit != customUsageRangeUnitHour && unit != customUsageRangeUnitDay {
		return "", fmt.Errorf("unsupported custom range unit %q", unit)
	}
	return unit, nil
}

func parseCustomUsageRange(query url.Values, anchor time.Time) (servicedto.UsageFilter, error) {
	startValue := strings.TrimSpace(query.Get("start"))
	endValue := strings.TrimSpace(query.Get("end"))
	if startValue == "" || endValue == "" {
		return servicedto.UsageFilter{}, fmt.Errorf("custom range requires start and end")
	}
	unit, err := resolveCustomUsageRangeUnit(query.Get("unit"), startValue, endValue)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}

	if unit == customUsageRangeUnitDay {
		startTime, startErr := parseCustomUsageDay(startValue)
		if startErr != nil {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid start: %w", startErr)
		}
		endDay, endErr := parseCustomUsageDay(endValue)
		if endErr != nil {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid end: %w", endErr)
		}
		if startTime.After(endDay) {
			return servicedto.UsageFilter{}, fmt.Errorf("custom range start must be before end")
		}
		if !anchor.IsZero() {
			localAnchor := timeutil.NormalizeStorageTime(anchor)
			today := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, time.Local)
			if startTime.Before(today.AddDate(0, 0, -29)) {
				return servicedto.UsageFilter{}, fmt.Errorf("custom day range cannot start more than 30 calendar days ago")
			}
			if endDay.After(today) {
				return servicedto.UsageFilter{}, fmt.Errorf("custom day range cannot end in the future")
			}
		}
		startTime = timeutil.NormalizeStorageTime(startTime)
		endTime := timeutil.NormalizeStorageTime(endDay.AddDate(0, 0, 1))
		return servicedto.UsageFilter{Range: "custom", CustomUnit: unit, StartTime: &startTime, EndTime: &endTime, EndExclusive: true}, nil
	}

	startTime, err := parseCustomUsageHour(startValue)
	if err != nil {
		return servicedto.UsageFilter{}, fmt.Errorf("invalid start: %w", err)
	}
	endHour, err := parseCustomUsageHour(endValue)
	if err != nil {
		return servicedto.UsageFilter{}, fmt.Errorf("invalid end: %w", err)
	}
	if startTime.After(endHour) {
		return servicedto.UsageFilter{}, fmt.Errorf("custom range start must be before end")
	}
	selectedHours := int(endHour.Sub(startTime)/time.Hour) + 1
	if endHour.Sub(startTime)%time.Hour != 0 || selectedHours < 5 || selectedHours > 24 {
		return servicedto.UsageFilter{}, fmt.Errorf("custom hour range must include between 5 and 24 hours")
	}
	if !anchor.IsZero() {
		localAnchor := timeutil.NormalizeStorageTime(anchor)
		currentHour := localAnchor.Add(-time.Duration(localAnchor.Minute())*time.Minute - time.Duration(localAnchor.Second())*time.Second - time.Duration(localAnchor.Nanosecond()))
		if startTime.Before(currentHour.Add(-23 * time.Hour)) {
			return servicedto.UsageFilter{}, fmt.Errorf("custom hour range cannot start more than 24 hours ago")
		}
		if endHour.After(currentHour) {
			return servicedto.UsageFilter{}, fmt.Errorf("custom hour range cannot end in the future")
		}
	}
	endTime := timeutil.NormalizeStorageTime(endHour.Add(time.Hour))
	return servicedto.UsageFilter{Range: "custom", CustomUnit: unit, StartTime: &startTime, EndTime: &endTime, EndExclusive: true}, nil
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
		customFilter, err := parseCustomUsageRange(query, anchor)
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
		filter.CustomUnit = customFilter.CustomUnit
		filter.StartTime = customFilter.StartTime
		filter.EndTime = customFilter.EndTime
		filter.EndExclusive = customFilter.EndExclusive
		return filter, nil
	default:
		rollingRange, ok := timeutil.ParseUsageRollingRange(rangeValue)
		if !ok {
			return servicedto.UsageFilter{}, fmt.Errorf("unsupported usage range %q", rangeValue)
		}
		endTime := timeutil.NormalizeStorageTime(anchor)
		startTime := timeutil.NormalizeStorageTime(endTime.Add(-rollingRange.Duration()))
		filter.StartTime = &startTime
		filter.EndTime = &endTime
		return filter, nil
	}
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
