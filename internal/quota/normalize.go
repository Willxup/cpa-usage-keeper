package quota

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/timeutil"
)

const (
	quotaWindowFiveHourSeconds     int64 = 5 * 60 * 60
	quotaWindowSevenDaySeconds     int64 = 7 * 24 * 60 * 60
	quotaWindowThirtyDaySeconds    int64 = 30 * 24 * 60 * 60
	quotaWindowAverageMonthSeconds int64 = 365 * 24 * 60 * 60 / 12
)

func NormalizeQuotaRows(output ProviderOutput) []QuotaRow {
	// 不在 provider 层强行统一原始结构，只在出口处转换为前端展示需要的 quota rows。
	switch result := output.Result.(type) {
	case AntigravityResult:
		return normalizeAntigravityQuotaRows(result)
	case *AntigravityResult:
		if result == nil {
			return nil
		}
		return normalizeAntigravityQuotaRows(*result)
	case CodexResult:
		return normalizeCodexQuotaRows(result)
	case *CodexResult:
		if result == nil {
			return nil
		}
		return normalizeCodexQuotaRows(*result)
	case GeminiCLIResult:
		return normalizeGeminiCLIQuotaRows(result)
	case *GeminiCLIResult:
		if result == nil {
			return nil
		}
		return normalizeGeminiCLIQuotaRows(*result)
	case ClaudeResult:
		return normalizeClaudeQuotaRows(result)
	case *ClaudeResult:
		if result == nil {
			return nil
		}
		return normalizeClaudeQuotaRows(*result)
	case KimiResult:
		return normalizeKimiQuotaRows(result)
	case *KimiResult:
		if result == nil {
			return nil
		}
		return normalizeKimiQuotaRows(*result)
	case XAIResult:
		return normalizeXAIQuotaRows(result)
	case *XAIResult:
		if result == nil {
			return nil
		}
		return normalizeXAIQuotaRows(*result)
	default:
		return nil
	}
}

func normalizeClaudeQuotaRows(result ClaudeResult) []QuotaRow {
	if result.Usage == nil {
		return nil
	}
	rows := make([]QuotaRow, 0, 8)
	rows = appendClaudeWindowQuotaRow(rows, "five_hour", "5h", "window", result.Usage.FiveHour)
	rows = appendClaudeWindowQuotaRow(rows, "seven_day", "Weekly", "window", result.Usage.SevenDay)
	rows = appendClaudeWindowQuotaRow(rows, "seven_day_oauth_apps", "7d OAuth Apps", "window", result.Usage.SevenDayOAuthApps)
	rows = appendClaudeWindowQuotaRow(rows, "seven_day_opus", "7d Opus", "model", result.Usage.SevenDayOpus)
	rows = appendClaudeWindowQuotaRow(rows, "seven_day_sonnet", "7d Sonnet", "model", result.Usage.SevenDaySonnet)
	rows = appendClaudeWindowQuotaRow(rows, "seven_day_cowork", "7d Cowork", "window", result.Usage.SevenDayCowork)
	rows = appendClaudeWindowQuotaRow(rows, "iguana_necktie", "Iguana Necktie", "window", result.Usage.IguanaNecktie)
	if result.Usage.ExtraUsage != nil {
		rows = append(rows, QuotaRow{
			Key:         "extra_usage",
			Label:       "Extra Usage",
			Scope:       "extra_usage",
			Used:        floatPtr(result.Usage.ExtraUsage.UsedCredits),
			Limit:       floatPtr(result.Usage.ExtraUsage.MonthlyLimit),
			UsedPercent: result.Usage.ExtraUsage.Utilization,
			Allowed:     boolPtr(result.Usage.ExtraUsage.IsEnabled),
		})
	}
	return rows
}

func appendClaudeWindowQuotaRow(rows []QuotaRow, key string, label string, scope string, window *ClaudeUsageWindow) []QuotaRow {
	if window == nil {
		return rows
	}
	row := QuotaRow{
		Key:         key,
		Label:       label,
		Scope:       scope,
		UsedPercent: floatPtr(window.Utilization),
		ResetAt:     window.ResetsAt,
	}
	// Claude 只给官方语义明确的 5h 会话窗口和 seven_day 系列补 seconds，其它未知 key 不猜测。
	if key == "five_hour" {
		row.Window = &QuotaWindow{Seconds: intPtr(quotaWindowFiveHourSeconds)}
	} else if strings.HasPrefix(key, "seven_day") {
		row.Window = &QuotaWindow{Seconds: intPtr(quotaWindowSevenDaySeconds)}
	}
	return append(rows, row)
}

