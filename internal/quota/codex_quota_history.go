package quota

import (
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

// BuildCodexMainQuotaObservations 兼容既有调用方，从主动查询原始 ProviderOutput 只提取 Codex 主额度历史。
func BuildCodexMainQuotaObservations(authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation {
	return BuildQuotaHistoryObservations("codex", authIndex, output, observedAt)
}

// BuildMainQuotaObservations 兼容既有调用方，按 ProviderOutput.Result 具体类型分发到对应渠道限额历史提取器。
func BuildMainQuotaObservations(authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation {
	provider := quotaHistoryProviderFromResult(output.Result)
	if provider == "" {
		return nil
	}
	return BuildQuotaHistoryObservations(provider, authIndex, output, observedAt)
}

func quotaHistoryProviderFromResult(result any) string {
	switch result.(type) {
	case CodexResult, *CodexResult:
		return "codex"
	case ClaudeResult, *ClaudeResult:
		return "claude"
	case AntigravityResult, *AntigravityResult:
		return "antigravity"
	default:
		return ""
	}
}
