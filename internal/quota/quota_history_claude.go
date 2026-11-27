package quota

import (
	"math"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

// extractClaudeQuotaHistoryObservations 从 Claude 主动查询或 Header 结果提取 5h/7d 主窗口观测。
func extractClaudeQuotaHistoryObservations(authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation {
	var usage *ClaudeUsagePayload
	switch result := output.Result.(type) {
	case ClaudeResult:
		usage = result.Usage
	case *ClaudeResult:
		if result != nil {
			usage = result.Usage
		}
	}
	if usage == nil {
		return nil
	}

	observations := make([]repositorydto.CodexMainQuotaObservation, 0, 2)
	for _, window := range []struct {
		role    string
		key     string
		seconds int64
		value   *ClaudeUsageWindow
	}{
		{"primary", "five_hour", quotaWindowFiveHourSeconds, usage.FiveHour},
		{"secondary", "seven_day", quotaWindowSevenDaySeconds, usage.SevenDay},
	} {
		if window.value == nil || !window.value.HasUtilization || math.IsNaN(window.value.Utilization) || math.IsInf(window.value.Utilization, 0) {
			continue
		}
		resetAt, err := time.Parse(time.RFC3339Nano, window.value.ResetsAt)
		if err != nil || resetAt.IsZero() || resetAt.Unix() <= 0 {
			continue
		}
		clampedUsed := math.Max(0, math.Min(100, window.value.Utilization))
		remainingPercent := int(math.Round(100 - clampedUsed))
		observations = append(observations, repositorydto.CodexMainQuotaObservation{
			Provider:         "claude",
			QuotaKey:         window.key,
			AuthIndex:        authIndex,
			WindowRole:       window.role,
			WindowSeconds:    window.seconds,
			ResetAtSource:    "absolute",
			ResetAt:          time.Unix(resetAt.Unix(), 0),
			RemainingPercent: remainingPercent,
			FirstObservedAt:  observedAt,
			LastObservedAt:   observedAt,
			ObservationCount: 1,
			ZeroUsage:        window.value.Utilization <= 0,
		})
	}
	return observations
}
