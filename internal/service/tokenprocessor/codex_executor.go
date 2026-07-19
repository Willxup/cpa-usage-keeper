package tokenprocessor

// CodexExecutor 维护说明：CPA ParseCodexUsage/ParseOpenAIUsage 的 Input 已含 cached 子项、Output 已含 reasoning；
// CPA 更新时需复核 normalizeUsageDetailTotal、usageQueuePlugin.HandleUsage、EnsurePublished 与 PublishAdditionalModel。
// 还需复核 NewExecutorUsageReporter、ExecutorTypeName 和 Codex reporter 创建位置，确认记录确实来自本 parser 合同。
var codexExecutorDefinition = executorDefinition{
	// alias 与 CPA Codex Go 类型完全对应，只授予 Responses 父级字段与 Total 合同。
	alias: "CodexExecutor",
	// Responses handler 保持 Input/Output inclusive；缓存别名继续复用生产旧规则。
	handlerID: HandlerResponsesInclusive,
}

type responsesInclusiveHandler struct{}

func (responsesInclusiveHandler) ID() HandlerID { return HandlerResponsesInclusive }

func (responsesInclusiveHandler) Normalize(context normalizationContext) handlerResult {
	// 所有 Responses 事件只执行现有非 Claude cache alias，不改变 inclusive Input/Output。
	tokens, actions, violations := normalizeNonClaudeCacheRead(context)
	// Codex 旧逻辑会接受仅 cached 的已知兼容形态；保持旧值且不把它升级成新的父子冲突告警。
	if matchesLegacyCodexCachedOnlyShape(context) {
		// preserve 只阻止新 Total/父子推导，cached→read action 仍完整保留为既有 compatibility。
		return handlerResult{tokens: tokens, authority: totalAuthorityPreserve, actions: actions, violations: violations}
	}
	authority := totalAuthorityZeroOnly
	if len(violations) == 0 && context.resolution.evidenceStrength == EvidenceParserContract &&
		!context.clamped.hasAny("input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens", "cache_creation_tokens") {
		// 已知 Codex/xAI executor 才证明 Input/Output 已含子项，允许 final reconciler 强校验 Total。
		authority = totalAuthorityCanonical
	}
	// cache alias 或父子校验字段损坏时只撤销非零 Total 强纠错；旧零值补法仍按自己的 Input/Output 依赖执行。
	return handlerResult{tokens: tokens, authority: authority, actions: actions, violations: violations}
}

func matchesLegacyCodexCachedOnlyShape(context normalizationContext) bool {
	// 该判断只保护 Keeper 已上线的旧结果，不宣称 payload 能唯一证明 additional-model 来源。
	if context.resolution.evidenceStrength != EvidenceParserContract || normalizeRegistryAlias(context.resolution.executorType) != "codexexecutor" {
		return false
	}
	// 任一参与字段曾由负数清零时都不是已验证的旧形态，不能借兼容分支隐藏损坏数据。
	if context.clamped.hasAny("input_tokens", "output_tokens", "reasoning_tokens", "cached_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens") {
		return false
	}
	// 必须读取 cache alias 执行前的非负快照，避免 read 已回填后反向制造命中条件。
	tokens := context.nonNegative
	// PublishAdditionalModel 可能只给出 cached，普通 parser 也允许相同零父级形态；因此这里只识别旧兼容外形，不能反推来源或 Input。
	if tokens.InputTokens != 0 || tokens.OutputTokens != 0 || tokens.ReasoningTokens != 0 {
		return false
	}
	// ParseOpenAIStyleUsageNode 把 input_tokens_details.cached_tokens 写入 cached；显式 read/creation 存在时不再属于已验证旧形态。
	if tokens.CachedTokens <= 0 || tokens.CacheReadTokens != 0 || tokens.CacheCreationTokens != 0 {
		return false
	}
	// normalizeUsageDetailTotal 无法从零父级补值，只有 usageQueuePlugin.HandleUsage 会退到 cached，因此 Total 必须精确等于 cached。
	return tokens.TotalTokens == tokens.CachedTokens
}
