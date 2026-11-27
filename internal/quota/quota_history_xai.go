package quota

import (
	"math"
	"strings"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

// extractXAIQuotaHistoryObservations 从 xAI Weekly billing 响应提取周度限额历史观测；缺省总百分比时不伪造记录。
func extractXAIQuotaHistoryObservations(authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation {
	var weekly *XAIBillingPayload
	switch result := output.Result.(type) {
	case XAIResult:
		weekly = result.Weekly
	case *XAIResult:
		if result != nil {
			weekly = result.Weekly
		}
	}
	config := xaiBillingConfig(weekly)
	if config == nil || config.CreditUsagePercent == nil {
		return nil
	}
	usedPercent := *config.CreditUsagePercent
	if math.IsNaN(usedPercent) || math.IsInf(usedPercent, 0) {
		return nil
	}
	resetText := strings.TrimSpace(xaiWeeklyResetAt(config))
	if resetText == "" {
		return nil
	}
	resetAt, err := timeutil.ParseStorageTime(resetText)
	if err != nil || resetAt.IsZero() || resetAt.Unix() <= 0 {
		return nil
	}
	spec, ok := repositorydto.QuotaWindowSpecByRole("xai", "weekly")
	if !ok {
		return nil
	}

	clampedUsed := math.Max(0, math.Min(100, usedPercent))
	remainingPercent := int(math.Round(100 - clampedUsed))
	return []repositorydto.CodexMainQuotaObservation{{
		Provider:         "xai",
		QuotaKey:         spec.QuotaKey,
		AuthIndex:        authIndex,
		WindowRole:       spec.WindowRole,
		WindowSeconds:    quotaWindowSevenDaySeconds,
		ResetAtSource:    "absolute",
		ResetAt:          resetAt,
		RemainingPercent: remainingPercent,
		FirstObservedAt:  observedAt,
		LastObservedAt:   observedAt,
		ObservationCount: 1,
		ZeroUsage:        usedPercent <= 0,
	}}
}
