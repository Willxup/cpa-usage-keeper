package tokenprocessor

import "encoding/json"

// ResolveExecutorWithBreakdown 保持已登记 executor 优先，只在未匹配时使用 CPA 完整 accounting。
// 不可用的 accounting 原样返回待解析结果，调用方继续已有 identity → strict 回退链。
func ResolveExecutorWithBreakdown(executorType string, values TokenValues, accountingVersion, raw json.RawMessage) (HandlerResolution, error) {
	resolution, err := ResolveExecutor(executorType)
	if err != nil || !resolution.NeedsIdentity() {
		return resolution, err
	}
	tokens, ok := decodeCompleteBreakdown(accountingVersion, raw)
	if !ok || !breakdownMatchesReportedValues(tokens, values) {
		return resolution, nil
	}
	resolved := resolution.(handlerResolution)
	resolved.handlerID = HandlerTokenBreakdown
	resolved.evidenceSource = EvidenceTokenBreakdown
	resolved.evidenceStrength = EvidenceAccountingContract
	resolved.needsIdentity = false
	resolved.breakdownTokens = &tokens
	return resolved, nil
}

// 数字使用指针保留缺失/null 与显式零的区别，不能把缺字段的对象误认成 complete。
type accountingBreakdown struct {
	SchemaVersion int    `json:"schema_version"`
	Quality       string `json:"quality"`
	Total         *int64 `json:"total_tokens"`
	Unclassified  *int64 `json:"unclassified_tokens"`
	Input         struct {
		Total      *int64 `json:"total_tokens"`
		Uncached   *int64 `json:"uncached_tokens"`
		CacheRead  *int64 `json:"cache_read_tokens"`
		CacheWrite *int64 `json:"cache_write_tokens"`
	} `json:"input"`
	Output struct {
		Total        *int64 `json:"total_tokens"`
		NonReasoning *int64 `json:"non_reasoning_tokens"`
		Reasoning    *int64 `json:"reasoning_tokens"`
	} `json:"output"`
}

func decodeCompleteBreakdown(accountingVersion, raw json.RawMessage) (TokenValues, bool) {
	var version int
	var breakdown accountingBreakdown
	if json.Unmarshal(accountingVersion, &version) != nil || version != 2 ||
		json.Unmarshal(raw, &breakdown) != nil || breakdown.SchemaVersion != 2 || breakdown.Quality != "complete" {
		return TokenValues{}, false
	}
	for _, value := range []*int64{
		breakdown.Total, breakdown.Unclassified,
		breakdown.Input.Total, breakdown.Input.Uncached, breakdown.Input.CacheRead, breakdown.Input.CacheWrite,
		breakdown.Output.Total, breakdown.Output.NonReasoning, breakdown.Output.Reasoning,
	} {
		if value == nil || *value < 0 {
			return TokenValues{}, false
		}
	}
	if *breakdown.Unclassified != 0 {
		return TokenValues{}, false
	}
	input, inputOK := safeAddTokenCounts(*breakdown.Input.Uncached, *breakdown.Input.CacheRead, *breakdown.Input.CacheWrite)
	output, outputOK := safeAddTokenCounts(*breakdown.Output.NonReasoning, *breakdown.Output.Reasoning)
	total, totalOK := safeAddTokenCounts(*breakdown.Input.Total, *breakdown.Output.Total)
	if !inputOK || !outputOK || !totalOK || input != *breakdown.Input.Total || output != *breakdown.Output.Total || total != *breakdown.Total {
		return TokenValues{}, false
	}
	return TokenValues{
		InputTokens: input, OutputTokens: output, TotalTokens: total,
		CacheReadTokens: *breakdown.Input.CacheRead, CacheReadPresent: true,
		CachedTokens: *breakdown.Input.CacheRead, CacheCreationTokens: *breakdown.Input.CacheWrite,
		ReasoningTokens: *breakdown.Output.Reasoning,
	}, true
}

func breakdownMatchesReportedValues(canonical, reported TokenValues) bool {
	for _, value := range []int64{reported.InputTokens, reported.OutputTokens, reported.TotalTokens, reported.CachedTokens, reported.CacheReadTokens, reported.CacheCreationTokens, reported.ReasoningTokens} {
		if value < 0 {
			return false
		}
	}
	// 父级可因缓存/reasoning 合并而增加，但不能抹掉已报明细或推翻明确的非零总量。
	if canonical.InputTokens < reported.InputTokens || canonical.OutputTokens < reported.OutputTokens ||
		(reported.TotalTokens > 0 && canonical.TotalTokens != reported.TotalTokens) {
		return false
	}
	// cached 是旧协议兼容别名，可能代表 cache creation，不能拿它反向推翻显式 breakdown。
	if (reported.CacheReadPresent || reported.CacheReadTokens > 0) && canonical.CacheReadTokens != reported.CacheReadTokens {
		return false
	}
	return (reported.CacheCreationTokens == 0 || canonical.CacheCreationTokens == reported.CacheCreationTokens) &&
		(reported.ReasoningTokens == 0 || canonical.ReasoningTokens == reported.ReasoningTokens)
}

type tokenBreakdownHandler struct{}

func (tokenBreakdownHandler) ID() HandlerID { return HandlerTokenBreakdown }

func (tokenBreakdownHandler) Normalize(context normalizationContext) handlerResult {
	if context.resolution.breakdownTokens == nil {
		return strictPassThroughHandler{}.Normalize(context)
	}
	tokens := *context.resolution.breakdownTokens
	before := context.nonNegative
	actions := make([]Action, 0, 7)
	for _, change := range []struct {
		field         string
		before, after int64
	}{
		{"input_tokens", before.InputTokens, tokens.InputTokens},
		{"output_tokens", before.OutputTokens, tokens.OutputTokens},
		{"reasoning_tokens", before.ReasoningTokens, tokens.ReasoningTokens},
		{"cached_tokens", before.CachedTokens, tokens.CachedTokens},
		{"cache_read_tokens", before.CacheReadTokens, tokens.CacheReadTokens},
		{"cache_creation_tokens", before.CacheCreationTokens, tokens.CacheCreationTokens},
		{"total_tokens", before.TotalTokens, tokens.TotalTokens},
	} {
		if change.before != change.after {
			actions = append(actions, Action{Code: ActionNormalizeTokenBreakdown, Field: change.field, Before: change.before, After: change.after, EvidenceSource: EvidenceTokenBreakdown, Rule: "complete_token_breakdown_v2", outcome: TokenOutcomeNormalized})
		}
	}
	// 完整快照已校验且一次映射，禁止再进入 Claude/Gemini fold 或 legacy Total 补值。
	return handlerResult{tokens: tokens, authority: totalAuthorityPreserve, actions: actions}
}
