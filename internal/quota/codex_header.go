package quota

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const codexHeaderPrefix = "X-Codex-"

// codexDecodedHeaderAdditional 保存一次 Header 解码得到的 Additional group，history 永远不遍历它。
type codexDecodedHeaderAdditional struct {
	// Group 是 X-Codex-{group}-* 中的稳定 Header group 名称，仅供读取同组窗口。
	Group string
	// LimitName 是上游展示/计量名称，沿用现有 cache key 语义。
	LimitName string
	// PrimaryWindow 是该 Additional group 的通用 Primary 解码结果。
	PrimaryWindow *CodexUsageWindow
	// SecondaryWindow 是该 Additional group 的通用 Secondary 解码结果。
	SecondaryWindow *CodexUsageWindow
}

// codexDecodedHeaderQuota 是一次性 Header 数值解码结果，可分别投影为 cache 和主额度历史。
type codexDecodedHeaderQuota struct {
	// PlanType 是 X-Codex-Plan-Type，用于现有 subscription cache 输出。
	PlanType string
	// PrimaryWindow 是无 group 主额度 Primary，允许 cache 未知的正窗口秒数。
	PrimaryWindow *CodexUsageWindow
	// SecondaryWindow 是无 group 主额度 Secondary，允许 cache 未知的正窗口秒数。
	SecondaryWindow *CodexUsageWindow
	// Additional 保存 cache 所需 group；history 投影不会读取该切片。
	Additional []codexDecodedHeaderAdditional
}

func parseCodexHeaderQuota(headers http.Header) (ProviderOutput, bool) {
	// 兼容入口复用共享单次解码层，只返回现有 cache 合同投影。
	decoded, ok := decodeCodexHeaderQuota(headers)
	if !ok {
		return ProviderOutput{}, false
	}
	return decoded.cacheOutput()
}

func decodeCodexHeaderQuota(headers http.Header) (codexDecodedHeaderQuota, bool) {
	// 先执行一次白名单 canonical/filter；异步快照不会保存返回 map。
	filtered := codexQuotaSnapshotHeaders(headers)
	if len(filtered) == 0 {
		return codexDecodedHeaderQuota{}, false
	}
	// 主额度两个窗口各解析一次，通用结果同时服务 cache/history 投影。
	decoded := codexDecodedHeaderQuota{
		PlanType:        strings.TrimSpace(firstHeaderValue(filtered, "X-Codex-Plan-Type")),
		PrimaryWindow:   parseCodexDecodedHeaderUsageWindow(filtered, codexHeaderPrefix+"Primary-"),
		SecondaryWindow: parseCodexDecodedHeaderUsageWindow(filtered, codexHeaderPrefix+"Secondary-"),
	}
	// Additional 每个 group 同样只解析一次，结果只进入现有 cache 投影。
	decoded.Additional = parseCodexDecodedAdditionalHeaderLimits(filtered)
	if decoded.PrimaryWindow == nil && decoded.SecondaryWindow == nil && len(decoded.Additional) == 0 {
		return codexDecodedHeaderQuota{}, false
	}
	return decoded, true
}

func (decoded codexDecodedHeaderQuota) cacheOutput() (ProviderOutput, bool) {
	// cache 主额度继续只接受既有已知窗口和非负 used percent，不因 history 放宽协议范围。
	usage := &CodexUsagePayload{PlanType: decoded.PlanType}
	usage.RateLimit = codexDecodedCacheRateLimit(decoded.PrimaryWindow, decoded.SecondaryWindow)
	// Additional group 按已排序解码顺序投影，保持既有 quota row 稳定顺序。
	for _, additional := range decoded.Additional {
		rateLimit := codexDecodedCacheRateLimit(additional.PrimaryWindow, additional.SecondaryWindow)
		if rateLimit == nil {
			continue
		}
		usage.AdditionalRateLimits = append(usage.AdditionalRateLimits, CodexAdditionalRateLimit{
			LimitName:      additional.LimitName,
			MeteredFeature: additional.LimitName,
			RateLimit:      rateLimit,
		})
	}
	// 与旧 parser 一致：没有任何 cache 可消费窗口时返回 false，即使 plan type 存在。
	if usage.RateLimit == nil && len(usage.AdditionalRateLimits) == 0 {
		return ProviderOutput{}, false
	}
	return ProviderOutput{Provider: "codex", Result: CodexResult{Usage: usage}}, true
}

func (decoded codexDecodedHeaderQuota) mainQuotaOutput() ProviderOutput {
	// history 投影只暴露无 group Primary/Secondary，Additional 从结构上无法进入遍历。
	rateLimit := &CodexRateLimitInfo{
		PrimaryWindow:   cloneCodexUsageWindow(decoded.PrimaryWindow),
		SecondaryWindow: cloneCodexUsageWindow(decoded.SecondaryWindow),
	}
	if rateLimit.PrimaryWindow == nil && rateLimit.SecondaryWindow == nil {
		rateLimit = nil
	}
	return ProviderOutput{
		Provider: "codex",
		Result: CodexResult{Usage: &CodexUsagePayload{
			RateLimit: rateLimit,
		}},
	}
}

func codexDecodedCacheRateLimit(primary *CodexUsageWindow, secondary *CodexUsageWindow) *CodexRateLimitInfo {
	// 两个窗口分别执行 cache 兼容过滤；一个无效不能阻止另一个合法窗口更新。
	cachePrimary := codexDecodedCacheWindow(primary)
	cacheSecondary := codexDecodedCacheWindow(secondary)
	if cachePrimary == nil && cacheSecondary == nil {
		return nil
	}
	return &CodexRateLimitInfo{PrimaryWindow: cachePrimary, SecondaryWindow: cacheSecondary}
}

