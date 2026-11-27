package quota

import (
	"strings"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

// QuotaHistoryExtractor 从单渠道原始 ProviderOutput 提取可持久化限额历史观测。
type QuotaHistoryExtractor func(authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation

var quotaHistoryExtractors = map[string]QuotaHistoryExtractor{
	"codex":  extractCodexQuotaHistoryObservations,
	"claude": extractClaudeQuotaHistoryObservations,
}

func hasQuotaHistoryProvider(provider string) bool {
	provider = normalizeIdentityType(provider)
	if provider == "" {
		return false
	}
	if _, ok := quotaHistoryExtractors[provider]; !ok {
		return false
	}
	_, ok := repositorydto.LookupQuotaHistoryProvider(provider)
	return ok
}

// BuildQuotaHistoryObservations 按权威 provider 身份分发到对应渠道提取器，并过滤任何不匹配的观测。
func BuildQuotaHistoryObservations(provider string, authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation {
	provider = normalizeIdentityType(provider)
	authIndex = strings.TrimSpace(authIndex)
	if provider == "" || authIndex == "" || observedAt.IsZero() {
		return nil
	}
	extractor, ok := quotaHistoryExtractors[provider]
	if !ok {
		return nil
	}
	observations := extractor(authIndex, output, observedAt)
	if len(observations) == 0 {
		return nil
	}
	filtered := observations[:0]
	for _, observation := range observations {
		if normalizeIdentityType(observation.Provider) != provider {
			continue
		}
		filtered = append(filtered, observation)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