func normalizeCodexQuotaRows(result CodexResult) []QuotaRow {
	// Codex 根据 limit_window_seconds 明确区分 5h/Weekly/Monthly；未知窗口回退到 primary/secondary 角色。
	if result.Usage == nil {
		return nil
	}
	rows := make([]QuotaRow, 0, 4+len(result.Usage.AdditionalRateLimits)*2)
	rows = appendCodexWindowQuotaRows(rows, "rate_limit", "5h", "Weekly", "window", "", result.Usage.RateLimit)
	rows = appendCodexWindowQuotaRows(rows, "code_review_rate_limit", "Code Review 5h", "Code Review Weekly", "code_review", "", result.Usage.CodeReviewRateLimit)
	for _, additional := range result.Usage.AdditionalRateLimits {
		// 主限额之外的 code review / spark 等窗口也保留为 extra quota，避免丢失上游数据。
		metric := additional.MeteredFeature
		if metric == "" {
			metric = additional.LimitName
		}
		primaryLabel := additional.LimitName + " 5h"
		secondaryLabel := additional.LimitName + " Weekly"
		rows = appendCodexWindowQuotaRows(rows, "additional_rate_limits."+additional.LimitName, primaryLabel, secondaryLabel, "additional", metric, additional.RateLimit)
	}
	if planType := strings.TrimSpace(result.Usage.PlanType); planType != "" {
		for index := range rows {
			rows[index].PlanType = planType
		}
	}
	return rows
}

func appendCodexWindowQuotaRows(rows []QuotaRow, keyPrefix string, primaryLabel string, secondaryLabel string, scope string, metric string, info *CodexRateLimitInfo) []QuotaRow {
	if info == nil {
		return rows
	}
	rows = appendCodexWindowQuotaRow(rows, keyPrefix+".primary_window", primaryLabel, scope, metric, info, info.PrimaryWindow)
	rows = appendCodexWindowQuotaRow(rows, keyPrefix+".secondary_window", secondaryLabel, scope, metric, info, info.SecondaryWindow)
	return rows
}

func codexWindowLabel(key string, label string, seconds int64) string {
	switch seconds {
	case quotaWindowFiveHourSeconds:
		return codexKnownWindowLabel(label, "5h")
	case quotaWindowSevenDaySeconds:
		return codexKnownWindowLabel(label, "Weekly")
	case quotaWindowThirtyDaySeconds, quotaWindowAverageMonthSeconds:
		return codexKnownWindowLabel(label, "Monthly")
	}
	return codexRoleWindowLabel(key, label)
}

func codexKnownWindowLabel(label string, replacement string) string {
	if label == "5h" || label == "Weekly" || label == "Monthly" || label == "Window" {
		return replacement
	}
	for _, candidate := range []string{"5h", "Weekly", "Monthly", "Window"} {
		if strings.Contains(label, candidate) {
			return strings.Replace(label, candidate, replacement, 1)
		}
	}
	return label
}

func codexRoleWindowLabel(key string, label string) string {
	role := codexWindowRole(key)
	if role == "" {
		return label
	}
	fallback := codexPrefixedWindowRole(key, role)
	if label == "" || label == "5h" || label == "Weekly" || label == "Monthly" || label == "Window" {
		return fallback
	}
	for _, candidate := range []string{"5h", "Weekly", "Monthly", "Window"} {
		if strings.Contains(label, candidate) {
			return strings.Replace(label, candidate, role, 1)
		}
	}
	return fallback
}

func codexWindowRole(key string) string {
	if strings.HasSuffix(key, ".primary_window") {
		return "Primary"
	}
	if strings.HasSuffix(key, ".secondary_window") {
		return "Secondary"
	}
	return ""
}

func codexPrefixedWindowRole(key string, role string) string {
	if strings.HasPrefix(key, "code_review_rate_limit.") {
		return "Code Review " + role
	}
	if strings.HasPrefix(key, "additional_rate_limits.") {
		name := strings.TrimPrefix(key, "additional_rate_limits.")
		name = strings.TrimSuffix(name, ".primary_window")
		name = strings.TrimSuffix(name, ".secondary_window")
		if name != "" {
			return name + " " + role
		}
	}
	return role
}

