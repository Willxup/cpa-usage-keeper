package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/quota"
)

func TestNormalizeClaudeQuotaRows(t *testing.T) {
	utilization := 25.0
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "claude", Result: quota.ClaudeResult{
		Usage: &quota.ClaudeUsagePayload{
			FiveHour:       &quota.ClaudeUsageWindow{Utilization: 36, ResetsAt: "2026-05-09T12:00:00Z"},
			SevenDay:       &quota.ClaudeUsageWindow{Utilization: 72, ResetsAt: "2026-05-10T12:00:00Z"},
			SevenDaySonnet: &quota.ClaudeUsageWindow{Utilization: 18, ResetsAt: "2026-05-11T12:00:00Z"},
			ExtraUsage:     &quota.ClaudeExtraUsage{IsEnabled: true, MonthlyLimit: 1000, UsedCredits: 250, Utilization: &utilization},
		},
		Profile: &quota.ClaudeProfileResponse{Account: &quota.ClaudeProfileAccount{Email: "user@example.com"}},
	}})

	if len(rows) != 4 {
		t.Fatalf("expected 4 quota rows, got %#v", rows)
	}
	fiveHour := findQuotaRow(t, rows, "five_hour")
	assertQuotaText(t, fiveHour, "5h", "window", "")
	assertFloatField(t, fiveHour.UsedPercent, 36, "five_hour usedPercent")
	if fiveHour.ResetAt != "2026-05-09T12:00:00Z" {
		t.Fatalf("unexpected five_hour resetAt: %#v", fiveHour)
	}
	weekly := findQuotaRow(t, rows, "seven_day")
	assertQuotaText(t, weekly, "Weekly", "window", "")
	assertFloatField(t, weekly.UsedPercent, 72, "seven_day usedPercent")
	sonnet := findQuotaRow(t, rows, "seven_day_sonnet")
	assertQuotaText(t, sonnet, "7d Sonnet", "model", "")
	extra := findQuotaRow(t, rows, "extra_usage")
	assertQuotaText(t, extra, "Extra Usage", "extra_usage", "")
	assertFloatField(t, extra.Used, 250, "extra_usage used")
	assertFloatField(t, extra.Limit, 1000, "extra_usage limit")
	assertFloatField(t, extra.UsedPercent, 25, "extra_usage usedPercent")
	assertBoolField(t, extra.Allowed, true, "extra_usage allowed")
}

func TestNormalizeCodexQuotaRows(t *testing.T) {
	allowed := true
	limitReached := false
	resetAt := int64(1760000000)
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
		PlanType: "plus",
		RateLimit: &quota.CodexRateLimitInfo{
			Allowed:      &allowed,
			LimitReached: &limitReached,
			PrimaryWindow: &quota.CodexUsageWindow{
				UsedPercent:        25,
				LimitWindowSeconds: 18000,
				ResetAfterSeconds:  1200,
				ResetAt:            resetAt,
			},
			SecondaryWindow: &quota.CodexUsageWindow{UsedPercent: 65, LimitWindowSeconds: 604800, ResetAfterSeconds: 7200},
		},
		CodeReviewRateLimit: &quota.CodexRateLimitInfo{
			Allowed:       &allowed,
			PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 40, LimitWindowSeconds: 18000, ResetAfterSeconds: 600},
		},
		AdditionalRateLimits: []quota.CodexAdditionalRateLimit{{
			LimitName:      "codex-spark",
			MeteredFeature: "spark",
			RateLimit: &quota.CodexRateLimitInfo{
				Allowed:       &allowed,
				PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 12, LimitWindowSeconds: 18000, ResetAfterSeconds: 900},
			},
		}},
	}}})

	if len(rows) != 4 {
		t.Fatalf("expected 4 quota rows, got %#v", rows)
	}
	primary := findQuotaRow(t, rows, "rate_limit.primary_window")
	assertQuotaText(t, primary, "5h", "window", "")
	assertFloatField(t, primary.UsedPercent, 25, "primary usedPercent")
	assertIntField(t, primary.Window.Seconds, 18000, "primary window seconds")
	assertIntField(t, primary.ResetAfterSeconds, 1200, "primary resetAfterSeconds")
	if primary.ResetAt != time.Unix(resetAt, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("unexpected primary resetAt: %#v", primary)
	}
	assertBoolField(t, primary.Allowed, true, "primary allowed")
	assertBoolField(t, primary.LimitReached, false, "primary limitReached")

	secondary := findQuotaRow(t, rows, "rate_limit.secondary_window")
	assertQuotaText(t, secondary, "Weekly", "window", "")
	assertFloatField(t, secondary.UsedPercent, 65, "secondary usedPercent")
	codeReview := findQuotaRow(t, rows, "code_review_rate_limit.primary_window")
	assertQuotaText(t, codeReview, "Code Review 5h", "code_review", "")
	additional := findQuotaRow(t, rows, "additional_rate_limits.codex-spark.primary_window")
	assertQuotaText(t, additional, "codex-spark 5h", "additional", "spark")
	assertFloatField(t, additional.UsedPercent, 12, "additional usedPercent")
}

