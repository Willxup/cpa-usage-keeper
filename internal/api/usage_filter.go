package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/gin-gonic/gin"
)

var allowedUsageEventsPageSizes = map[int]struct{}{
	20:  {},
	50:  {},
	100: {},
}

const usageEventsCustomDayRangeMaxDays = 90

func writeUsageFilterParseError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if timeutil.IsUsageQueryRangeBoundsConflict(err) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"error": err.Error()})
}

// parseUsageTimeFilterQuery 只解析通用时间条件和 Admin API Key scope，不读取 Events 专属参数。
func parseUsageTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithClientAPIKey(req, anchor, true)
}

// parseKeyUsageTimeFilterQuery 复用公共时间解析，但完全忽略客户端 api_key_id；Key Viewer 身份只由 session 注入。
func parseKeyUsageTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithClientAPIKey(req, anchor, false)
}

// Overview 的 Custom 日范围只依赖 daily 汇总，因此可以放宽至统一的一年上限。
func parseUsageOverviewTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, true, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: timeutil.LongCustomDayRangeMaxDays})
}

func parseKeyUsageOverviewTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, false, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: timeutil.LongCustomDayRangeMaxDays})
}

// Events 直接查询热表原始事件，Custom 日范围与 90 天保留窗口保持一致。
func parseUsageEventsTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, true, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: usageEventsCustomDayRangeMaxDays})
}

// Analysis 主数据来自 hourly/daily 汇总，因此和 Overview 一样放宽至统一的一年上限。
func parseUsageAnalysisTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, true, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: timeutil.LongCustomDayRangeMaxDays})
}

func parseKeyUsageAnalysisTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageTimeFilterQueryWithOptions(req, anchor, false, timeutil.UsageQueryRangeOptions{MaxCustomDayRangeDays: timeutil.LongCustomDayRangeMaxDays})
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
func encodeUsageEventsCursor(timestamp time.Time, id int64) string {
	payload := timeutil.NormalizeStorageTime(timestamp).Format(time.RFC3339Nano) + "|" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeUsageEventsCursor(value string) (time.Time, int64, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	payload := string(decoded)
	separator := strings.LastIndexByte(payload, '|')
	if separator <= 0 || separator >= len(payload)-1 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, payload[:separator])
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	id, err := strconv.ParseInt(payload[separator+1:], 10, 64)
	if err != nil || id <= 0 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	return timeutil.NormalizeStorageTime(timestamp), id, nil
}

func parseUsageFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageFilterQueryWithLatestIdentity(req, anchor, true)
}

// Export 保持 Request Events 原有的必选时间范围，不消费详情抽屉的无范围查询例外。
func parseUsageExportFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error) {
	return parseUsageFilterQueryWithLatestIdentity(req, anchor, false)
}

func parseUsageFilterQueryWithLatestIdentity(req *http.Request, anchor time.Time, allowLatestIdentity bool) (servicedto.UsageFilter, error) {
	if req == nil {
		return servicedto.UsageFilter{}, nil
	}
	query := req.URL.Query()
	var filter servicedto.UsageFilter
	latestIdentityCursorQuery := allowLatestIdentity && isLatestIdentityCursorQuery(req)
	if latestIdentityCursorQuery {
		apiKeyID, err := parseUsageAPIKeyID(query.Get("api_key_id"))
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
		filter.APIKeyID = apiKeyID
		// 详情只消费游标和 has_more，不为最新请求列表扫描全部历史总数。
		filter.SkipTotalCount = true
	} else {
		var err error
		filter, err = parseUsageEventsTimeFilterQuery(req, anchor)
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
	}
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
	cursorModeValue := strings.TrimSpace(query.Get("cursor_mode"))
	if cursorModeValue != "" {
		cursorMode, err := strconv.ParseBool(cursorModeValue)
		if err != nil {
			return servicedto.UsageFilter{}, fmt.Errorf("invalid cursor_mode %q", cursorModeValue)
		}
		filter.CursorMode = cursorMode
	}
	if cursorValue := strings.TrimSpace(query.Get("cursor")); cursorValue != "" {
		cursorTimestamp, cursorID, err := decodeUsageEventsCursor(cursorValue)
		if err != nil {
			return servicedto.UsageFilter{}, err
		}
		filter.CursorMode = true
		filter.CursorTimestamp = &cursorTimestamp
		filter.CursorID = cursorID
	}
	if filter.CursorMode {
		filter.Page = 1
		filter.Offset = 0
	}
	filter.Model = strings.TrimSpace(query.Get("model"))
	// Request Events 前端参数仍叫 source，但它的值是 usage identity；路由层会转换成 auth_index 查询。
	filter.Source = strings.TrimSpace(query.Get("source"))
	filter.AuthIndex = strings.TrimSpace(query.Get("auth_index"))
	authType, err := parseUsageEventsAuthType(query.Get("auth_type"))
	if err != nil {
		return servicedto.UsageFilter{}, err
	}
	filter.AuthType = authType
	filter.Result = strings.TrimSpace(query.Get("result"))
	if filter.Result != "" && filter.Result != "success" && filter.Result != "failed" {
		return servicedto.UsageFilter{}, fmt.Errorf("invalid result %q", filter.Result)
	}
	return filter, nil
}

// 详情事件只允许在身份、身份类型和游标模式都明确时省略固定时间范围。
func isLatestIdentityCursorQuery(req *http.Request) bool {
	if req == nil {
		return false
	}
	query := req.URL.Query()
	if strings.TrimSpace(query.Get("range")) != "" || strings.TrimSpace(query.Get("source")) == "" || strings.TrimSpace(query.Get("auth_type")) == "" {
		return false
	}
	cursorMode, _ := strconv.ParseBool(strings.TrimSpace(query.Get("cursor_mode")))
	return cursorMode || strings.TrimSpace(query.Get("cursor")) != ""
}

func parseUsageEventsAuthType(rawValue string) (string, error) {
	value := strings.TrimSpace(rawValue)
	if value == "" {
		return "", nil
	}
	authType, err := strconv.Atoi(value)
	if err != nil {
		return "", fmt.Errorf("auth_type must be 1 or 2")
	}
	switch entities.UsageIdentityAuthType(authType) {
	case entities.UsageIdentityAuthTypeAuthFile:
		return "oauth", nil
	case entities.UsageIdentityAuthTypeAIProvider:
		return "apikey", nil
	default:
		return "", fmt.Errorf("auth_type must be 1 or 2")
	}
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
