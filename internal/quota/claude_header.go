package quota

import (
	"math"
	"net/http"
	"strings"
	"time"
)

type claudeUsageHeaderSnapshotProcessor struct{}

func (claudeUsageHeaderSnapshotProcessor) TryBuildUsageHeaderSnapshot(input UsageHeaderSnapshotInput) (*UsageHeaderSnapshot, bool) {
	// 只保留四个账号主窗口字段；状态、模型窗口和其它命名空间都不参与额度采样。
	filtered := make(http.Header, 4)
	for key, values := range input.Headers {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(key))
		switch canonical {
		case "Anthropic-Ratelimit-Unified-5h-Utilization", "Anthropic-Ratelimit-Unified-5h-Reset",
			"Anthropic-Ratelimit-Unified-7d-Utilization", "Anthropic-Ratelimit-Unified-7d-Reset":
			if value, ok := firstBoundedHeaderValue(values); ok {
				filtered.Set(canonical, value)
			}
		}
	}
	five := parseClaudeHeaderWindow(filtered, "Anthropic-Ratelimit-Unified-5h-")
	seven := parseClaudeHeaderWindow(filtered, "Anthropic-Ratelimit-Unified-7d-")
	if five == nil && seven == nil {
		return nil, false
	}
	// history 保存所有有限值；cache 沿用 Codex 的负百分比过滤。
	historyOutput := ProviderOutput{Provider: "claude", Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: five, SevenDay: seven}}}
	cacheUsage := &ClaudeUsagePayload{}
	if five != nil && five.Utilization >= 0 {
		cacheUsage.FiveHour = five
	}
	if seven != nil && seven.Utilization >= 0 {
		cacheUsage.SevenDay = seven
	}
	observations := BuildMainQuotaObservations(input.AuthIndex, historyOutput, input.ObservedAt)
	if cacheUsage.FiveHour == nil && cacheUsage.SevenDay == nil && len(observations) == 0 {
		return nil, false
	}
	return &UsageHeaderSnapshot{
		AuthType: input.AuthType, AuthIndex: input.AuthIndex, Provider: "claude", ObservedAt: input.ObservedAt,
		CacheOutput:           ProviderOutput{Provider: "claude", Result: ClaudeResult{Usage: cacheUsage}},
		MainQuotaObservations: observations,
	}, true
}

func parseClaudeHeaderWindow(headers http.Header, prefix string) *ClaudeUsageWindow {
	utilization, ok := parseFiniteFloatHeader(headers, prefix+"Utilization")
	if !ok || math.IsNaN(utilization*100) || math.IsInf(utilization*100, 0) {
		return nil
	}
	reset, ok := parseIntHeader(headers, prefix+"Reset")
	if !ok || reset <= 0 {
		return nil
	}
	return &ClaudeUsageWindow{Utilization: utilization * 100, ResetsAt: time.Unix(reset, 0).Format(time.RFC3339), HasUtilization: true}
}