func TestNormalizeCodexPrimaryWindowUsesWindowSecondsForWeeklyLabel(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
		RateLimit: &quota.CodexRateLimitInfo{
			PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 10, LimitWindowSeconds: 604800},
		},
	}}})

	primary := findQuotaRow(t, rows, "rate_limit.primary_window")
	assertQuotaText(t, primary, "Weekly", "window", "")
	assertIntField(t, primary.Window.Seconds, 604800, "primary weekly window seconds")
}

func TestNormalizeCodexUnknownWindowDoesNotGuessFiveHourOrWeekly(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
		RateLimit: &quota.CodexRateLimitInfo{
			PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 10, LimitWindowSeconds: 3600},
		},
	}}})

	primary := findQuotaRow(t, rows, "rate_limit.primary_window")
	assertQuotaText(t, primary, "Window", "window", "")
	assertIntField(t, primary.Window.Seconds, 3600, "primary unknown window seconds")
}

func TestNormalizeGeminiCLIQuotaRows(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "gemini-cli", Result: quota.GeminiCLIResult{
		Quota: &quota.GeminiCliQuotaPayload{Buckets: []quota.GeminiCliQuotaBucket{{
			ModelID:           "gemini-2.5-pro_vertex",
			TokenType:         "PROMPT",
			RemainingFraction: 0.7,
			RemainingAmount:   42,
			ResetTime:         "2026-05-09T12:00:00Z",
		}}},
		CodeAssist: &quota.GeminiCLICodeAssistPayload{
			CurrentTier: &quota.GeminiCliUserTier{ID: "free-tier", Name: "Free", Description: "metadata", AvailableCredits: []quota.GeminiCliCredits{{CreditType: "GOOGLE_ONE_AI", CreditAmount: 10}}},
			PaidTier:    &quota.GeminiCliUserTier{ID: "paid-tier", AvailableCredits: []quota.GeminiCliCredits{{CreditType: "GEMINI_CODE_ASSIST", CreditAmount: 20}}},
		},
	}})

	if len(rows) != 3 {
		t.Fatalf("expected 3 quota rows, got %#v", rows)
	}
	bucket := findQuotaRow(t, rows, "bucket.gemini-2.5-pro_vertex.PROMPT")
	assertQuotaText(t, bucket, "gemini-2.5-pro_vertex", "model", "PROMPT")
	assertFloatField(t, bucket.Remaining, 42, "bucket remaining")
	assertFloatField(t, bucket.RemainingFraction, 0.7, "bucket remainingFraction")
	if bucket.ResetAt != "2026-05-09T12:00:00Z" {
		t.Fatalf("unexpected bucket resetAt: %#v", bucket)
	}
	currentCredits := findQuotaRow(t, rows, "code_assist.current_tier.GOOGLE_ONE_AI")
	assertQuotaText(t, currentCredits, "Code Assist Credit", "credits", "GOOGLE_ONE_AI")
	assertFloatField(t, currentCredits.Remaining, 10, "current credits remaining")
	paidCredits := findQuotaRow(t, rows, "code_assist.paid_tier.GEMINI_CODE_ASSIST")
	assertQuotaText(t, paidCredits, "Code Assist Credit", "credits", "GEMINI_CODE_ASSIST")
	assertFloatField(t, paidCredits.Remaining, 20, "paid credits remaining")
}