func appendCodexWindowQuotaRow(rows []QuotaRow, key string, label string, scope string, metric string, info *CodexRateLimitInfo, window *CodexUsageWindow) []QuotaRow {
	// 每个窗口单独展开为一条 quota row，前端再按窗口秒数决定主/次展示位置。
	if window == nil {
		return rows
	}
	label = codexWindowLabel(key, label, window.LimitWindowSeconds)
	row := QuotaRow{
		Key:               key,
		Label:             label,
		Scope:             scope,
		Metric:            metric,
		UsedPercent:       floatPtr(window.UsedPercent),
		Allowed:           info.Allowed,
		LimitReached:      info.LimitReached,
		WindowUsageTokens: window.WindowUsageTokens,
		WindowUsageCost:   window.WindowUsageCost,
	}
	if window.LimitWindowSeconds != 0 {
		row.Window = &QuotaWindow{Seconds: intPtr(window.LimitWindowSeconds)}
	}
	if window.ResetAfterSeconds != 0 {
		row.ResetAfterSeconds = intPtr(window.ResetAfterSeconds)
	}
	if window.ResetAt != 0 {
		row.ResetAt = timeutil.FormatStorageTime(time.Unix(window.ResetAt, 0))
	}
	return append(rows, row)
}

func normalizeGeminiCLIQuotaRows(result GeminiCLIResult) []QuotaRow {
	// Gemini CLI 同时可能返回模型桶和 Code Assist credits，两类都平铺给前端。
	rows := make([]QuotaRow, 0)
	if result.Quota != nil {
		for _, bucket := range result.Quota.Buckets {
			rows = append(rows, QuotaRow{
				Key:               "bucket." + bucket.ModelID + "." + bucket.TokenType,
				Label:             bucket.ModelID,
				Scope:             "model",
				Metric:            bucket.TokenType,
				Remaining:         floatPtr(bucket.RemainingAmount),
				RemainingFraction: floatPtr(bucket.RemainingFraction),
				ResetAt:           bucket.ResetTime,
			})
		}
	}
	if result.CodeAssist != nil {
		rows = appendGeminiCLICredits(rows, "code_assist.current_tier", result.CodeAssist.CurrentTier)
		rows = appendGeminiCLICredits(rows, "code_assist.paid_tier", result.CodeAssist.PaidTier)
	}
	return rows
}

func appendGeminiCLICredits(rows []QuotaRow, keyPrefix string, tier *GeminiCliUserTier) []QuotaRow {
	if tier == nil {
		return rows
	}
	for _, credit := range tier.AvailableCredits {
		rows = append(rows, QuotaRow{
			Key:       keyPrefix + "." + credit.CreditType,
			Label:     "Code Assist Credit",
			Scope:     "credits",
			Metric:    credit.CreditType,
			Remaining: floatPtr(credit.CreditAmount),
		})
	}
	return rows
}

func normalizeAntigravityQuotaRows(result AntigravityResult) []QuotaRow {
	if result.Quota == nil {
		return nil
	}
	keys := make([]string, 0, len(result.Quota.Models))
	for key := range result.Quota.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]QuotaRow, 0, len(keys))
	for _, key := range keys {
		model := result.Quota.Models[key]
		label := model.DisplayName
		if label == "" {
			label = key
		}
		row := QuotaRow{Key: "model." + key, Label: label, Scope: "model", Metric: key}
		if model.QuotaInfo != nil {
			// Antigravity 模型限额按 5 小时刷新，只有存在 quota info 时才让该 row 进入窗口统计。
			row.Window = &QuotaWindow{Seconds: intPtr(quotaWindowFiveHourSeconds)}
			row.Remaining = floatPtr(model.QuotaInfo.Remaining)
			row.RemainingFraction = floatPtr(model.QuotaInfo.RemainingFraction)
			row.ResetAt = model.QuotaInfo.ResetTime
		}
		rows = append(rows, row)
	}
	return rows
}

func normalizeKimiQuotaRows(result KimiResult) []QuotaRow {
	// Kimi 的 summary 和 limits 结构不同，先保留 summary，再逐条展开 limits。
	if result.Usage == nil {
		return nil
	}
	rows := make([]QuotaRow, 0, 1+len(result.Usage.Limits))
	if isMeaningfulKimiDetail(result.Usage.Usage) {
		rows = append(rows, kimiDetailQuotaRow("usage", "summary", "Usage", result.Usage.Usage))
	}
	for index, limit := range result.Usage.Limits {
		keyName := limit.Name
		if keyName == "" {
			keyName = fmt.Sprintf("%d", index)
		}
		label := firstNonEmpty(limit.Title, limit.Name, "Limit")
		scope := firstNonEmpty(limit.Scope, "limit")
		row := QuotaRow{
			Key:       "limits." + keyName,
			Label:     label,
			Scope:     scope,
			Metric:    limit.Name,
			Used:      floatPtr(limit.Used),
			Limit:     floatPtr(limit.Limit),
			Remaining: floatPtr(limit.Remaining),
			ResetAt:   firstNonEmpty(limit.ResetAt, resetAtFromKimiDetail(limit.Detail)),
		}
		if limit.Limit > 0 {
			row.UsedPercent = floatPtr(limit.Used / limit.Limit * 100)
		}
		if limit.ResetIn != 0 {
			row.ResetAfterSeconds = intPtr(int64(limit.ResetIn))
		} else if limit.Detail != nil && limit.Detail.ResetIn != 0 {
			row.ResetAfterSeconds = intPtr(int64(limit.Detail.ResetIn))
		}
		row.Window = kimiWindow(limit)
		rows = append(rows, row)
	}
	return rows
}

