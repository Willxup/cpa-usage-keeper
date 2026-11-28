package dto

import "strings"

const (
	AntigravityGeminiGroupKey    = "antigravity-gemini-models"
	AntigravityClaudeGPTGroupKey = "antigravity-claude-and-gpt-models"
)

// AntigravityUsageGroupForModel 按模型前缀返回 Antigravity 共享模型池键，供实时窗口统计与限额历史分池汇总共用。
func AntigravityUsageGroupForModel(model string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(normalized, "gemini-"):
		return AntigravityGeminiGroupKey, true
	case strings.HasPrefix(normalized, "claude-"), strings.HasPrefix(normalized, "gpt-"):
		return AntigravityClaudeGPTGroupKey, true
	default:
		return "", false
	}
}

var antigravityQuotaHistoryProviderSpec = QuotaHistoryProviderSpec{
	Provider:    "antigravity",
	ModelScoped: true,
	Windows: []QuotaWindowSpec{
		{
			WindowRole: "gemini_5h",
			QuotaKey:   "bucket.antigravity-gemini-models.5h",
			GroupKey:   AntigravityGeminiGroupKey,
			GroupLabel: "Gemini",
			Order:      0,
		},
		{
			WindowRole: "gemini_weekly",
			QuotaKey:   "bucket.antigravity-gemini-models.weekly",
			GroupKey:   AntigravityGeminiGroupKey,
			GroupLabel: "Gemini",
			Order:      1,
		},
		{
			WindowRole: "claude_gpt_5h",
			QuotaKey:   "bucket.antigravity-claude-and-gpt-models.5h",
			GroupKey:   AntigravityClaudeGPTGroupKey,
			GroupLabel: "Claude & GPT",
			Order:      2,
		},
		{
			WindowRole: "claude_gpt_weekly",
			QuotaKey:   "bucket.antigravity-claude-and-gpt-models.weekly",
			GroupKey:   AntigravityClaudeGPTGroupKey,
			GroupLabel: "Claude & GPT",
			Order:      3,
		},
	},
	MatchModel: func(quotaKey string, model string) bool {
		spec, ok := QuotaWindowSpecByKey("antigravity", quotaKey)
		if !ok || spec.GroupKey == "" {
			return false
		}
		groupKey, matched := AntigravityUsageGroupForModel(model)
		return matched && groupKey == spec.GroupKey
	},
}