func TestNormalizeAntigravityQuotaRows(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "antigravity", Result: quota.AntigravityResult{Quota: &quota.AntigravityQuotaPayload{Models: map[string]quota.AntigravityQuotaModel{
		"pro":   {DisplayName: "Pro", QuotaInfo: &quota.AntigravityQuotaInfo{RemainingFraction: 0.4, Remaining: 12, ResetTime: "2026-05-09T12:00:00Z"}},
		"flash": {QuotaInfo: &quota.AntigravityQuotaInfo{RemainingFraction: 0.9, Remaining: 32, ResetTime: "2026-05-10T12:00:00Z"}},
	}}}})

	if len(rows) != 2 {
		t.Fatalf("expected 2 quota rows, got %#v", rows)
	}
	pro := findQuotaRow(t, rows, "model.pro")
	assertQuotaText(t, pro, "Pro", "model", "pro")
	assertFloatField(t, pro.Remaining, 12, "pro remaining")
	assertFloatField(t, pro.RemainingFraction, 0.4, "pro remainingFraction")
	flash := findQuotaRow(t, rows, "model.flash")
	assertQuotaText(t, flash, "flash", "model", "flash")
	assertFloatField(t, flash.Remaining, 32, "flash remaining")
}

func TestNormalizeKimiQuotaRows(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "kimi", Result: quota.KimiResult{Usage: &quota.KimiUsagePayload{
		Usage: &quota.KimiUsageDetail{Used: 3, Limit: 10, Remaining: 7, Name: "monthly", Title: "Monthly", ResetAt: "2026-05-09T12:00:00Z", ResetIn: 3600},
		Limits: []quota.KimiLimitItem{{
			Name:      "daily",
			Title:     "Daily",
			Scope:     "request",
			Used:      4,
			Limit:     20,
			Remaining: 16,
			Window:    &quota.KimiLimitWindow{Duration: 1, TimeUnit: "day"},
			Detail:    &quota.KimiUsageDetail{ResetAt: "2026-05-10T12:00:00Z", ResetIn: 7200},
		}},
	}}})

	if len(rows) != 2 {
		t.Fatalf("expected 2 quota rows, got %#v", rows)
	}
	usage := findQuotaRow(t, rows, "usage")
	assertQuotaText(t, usage, "Monthly", "summary", "monthly")
	assertFloatField(t, usage.Used, 3, "usage used")
	assertFloatField(t, usage.Limit, 10, "usage limit")
	assertFloatField(t, usage.Remaining, 7, "usage remaining")
	assertFloatField(t, usage.UsedPercent, 30, "usage usedPercent")
	assertIntField(t, usage.ResetAfterSeconds, 3600, "usage resetAfterSeconds")

	limit := findQuotaRow(t, rows, "limits.daily")
	assertQuotaText(t, limit, "Daily", "request", "daily")
	assertFloatField(t, limit.Used, 4, "limit used")
	assertFloatField(t, limit.Limit, 20, "limit limit")
	assertFloatField(t, limit.Remaining, 16, "limit remaining")
	assertFloatField(t, limit.UsedPercent, 20, "limit usedPercent")
	assertFloatField(t, limit.Window.Duration, 1, "limit window duration")
	if limit.Window.Unit != "day" {
		t.Fatalf("unexpected limit window unit: %#v", limit.Window)
	}
	if limit.ResetAt != "2026-05-10T12:00:00Z" {
		t.Fatalf("unexpected limit resetAt: %#v", limit)
	}
	assertIntField(t, limit.ResetAfterSeconds, 7200, "limit resetAfterSeconds")
}

func findQuotaRow(t *testing.T, rows []quota.QuotaRow, key string) quota.QuotaRow {
	t.Helper()
	for _, row := range rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("missing quota row %q in %#v", key, rows)
	return quota.QuotaRow{}
}

func assertQuotaText(t *testing.T, row quota.QuotaRow, label string, scope string, metric string) {
	t.Helper()
	if row.Label != label || row.Scope != scope || row.Metric != metric {
		t.Fatalf("unexpected quota row text: got %#v want label=%q scope=%q metric=%q", row, label, scope, metric)
	}
}

func assertFloatField(t *testing.T, value *float64, expected float64, label string) {
	t.Helper()
	if value == nil || *value != expected {
		t.Fatalf("unexpected %s: got %#v want %v", label, value, expected)
	}
}

func assertIntField(t *testing.T, value *int64, expected int64, label string) {
	t.Helper()
	if value == nil || *value != expected {
		t.Fatalf("unexpected %s: got %#v want %v", label, value, expected)
	}
}

func assertBoolField(t *testing.T, value *bool, expected bool, label string) {
	t.Helper()
	if value == nil || *value != expected {
		t.Fatalf("unexpected %s: got %#v want %v", label, value, expected)
	}
}
