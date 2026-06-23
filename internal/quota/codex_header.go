package quota

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const codexHeaderPrefix = "X-Codex-"

func parseCodexHeaderQuota(headers http.Header) (ProviderOutput, bool) {
	headers = canonicalQuotaHeaders(headers)
	usage := &CodexUsagePayload{
		PlanType: strings.TrimSpace(firstHeaderValue(headers, "X-Codex-Plan-Type")),
	}
	usage.RateLimit = parseCodexHeaderRateLimit(headers, codexHeaderPrefix)
	usage.AdditionalRateLimits = parseCodexAdditionalHeaderLimits(headers)
	if usage.RateLimit == nil && len(usage.AdditionalRateLimits) == 0 {
		return ProviderOutput{}, false
	}
	return ProviderOutput{Provider: "codex", Result: CodexResult{Usage: usage}}, true
}

func canonicalQuotaHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return nil
	}
	canonical := make(http.Header, len(headers))
	for key, values := range headers {
		canonicalKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if canonicalKey == "" {
			continue
		}
		for _, value := range values {
			canonical.Add(canonicalKey, value)
		}
	}
	return canonical
}

func firstHeaderValue(headers http.Header, key string) string {
	for _, value := range headers.Values(key) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseCodexHeaderRateLimit(headers http.Header, prefix string) *CodexRateLimitInfo {
	primary := parseCodexHeaderUsageWindow(headers, prefix+"Primary-")
	secondary := parseCodexHeaderUsageWindow(headers, prefix+"Secondary-")
	if primary == nil && secondary == nil {
		return nil
	}
	return &CodexRateLimitInfo{
		PrimaryWindow:   primary,
		SecondaryWindow: secondary,
	}
}

func parseCodexHeaderUsageWindow(headers http.Header, prefix string) *CodexUsageWindow {
	usedPercent, ok := parseFloatHeader(headers, prefix+"Used-Percent")
	if !ok {
		return nil
	}
	windowMinutes, ok := parseIntHeader(headers, prefix+"Window-Minutes")
	if !ok || windowMinutes <= 0 {
		return nil
	}
	windowSeconds, ok := codexHeaderWindowSecondsFromMinutes(windowMinutes)
	if !ok {
		return nil
	}
	window := CodexUsageWindow{
		UsedPercent:        usedPercent,
		LimitWindowSeconds: windowSeconds,
	}
	hasResetBoundary := false
	if value, ok := parseIntHeader(headers, prefix+"Reset-After-Seconds"); ok && value >= 0 {
		window.ResetAfterSeconds = value
		hasResetBoundary = true
	}
	if value, ok := parseIntHeader(headers, prefix+"Reset-At"); ok && value > 0 {
		window.ResetAt = value
		hasResetBoundary = true
	}
	if !hasResetBoundary {
		return nil
	}
	return &window
}

func codexHeaderWindowSecondsFromMinutes(minutes int64) (int64, bool) {
	switch minutes {
	case quotaWindowFiveHourSeconds / 60:
		return quotaWindowFiveHourSeconds, true
	case quotaWindowSevenDaySeconds / 60:
		return quotaWindowSevenDaySeconds, true
	case quotaWindowThirtyDaySeconds / 60:
		return quotaWindowThirtyDaySeconds, true
	case quotaWindowAverageMonthSeconds / 60:
		return quotaWindowAverageMonthSeconds, true
	default:
		return 0, false
	}
}

func parseCodexAdditionalHeaderLimits(headers http.Header) []CodexAdditionalRateLimit {
	type additionalGroup struct {
		group     string
		limitName string
	}
	groups := make([]additionalGroup, 0)
	for key := range headers {
		if !strings.HasPrefix(key, codexHeaderPrefix) || !strings.HasSuffix(key, "-Limit-Name") {
			continue
		}
		group := strings.TrimSuffix(strings.TrimPrefix(key, codexHeaderPrefix), "-Limit-Name")
		if group == "" {
			continue
		}
		limitName := firstHeaderValue(headers, key)
		if limitName == "" {
			continue
		}
		groups = append(groups, additionalGroup{group: group, limitName: limitName})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].limitName == groups[j].limitName {
			return groups[i].group < groups[j].group
		}
		return groups[i].limitName < groups[j].limitName
	})
	limits := make([]CodexAdditionalRateLimit, 0, len(groups))
	for _, group := range groups {
		rateLimit := parseCodexHeaderRateLimit(headers, codexHeaderPrefix+group.group+"-")
		if rateLimit == nil {
			continue
		}
		limits = append(limits, CodexAdditionalRateLimit{
			LimitName:      group.limitName,
			MeteredFeature: group.limitName,
			RateLimit:      rateLimit,
		})
	}
	return limits
}

func parseFloatHeader(headers http.Header, key string) (float64, bool) {
	value := firstHeaderValue(headers, key)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

func parseIntHeader(headers http.Header, key string) (int64, bool) {
	value := firstHeaderValue(headers, key)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
