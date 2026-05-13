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
		rangeValue = "all"
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
	filter.Source = strings.TrimSpace(query.Get("source"))
	filter.AuthIndex = strings.TrimSpace(query.Get("auth_index"))
	filter.Result = strings.TrimSpace(query.Get("result"))
	if filter.Result != "" && filter.Result != "success" && filter.Result != "failed" {
		return servicedto.UsageFilter{}, fmt.Errorf("invalid result %q", filter.Result)
	}
	switch rangeValue {
	case "all":
		return filter, nil
	case "today":
		localAnchor := timeutil.NormalizeStorageTime(anchor)
		localStart := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, time.Local)
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