func normalizeXAIQuotaRows(result XAIResult) []QuotaRow {
	// xAI billing 的 val 是 USD cents；后端缓存保留 cents，前端按 metric=usd_cents 格式化成美元。
	if result.Billing == nil || result.Billing.Config == nil {
		return nil
	}
	config := result.Billing.Config
	limit := config.MonthlyLimit.Val
	used := config.Used.Val
	remaining := math.Max(0, limit-used)
	row := QuotaRow{
		Key:       "billing.monthly",
		Label:     "Monthly Spend",
		Scope:     "billing",
		Metric:    "usd_cents",
		Used:      floatPtr(used),
		Limit:     floatPtr(limit),
		Remaining: floatPtr(remaining),
		Window:    &QuotaWindow{Seconds: intPtr(quotaWindowThirtyDaySeconds)},
		ResetAt:   config.BillingPeriodEnd,
	}
	if limit > 0 {
		row.UsedPercent = floatPtr(used / limit * 100)
		limitReached := used >= limit
		row.LimitReached = boolPtr(limitReached)
		row.Allowed = boolPtr(!limitReached)
	}
	return []QuotaRow{row}
}

func kimiDetailQuotaRow(key string, scope string, fallbackLabel string, detail *KimiUsageDetail) QuotaRow {
	row := QuotaRow{
		Key:       key,
		Label:     firstNonEmpty(detail.Title, fallbackLabel),
		Scope:     scope,
		Metric:    detail.Name,
		Used:      floatPtr(detail.Used),
		Limit:     floatPtr(detail.Limit),
		Remaining: floatPtr(detail.Remaining),
		ResetAt:   detail.ResetAt,
	}
	if detail.Limit > 0 {
		row.UsedPercent = floatPtr(detail.Used / detail.Limit * 100)
	}
	if detail.ResetIn != 0 {
		row.ResetAfterSeconds = intPtr(int64(detail.ResetIn))
	}
	return row
}

func isMeaningfulKimiDetail(detail *KimiUsageDetail) bool {
	if detail == nil {
		return false
	}
	return detail.Used != 0 || detail.Limit != 0 || detail.Remaining != 0 || detail.Name != "" || detail.Title != "" || detail.ResetAt != "" || detail.ResetIn != 0 || detail.TTL != 0
}

func resetAtFromKimiDetail(detail *KimiUsageDetail) string {
	if detail == nil {
		return ""
	}
	return detail.ResetAt
}

func kimiWindow(limit KimiLimitItem) *QuotaWindow {
	if limit.Window != nil {
		return quotaWindowFromDurationUnit(limit.Window.Duration, limit.Window.TimeUnit)
	}
	if limit.Duration != 0 || limit.TimeUnit != "" {
		return quotaWindowFromDurationUnit(limit.Duration, limit.TimeUnit)
	}
	return nil
}

func quotaWindowFromDurationUnit(duration int64, unit string) *QuotaWindow {
	window := &QuotaWindow{Duration: floatPtr(float64(duration)), Unit: unit}
	// Kimi 返回显式 duration/unit 时才换算 seconds；未知单位保留原字段但不参与窗口用量统计。
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "second", "seconds", "s":
		window.Seconds = intPtr(int64(duration))
	case "minute", "minutes", "m":
		window.Seconds = intPtr(int64(duration) * 60)
	case "hour", "hours", "h":
		window.Seconds = intPtr(int64(duration) * 3600)
	case "day", "days", "d":
		window.Seconds = intPtr(int64(duration) * 86400)
	case "week", "weeks", "w":
		window.Seconds = intPtr(int64(duration) * 604800)
	}
	return window
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func floatPtr(value float64) *float64 {
	return &value
}

func intPtr(value int64) *int64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
