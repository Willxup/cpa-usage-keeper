package quota

import (
	"math"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

// extractAntigravityQuotaHistoryObservations 从 Antigravity 主动查询结果提取双模型池四窗口历史观测。
func extractAntigravityQuotaHistoryObservations(authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation {
	var quota *AntigravityQuotaPayload
	switch result := output.Result.(type) {
	case AntigravityResult:
		quota = result.Quota
	case *AntigravityResult:
		if result != nil {
			quota = result.Quota
		}
	}
	if quota == nil || len(quota.Groups) == 0 {
		return nil
	}

	observations := make([]repositorydto.CodexMainQuotaObservation, 0, 4)
	seenRoles := make(map[string]struct{}, 4)
	for groupIndex, group := range quota.Groups {
		if isDeprecatedAntigravityArgonGroup(group) {
			continue
		}
		groupKey := antigravityQuotaGroupKey(group, groupIndex)
		if groupKey != antigravityGeminiGroupKey && groupKey != antigravityClaudeGPTGroupKey {
			continue
		}
		for _, bucket := range group.Buckets {
			if bucket.RemainingFraction == nil || math.IsNaN(*bucket.RemainingFraction) || math.IsInf(*bucket.RemainingFraction, 0) {
				continue
			}
			_, metric, window := normalizeAntigravityQuotaWindow(bucket)
			if window == nil || window.Seconds == nil || *window.Seconds <= 0 {
				continue
			}
			role := antigravityQuotaHistoryWindowRole(groupKey, metric)
			if role == "" {
				continue
			}
			if _, duplicate := seenRoles[role]; duplicate {
				continue
			}
			spec, ok := repositorydto.QuotaWindowSpecByRole("antigravity", role)
			if !ok {
				continue
			}
			resetAt, err := timeutil.ParseStorageTime(bucket.ResetTime)
			if err != nil || resetAt.IsZero() || resetAt.Unix() <= 0 {
				continue
			}

			remainingFraction := *bucket.RemainingFraction
			clampedRemaining := math.Max(0, math.Min(100, remainingFraction*100))
			remainingPercent := int(math.Round(clampedRemaining))

			seenRoles[role] = struct{}{}
			observations = append(observations, repositorydto.CodexMainQuotaObservation{
				Provider:         "antigravity",
				QuotaKey:         spec.QuotaKey,
				AuthIndex:        authIndex,
				WindowRole:       spec.WindowRole,
				WindowSeconds:    *window.Seconds,
				ResetAtSource:    "absolute",
				ResetAt:          resetAt,
				RemainingPercent: remainingPercent,
				FirstObservedAt:  observedAt,
				LastObservedAt:   observedAt,
				ObservationCount: 1,
				ZeroUsage:        remainingFraction >= 1,
			})
		}
	}
	return observations
}

func antigravityQuotaHistoryWindowRole(groupKey string, metric string) string {
	switch groupKey {
	case antigravityGeminiGroupKey:
		switch metric {
		case "5h":
			return "gemini_5h"
		case "weekly":
			return "gemini_weekly"
		}
	case antigravityClaudeGPTGroupKey:
		switch metric {
		case "5h":
			return "claude_gpt_5h"
		case "weekly":
			return "claude_gpt_weekly"
		}
	}
	return ""
}
