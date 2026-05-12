package quota

import (
	"fmt"
	"sort"
	"strings"
	"time"
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
	return append(rows, QuotaRow{
		Key:         key,
		Label:       label,
		Scope:       scope,
		UsedPercent: floatPtr(window.Utilization),
		ResetAt:     window.ResetsAt,
	})
}

func normalizeCodexQuotaRows(result CodexResult) []QuotaRow {
	// Codex 根据 limit_window_seconds 明确区分 5h/Weekly；未知窗口只标记 Window，不猜测。
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

func codexWindowLabel(label string, seconds int64) string {
	switch seconds {
	case 18000:
		if strings.Contains(label, "Weekly") {
			return strings.Replace(label, "Weekly", "5h", 1)
		}
		return label
	case 604800:
		if strings.Contains(label, "5h") {
			return strings.Replace(label, "5h", "Weekly", 1)
		}
		return label
	}
	return codexUnknownWindowLabel(label)
}

func codexUnknownWindowLabel(label string) string {
	if label == "5h" || label == "Weekly" {
		return "Window"
	}
	if strings.Contains(label, "5h") {
		return strings.Replace(label, "5h", "Window", 1)
	}
	if strings.Contains(label, "Weekly") {
		return strings.Replace(label, "Weekly", "Window", 1)
	}
	return label
}

func appendCodexWindowQuotaRow(rows []QuotaRow, key string, label string, scope string, metric string, info *CodexRateLimitInfo, window *CodexUsageWindow) []QuotaRow {
	// 每个窗口单独展开为一条 quota row，前端再按窗口秒数决定主/次展示位置。
	if window == nil {
		return rows
	}
	label = codexWindowLabel(label, window.LimitWindowSeconds)
	row := QuotaRow{
		Key:               key,
		Label:             label,
		Scope:             scope,
		Metric:            metric,
		UsedPercent:       floatPtr(window.UsedPercent),
		Allowed:           info.Allowed,
		LimitReached:      info.LimitReached,
		ResetAfterSeconds: intPtr(window.ResetAfterSeconds),
	}
	if window.LimitWindowSeconds != 0 {
		row.Window = &QuotaWindow{Seconds: intPtr(window.LimitWindowSeconds)}
	}
	if window.ResetAt != 0 {
		row.ResetAt = time.Unix(window.ResetAt, 0).UTC().Format(time.RFC3339)
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
		return &QuotaWindow{Duration: floatPtr(float64(limit.Window.Duration)), Unit: limit.Window.TimeUnit}
	}
	if limit.Duration != 0 || limit.TimeUnit != "" {
		return &QuotaWindow{Duration: floatPtr(float64(limit.Duration)), Unit: limit.TimeUnit}
	}
	return nil
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
