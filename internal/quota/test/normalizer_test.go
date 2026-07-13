package test

import (
	"encoding/json"
	"testing"
	"time"

	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/timeutil"
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
	assertIntField(t, fiveHour.Window.Seconds, 18000, "five_hour window seconds")
	weekly := findQuotaRow(t, rows, "seven_day")
	assertQuotaText(t, weekly, "Weekly", "window", "")
	assertFloatField(t, weekly.UsedPercent, 72, "seven_day usedPercent")
	assertIntField(t, weekly.Window.Seconds, 604800, "seven_day window seconds")
	sonnet := findQuotaRow(t, rows, "seven_day_sonnet")
	assertQuotaText(t, sonnet, "7d Sonnet", "model", "")
	assertIntField(t, sonnet.Window.Seconds, 604800, "seven_day_sonnet window seconds")
	extra := findQuotaRow(t, rows, "extra_usage")
	assertQuotaText(t, extra, "Extra Usage", "extra_usage", "")
	assertFloatField(t, extra.Used, 250, "extra_usage used")
	assertFloatField(t, extra.Limit, 1000, "extra_usage limit")
	assertFloatField(t, extra.UsedPercent, 25, "extra_usage usedPercent")
	assertBoolField(t, extra.Allowed, true, "extra_usage allowed")
}

func TestNormalizeCodexQuotaRows(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

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
	if primary.PlanType != "plus" {
		t.Fatalf("expected primary planType plus, got %#v", primary.PlanType)
	}
	assertFloatField(t, primary.UsedPercent, 25, "primary usedPercent")
	assertIntField(t, primary.Window.Seconds, 18000, "primary window seconds")
	assertIntField(t, primary.ResetAfterSeconds, 1200, "primary resetAfterSeconds")
	if primary.ResetAt != timeutil.FormatStorageTime(time.Unix(resetAt, 0)) {
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
	if additional.PlanType != "plus" {
		t.Fatalf("expected additional planType plus, got %#v", additional.PlanType)
	}
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
	if primary.ResetAfterSeconds != nil {
		t.Fatalf("expected missing Codex reset_after_seconds to stay nil, got %#v", primary.ResetAfterSeconds)
	}
}

func TestNormalizeCodexPrimaryWindowUsesWindowSecondsForMonthlyLabel(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
		RateLimit: &quota.CodexRateLimitInfo{
			PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 10, LimitWindowSeconds: 2628000},
		},
		CodeReviewRateLimit: &quota.CodexRateLimitInfo{
			PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 25, LimitWindowSeconds: 2592000},
		},
		AdditionalRateLimits: []quota.CodexAdditionalRateLimit{{
			LimitName: "codex-spark",
			RateLimit: &quota.CodexRateLimitInfo{
				PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 40, LimitWindowSeconds: 2628000},
			},
		}},
	}}})

	primary := findQuotaRow(t, rows, "rate_limit.primary_window")
	assertQuotaText(t, primary, "Monthly", "window", "")
	assertIntField(t, primary.Window.Seconds, 2628000, "primary average monthly window seconds")
	codeReview := findQuotaRow(t, rows, "code_review_rate_limit.primary_window")
	assertQuotaText(t, codeReview, "Code Review Monthly", "code_review", "")
	additional := findQuotaRow(t, rows, "additional_rate_limits.codex-spark.primary_window")
	assertQuotaText(t, additional, "codex-spark Monthly", "additional", "codex-spark")
}

