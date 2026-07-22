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

var allowedUsageEventsPageSizes = map[int]struct{}{
	20:   {},
	50:   {},
	100:  {},
	500:  {},
	1000: {},
}

// parseUsageTimeFilterQuery 只解析通用时间条件和 Admin API Key scope，不读取 Events 专属参数。
func parseUsageTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithClientAPIKey(req, anchor, true)
}

// parseKeyUsageTimeFilterQuery 复用公共时间解析，但完全忽略客户端 api_key_id；Key Viewer 身份只由 session 注入。
func parseKeyUsageTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithClientAPIKey(req, anchor, false)
}

// Overview 的 Custom 日范围只依赖 daily 汇总，因此可以独立放开历史跨度。
func parseUsageOverviewTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, true, timeutil.UsageQueryRangeOptions{AllowLongCustomDayRange: true})
}

func parseKeyUsageOverviewTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, false, timeutil.UsageQueryRangeOptions{AllowLongCustomDayRange: true})
}

func parseUsageTimeFilterQueryWithClientAPIKey(req *http.Request, anchor time.Time, includeClientAPIKey bool) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, includeClientAPIKey, timeutil.UsageQueryRangeOptions{})
}

func parseUsageTimeFilterQueryWithOptions(req *http.Request, anchor time.Time, includeClientAPIKey bool, options timeutil.UsageQueryRangeOptions) (servicedto.UsageFilter, error) {
	if req == nil {
		return servicedto.UsageFilter{}, nil
	}
	query := req.URL.Query()
	normalizedRange, err := timeutil.ParseUsageQueryRangeWithOptions(
		query.Get("range"),
		query.Get("unit"),
		query.Get("start"),
		query.Get("end"),
		anchor,
		options,
	)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	startTime := normalizedRange.StartTime
	endTime := normalizedRange.EndTime
	filter := servicedto.UsageFilter{
		Range:        normalizedRange.Range,
		RangeUnit:    string(normalizedRange.Unit),
		RangeCount:   normalizedRange.Count,
		StartTime:    &startTime,
		EndTime:      &endTime,
		EndExclusive: normalizedRange.EndExclusive,
	}
	if normalizedRange.Range == "custom" {
		filter.CustomUnit = string(normalizedRange.Unit)
	}
	if includeClientAPIKey {
		apiKeyID, err := parseUsageAPIKeyID(query.Get("api_key_id"))
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
		filter.APIKeyID = apiKeyID
	}
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

func parseUsageFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	if req == nil {
		return servicedto.UsageFilter{}, nil
	}
	filter, err := parseUsageTimeFilterQuery(req, anchor)
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	query := req.URL.Query()
	filter.Limit = servicedto.DefaultUsageEventsLimit
	filter.Page = 1
	filter.PageSize = servicedto.DefaultUsageEventsLimit
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
	filter.Result = strings.TrimSpace(query.Get("result"))
	if filter.Result != "" && filter.Result != "success" && filter.Result != "failed" {
		return servicedto.UsageFilter{}, fmt.Errorf("invalid result %q", filter.Result)
	}
	return filter, nil
}

func parseUsageRealtimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageRealtimeFilterQueryWithClientAPIKey(req, anchor, true)
}

// parseKeyUsageRealtimeFilterQuery 只解析 Realtime window，不读取客户端 api_key_id。
func parseKeyUsageRealtimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageRealtimeFilterQueryWithClientAPIKey(req, anchor, false)
}

func parseUsageRealtimeFilterQueryWithClientAPIKey(req *http.Request, anchor time.Time, includeClientAPIKey bool) (servicedto.UsageFilter, error) {
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
	apiKeyID := ""
	if includeClientAPIKey {
		var err error
		apiKeyID, err = parseUsageAPIKeyID(query.Get("api_key_id"))
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
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