func codexDecodedCacheWindow(window *CodexUsageWindow) *CodexUsageWindow {
	// 旧 parser 拒绝负 used percent；history 仍可在独立投影保留有限超界 raw。
	if window == nil || window.UsedPercent < 0 {
		return nil
	}
	// 旧 cache 只识别 5h、7d、30d 和平均月，未知正窗口只能进入 history。
	if !codexHeaderWindowSecondsSupportedByCache(window.LimitWindowSeconds) {
		return nil
	}
	return cloneCodexUsageWindow(window)
}

func parseCodexDecodedHeaderUsageWindow(headers http.Header, prefix string) *CodexUsageWindow {
	// used percent 必须存在且有限；负数只在 cache 投影阶段按旧规则过滤。
	usedPercent, ok := parseFiniteFloatHeader(headers, prefix+"Used-Percent")
	if !ok {
		return nil
	}
	// Window-Minutes 先确保正数和乘 60 不溢出，再保存上游原始秒数。
	windowMinutes, ok := parseIntHeader(headers, prefix+"Window-Minutes")
	if !ok || windowMinutes <= 0 || windowMinutes > math.MaxInt64/60 {
		return nil
	}
	window := &CodexUsageWindow{
		UsedPercent:           usedPercent,
		LimitWindowSeconds:    windowMinutes * 60,
		HasUsedPercent:        true,
		HasLimitWindowSeconds: true,
	}
	// 重置倒计时允许明确零秒；负数没有合法边界。
	if value, ok := parseIntHeader(headers, prefix+"Reset-After-Seconds"); ok && value >= 0 {
		window.ResetAfterSeconds = value
		window.HasResetAfterSeconds = true
	}
	// 上游直接返回的重置时刻只接受正 Unix 秒，并在历史构造时优先于倒计时推算值。
	if value, ok := parseIntHeader(headers, prefix+"Reset-At"); ok && value > 0 {
		window.ResetAt = value
		window.HasResetAt = true
	}
	if !window.HasResetAfterSeconds && !window.HasResetAt {
		return nil
	}
	return window
}

func parseCodexDecodedAdditionalHeaderLimits(headers http.Header) []codexDecodedHeaderAdditional {
	// 先收集 group 与 limit name，避免遍历窗口字段时把 Primary/Secondary 误当 Additional。
	groups := make([]codexDecodedHeaderAdditional, 0)
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
		groups = append(groups, codexDecodedHeaderAdditional{Group: group, LimitName: limitName})
	}
	// 先按展示名再按 group 排序，保持旧 parser 产生 quota rows 的稳定顺序。
	sort.Slice(groups, func(i int, j int) bool {
		if groups[i].LimitName == groups[j].LimitName {
			return groups[i].Group < groups[j].Group
		}
		return groups[i].LimitName < groups[j].LimitName
	})
	// 每个 group 的两个窗口只执行一次数值解析，随后 cache 投影复用结构化结果。
	decoded := make([]codexDecodedHeaderAdditional, 0, len(groups))
	for _, group := range groups {
		prefix := codexHeaderPrefix + group.Group + "-"
		group.PrimaryWindow = parseCodexDecodedHeaderUsageWindow(headers, prefix+"Primary-")
		group.SecondaryWindow = parseCodexDecodedHeaderUsageWindow(headers, prefix+"Secondary-")
		if group.PrimaryWindow == nil && group.SecondaryWindow == nil {
			continue
		}
		decoded = append(decoded, group)
	}
	return decoded
}

func codexHeaderWindowSecondsSupportedByCache(seconds int64) bool {
	// cache 合同只接受当前四个已知原始秒数，history 分类规则独立处理未知正值。
	switch seconds {
	case quotaWindowFiveHourSeconds, quotaWindowSevenDaySeconds, quotaWindowThirtyDaySeconds, quotaWindowAverageMonthSeconds:
		return true
	default:
		return false
	}
}

func codexHeaderWindowSecondsFromMinutes(minutes int64) (int64, bool) {
	// 保留旧 helper 合同供定向测试和兼容调用；内部共享解码先保存通用正秒数。
	if minutes <= 0 || minutes > math.MaxInt64/60 {
		return 0, false
	}
	seconds := minutes * 60
	if !codexHeaderWindowSecondsSupportedByCache(seconds) {
		return 0, false
	}
	return seconds, true
}

func firstHeaderValue(headers http.Header, key string) string {
	// 快照过滤层已经把每个 key 收敛为单值，但保留循环兼容直接 parser 调用。
	for _, value := range headers.Values(key) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseFiniteFloatHeader(headers http.Header, key string) (float64, bool) {
	// Header 数值允许字符串小数；空值和解析失败直接视为当前窗口缺失。
	value := firstHeaderValue(headers, key)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func parseIntHeader(headers http.Header, key string) (int64, bool) {
	// reset/window 字段必须是完整十进制整数，禁止浮点截断改变周期身份。
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

func cloneCodexUsageWindow(window *CodexUsageWindow) *CodexUsageWindow {
	// nil 保持缺失窗口语义；非 nil 只含值字段和只读标量指针，可以复制 struct 所有权。
	if window == nil {
		return nil
	}
	cloned := *window
	return &cloned
}