func TestNormalizeCodexUnknownWindowKeepsRoleLabelWithoutGenericWindow(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
		RateLimit: &quota.CodexRateLimitInfo{
			PrimaryWindow:   &quota.CodexUsageWindow{UsedPercent: 10, LimitWindowSeconds: 3600},
			SecondaryWindow: &quota.CodexUsageWindow{UsedPercent: 20, LimitWindowSeconds: 7200},
		},
		CodeReviewRateLimit: &quota.CodexRateLimitInfo{
			PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 30, LimitWindowSeconds: 3600},
		},
		AdditionalRateLimits: []quota.CodexAdditionalRateLimit{{
			LimitName: "GPT-5.3-Codex-Spark",
			RateLimit: &quota.CodexRateLimitInfo{
				SecondaryWindow: &quota.CodexUsageWindow{UsedPercent: 40, LimitWindowSeconds: 7200},
			},
		}},
	}}})

	primary := findQuotaRow(t, rows, "rate_limit.primary_window")
	assertQuotaText(t, primary, "Primary", "window", "")
	assertIntField(t, primary.Window.Seconds, 3600, "primary unknown window seconds")
	secondary := findQuotaRow(t, rows, "rate_limit.secondary_window")
	assertQuotaText(t, secondary, "Secondary", "window", "")
	assertIntField(t, secondary.Window.Seconds, 7200, "secondary unknown window seconds")
	codeReview := findQuotaRow(t, rows, "code_review_rate_limit.primary_window")
	assertQuotaText(t, codeReview, "Code Review Primary", "code_review", "")
	additional := findQuotaRow(t, rows, "additional_rate_limits.GPT-5.3-Codex-Spark.secondary_window")
	assertQuotaText(t, additional, "GPT-5.3-Codex-Spark Secondary", "additional", "GPT-5.3-Codex-Spark")
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
	if bucket.Window != nil {
		t.Fatalf("expected Gemini CLI bucket to avoid guessed window seconds, got %#v", bucket.Window)
	}
	currentCredits := findQuotaRow(t, rows, "code_assist.current_tier.GOOGLE_ONE_AI")
	assertQuotaText(t, currentCredits, "Code Assist Credit", "credits", "GOOGLE_ONE_AI")
	assertFloatField(t, currentCredits.Remaining, 10, "current credits remaining")
	paidCredits := findQuotaRow(t, rows, "code_assist.paid_tier.GEMINI_CODE_ASSIST")
	assertQuotaText(t, paidCredits, "Code Assist Credit", "credits", "GEMINI_CODE_ASSIST")
	assertFloatField(t, paidCredits.Remaining, 20, "paid credits remaining")
}

func TestNormalizeAntigravityQuotaRows(t *testing.T) {
	remainingFiveHour := 0.4
	remainingWeekly := 0.9
	remainingExhausted := 0.0
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "antigravity", Result: quota.AntigravityResult{Quota: &quota.AntigravityQuotaPayload{Groups: []quota.AntigravityQuotaGroup{
		{
			DisplayName: "Gemini Models",
			Description: "Models within this group: Gemini Flash, Gemini Pro",
			Buckets: []quota.AntigravityQuotaBucket{
				{BucketID: "gemini-weekly", DisplayName: "Weekly Limit", Window: "weekly", RemainingFraction: &remainingWeekly, ResetTime: "2026-05-10T12:00:00Z"},
				{BucketID: "gemini-5h", DisplayName: "Five Hour Limit", Window: "5h", RemainingFraction: &remainingFiveHour, ResetTime: "2026-05-09T12:00:00Z"},
				{BucketID: "gemini-missing", DisplayName: "Missing", Window: "5h"},
			},
		},
		{
			DisplayName: "Claude and GPT models",
			Buckets: []quota.AntigravityQuotaBucket{
				{BucketID: "3p-5h", DisplayName: "Five Hour Limit", Window: "5h", RemainingFraction: &remainingExhausted, ResetTime: "2026-05-11T12:00:00Z"},
			},
		},
	}}}})

	if len(rows) != 3 {
		t.Fatalf("expected 3 quota rows, got %#v", rows)
	}
	if rows[0].Key != "bucket.antigravity-gemini-models.gemini-5h" || rows[1].Key != "bucket.antigravity-gemini-models.gemini-weekly" || rows[2].Key != "bucket.antigravity-claude-and-gpt-models.3p-5h" {
		t.Fatalf("expected 5h before Weekly within each upstream group, got %#v", rows)
	}
	fiveHour := findQuotaRow(t, rows, "bucket.antigravity-gemini-models.gemini-5h")
	assertQuotaText(t, fiveHour, "5h", "quota_group", "5h")
	assertFloatField(t, fiveHour.RemainingFraction, 0.4, "five hour remainingFraction")
	assertIntField(t, fiveHour.Window.Seconds, 18000, "five hour window seconds")
	if fiveHour.GroupKey != "antigravity-gemini-models" || fiveHour.GroupLabel != "Gemini Models" {
		t.Fatalf("unexpected five hour group metadata: %#v", fiveHour)
	}
	encodedRows, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal normalized antigravity rows: %v", err)
	}
	if !contains(string(encodedRows), `"groupDescription":"Models within this group: Gemini Flash, Gemini Pro"`) {
		t.Fatalf("expected upstream group description on normalized rows, got %s", encodedRows)
	}
	weekly := findQuotaRow(t, rows, "bucket.antigravity-gemini-models.gemini-weekly")
	assertQuotaText(t, weekly, "Weekly", "quota_group", "weekly")
	assertFloatField(t, weekly.RemainingFraction, 0.9, "weekly remainingFraction")
	assertIntField(t, weekly.Window.Seconds, 604800, "weekly window seconds")
	exhausted := findQuotaRow(t, rows, "bucket.antigravity-claude-and-gpt-models.3p-5h")
	assertFloatField(t, exhausted.RemainingFraction, 0, "exhausted remainingFraction")
	if exhausted.GroupKey != "antigravity-claude-and-gpt-models" || exhausted.GroupLabel != "Claude and GPT models" {
		t.Fatalf("unexpected exhausted group metadata: %#v", exhausted)
	}
}

