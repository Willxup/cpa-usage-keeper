package test

import (
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/quota"
)

func TestBuildCodexMainQuotaObservationsUsesPresenceFlagsAndExcludesReviewAdditional(t *testing.T) {
	// 准备：Primary 显式 0% 已用，Secondary 缺少 used 字段；Review/Additional 即使完整也不得进入历史。
	observedAt := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	completeWindow := &quota.CodexUsageWindow{
		UsedPercent:           0,
		LimitWindowSeconds:    18_000,
		ResetAt:               observedAt.Add(5 * time.Hour).Unix(),
		HasUsedPercent:        true,
		HasLimitWindowSeconds: true,
		HasResetAt:            true,
		HasResetAfterSeconds:  false,
	}
	missingUsed := &quota.CodexUsageWindow{
		LimitWindowSeconds:    604_800,
		ResetAt:               observedAt.Add(7 * 24 * time.Hour).Unix(),
		HasLimitWindowSeconds: true,
		HasResetAt:            true,
	}
	output := quota.ProviderOutput{
		Provider: "codex",
		Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
			RateLimit: &quota.CodexRateLimitInfo{
				PrimaryWindow:   completeWindow,
				SecondaryWindow: missingUsed,
			},
			CodeReviewRateLimit: &quota.CodexRateLimitInfo{PrimaryWindow: completeWindow},
			AdditionalRateLimits: []quota.CodexAdditionalRateLimit{{
				LimitName: "Spark",
				RateLimit: &quota.CodexRateLimitInfo{PrimaryWindow: completeWindow},
			}},
		}},
	}

	// 执行：主动查询提取只能读取 RateLimit.Primary/Secondary，并要求关键字段明确存在。
	observations := quota.BuildCodexMainQuotaObservations("codex-auth", output, observedAt)
	if len(observations) != 1 {
		t.Fatalf("expected only explicit primary main quota observation, got %+v", observations)
	}
	observation := observations[0]
	if observation.WindowRole != "primary" || observation.RemainingPercent != 100 {
		t.Fatalf("unexpected explicit-zero main quota observation: %+v", observation)
	}
}

func TestBuildCodexMainQuotaObservationsClampsFiniteRawPercentAndRejectsNonFinite(t *testing.T) {
	// 准备：有限超界 raw 只用来生成钳制后的剩余值；非有限 raw 不能进入历史。
	observedAt := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	window := &quota.CodexUsageWindow{
		UsedPercent:           125.5,
		LimitWindowSeconds:    18_000,
		ResetAfterSeconds:     60,
		HasUsedPercent:        true,
		HasLimitWindowSeconds: true,
		HasResetAfterSeconds:  true,
	}
	output := quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
		RateLimit: &quota.CodexRateLimitInfo{PrimaryWindow: window},
	}}}
	observations := quota.BuildCodexMainQuotaObservations("codex-auth", output, observedAt)
	if len(observations) != 1 || observations[0].RemainingPercent != 0 {
		t.Fatalf("unexpected clamped finite raw observation: %+v", observations)
	}

	// NaN 使用同一明确存在字段路径，但必须被有限数值校验跳过。
	window.UsedPercent = math.NaN()
	if observations := quota.BuildCodexMainQuotaObservations("codex-auth", output, observedAt); len(observations) != 0 {
		t.Fatalf("expected non-finite raw percent to be rejected, got %+v", observations)
	}
}