func TestNormalizeAntigravityQuotaRowsScopesDuplicateBucketIDsByStableGroup(t *testing.T) {
	remaining := 0.5
	groups := []quota.AntigravityQuotaGroup{
		{DisplayName: "Gemini Models", Buckets: []quota.AntigravityQuotaBucket{{BucketID: "shared-5h", Window: "5h", RemainingFraction: &remaining}}},
		{DisplayName: "Claude and GPT models", Buckets: []quota.AntigravityQuotaBucket{{BucketID: "shared-5h", Window: "5h", RemainingFraction: &remaining}}},
	}
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "antigravity", Result: quota.AntigravityResult{Quota: &quota.AntigravityQuotaPayload{Groups: groups}}})

	if len(rows) != 2 || rows[0].Key == rows[1].Key {
		t.Fatalf("expected unique cross-group row keys, got %#v", rows)
	}
	if rows[0].Key != "bucket.antigravity-gemini-models.shared-5h" || rows[1].Key != "bucket.antigravity-claude-and-gpt-models.shared-5h" {
		t.Fatalf("expected stable group-scoped bucket keys, got %#v", rows)
	}

	reversed := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "antigravity", Result: quota.AntigravityResult{Quota: &quota.AntigravityQuotaPayload{Groups: []quota.AntigravityQuotaGroup{groups[1], groups[0]}}}})
	if reversed[0].Key != rows[1].Key || reversed[1].Key != rows[0].Key {
		t.Fatalf("expected keys to remain stable after group reorder, got %#v", reversed)
	}
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
	assertIntField(t, limit.Window.Seconds, 86400, "limit window seconds")
	if limit.ResetAt != "2026-05-10T12:00:00Z" {
		t.Fatalf("unexpected limit resetAt: %#v", limit)
	}
	assertIntField(t, limit.ResetAfterSeconds, 7200, "limit resetAfterSeconds")
}

func TestNormalizeXAIQuotaRows(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "xai", Result: quota.XAIResult{
		Weekly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
			CurrentPeriod:      &quota.XAIBillingPeriod{Type: "weekly", Start: "2026-07-06T00:00:00Z", End: "2026-07-13T00:00:00Z"},
			CreditUsagePercent: floatPtr(62.5),
		}},
		Monthly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
			MonthlyLimit:       quota.XAIMoneyValue{Val: floatPtr(1000)},
			Used:               quota.XAIMoneyValue{Val: floatPtr(1250)},
			OnDemandCap:        quota.XAIMoneyValue{Val: floatPtr(500)},
			OnDemandUsed:       quota.XAIMoneyValue{Val: floatPtr(200)},
			BillingPeriodStart: "2026-07-01T00:00:00Z",
			BillingPeriodEnd:   "2026-08-01T00:00:00Z",
		}},
	}})

	if len(rows) != 3 {
		t.Fatalf("expected weekly, monthly, and pay-as-you-go rows, got %#v", rows)
	}
	if rows[0].Key != "billing.weekly" || rows[1].Key != "billing.monthly" || rows[2].Key != "billing.on_demand" {
		t.Fatalf("unexpected xai quota order: %#v", rows)
	}
	weekly := rows[0]
	assertQuotaText(t, weekly, "Weekly", "billing", "weekly")
	assertFloatField(t, weekly.UsedPercent, 62.5, "xai weekly usedPercent")
	assertIntField(t, weekly.Window.Seconds, 604800, "xai weekly window seconds")
	if weekly.ResetAt != "2026-07-13T00:00:00Z" || weekly.LimitReached == nil || *weekly.LimitReached {
		t.Fatalf("unexpected xai weekly row: %#v", weekly)
	}
	monthly := findQuotaRow(t, rows, "billing.monthly")
	assertQuotaText(t, monthly, "Monthly Spend", "billing", "usd_cents")
	assertFloatField(t, monthly.Used, 1000, "xai monthly used")
	assertFloatField(t, monthly.Limit, 1000, "xai monthly limit")
	assertFloatField(t, monthly.Remaining, 0, "xai monthly remaining")
	assertFloatField(t, monthly.UsedPercent, 100, "xai monthly usedPercent")
	assertIntField(t, monthly.Window.Seconds, 2592000, "xai monthly window seconds")
	if monthly.ResetAt != "2026-08-01T00:00:00Z" || monthly.LimitReached == nil || *monthly.LimitReached || monthly.Allowed == nil || !*monthly.Allowed {
		t.Fatalf("unexpected xai monthly resetAt: %#v", monthly)
	}
	onDemand := findQuotaRow(t, rows, "billing.on_demand")
	assertQuotaText(t, onDemand, "Pay-as-you-go", "billing", "usd_cents")
	assertFloatField(t, onDemand.Used, 200, "xai pay-as-you-go used")
	assertFloatField(t, onDemand.Limit, 500, "xai pay-as-you-go limit")
	assertFloatField(t, onDemand.Remaining, 300, "xai pay-as-you-go remaining")
	assertFloatField(t, onDemand.UsedPercent, 40, "xai pay-as-you-go usedPercent")
	if onDemand.LimitReached == nil || *onDemand.LimitReached {
		t.Fatalf("unexpected xai pay-as-you-go state: %#v", onDemand)
	}
}

func TestNormalizeXAIProductRowsUsesStableKeysDeduplicationAndSorting(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "xai", Result: quota.XAIResult{
		Weekly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
			CurrentPeriod:      &quota.XAIBillingPeriod{Type: "weekly", End: "2026-07-13T00:00:00Z"},
			CreditUsagePercent: floatPtr(20),
			ProductUsage: []quota.XAIBillingProductUsage{
				{Product: "  Grok Code  ", UsagePercent: floatPtr(80)},
				{Product: "grok 4", UsagePercent: floatPtr(100)},
				{Product: "GROK CODE", UsagePercent: floatPtr(90)},
				{Product: "", UsagePercent: floatPtr(50)},
				{Product: "No Data"},
			},
		}},
	}})

	if len(rows) != 3 {
		t.Fatalf("expected weekly plus two product rows, got %#v", rows)
	}
	if rows[0].Key != "billing.weekly" || rows[1].Key != "billing.weekly.product.grok+4" || rows[2].Key != "billing.weekly.product.grok+code" {
		t.Fatalf("unexpected product row order or keys: %#v", rows)
	}
	assertQuotaText(t, rows[1], "grok 4 Usage", "product", "grok 4")
	assertFloatField(t, rows[1].UsedPercent, 100, "grok 4 usedPercent")
	if rows[1].LimitReached == nil || !*rows[1].LimitReached {
		t.Fatalf("expected grok 4 product quota to be reached: %#v", rows[1])
	}
	assertQuotaText(t, rows[2], "Grok Code Usage", "product", "Grok Code")
	assertFloatField(t, rows[2].UsedPercent, 90, "deduplicated grok code usedPercent")
	assertIntField(t, rows[2].Window.Seconds, 604800, "product weekly window seconds")
	if rows[2].ResetAt != "2026-07-13T00:00:00Z" {
		t.Fatalf("unexpected product resetAt: %#v", rows[2])
	}
}

func TestNormalizeXAIDerivesPayAsYouGoUsageFromTotalSpend(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "xai", Result: quota.XAIResult{
		Monthly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
			MonthlyLimit: quota.XAIMoneyValue{Val: floatPtr(1000)},
			Used:         quota.XAIMoneyValue{Val: floatPtr(1300)},
			OnDemandCap:  quota.XAIMoneyValue{Val: floatPtr(500)},
		}},
	}})

	monthly := findQuotaRow(t, rows, "billing.monthly")
	if monthly.LimitReached == nil || *monthly.LimitReached {
		t.Fatalf("monthly included spend must remain allowed while PAYG is available: %#v", monthly)
	}
	onDemand := findQuotaRow(t, rows, "billing.on_demand")
	assertFloatField(t, onDemand.Used, 300, "derived pay-as-you-go used")
	assertFloatField(t, onDemand.Remaining, 200, "derived pay-as-you-go remaining")
	assertFloatField(t, onDemand.UsedPercent, 60, "derived pay-as-you-go usedPercent")
}

func TestNormalizeXAIQuotaRowsMarksPayAsYouGoExhaustion(t *testing.T) {
	rows := quota.NormalizeQuotaRows(quota.ProviderOutput{Provider: "xai", Result: quota.XAIResult{
		Monthly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
			MonthlyLimit: quota.XAIMoneyValue{Val: floatPtr(1000)},
			Used:         quota.XAIMoneyValue{Val: floatPtr(1500)},
			OnDemandCap:  quota.XAIMoneyValue{Val: floatPtr(500)},
		}},
	}})

	monthly := findQuotaRow(t, rows, "billing.monthly")
	onDemand := findQuotaRow(t, rows, "billing.on_demand")
	if monthly.LimitReached == nil || !*monthly.LimitReached || onDemand.LimitReached == nil || !*onDemand.LimitReached {
		t.Fatalf("expected exhausted included and PAYG rows to be limit reached: %#v", rows)
	}
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
