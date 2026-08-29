package tokenprocessor

// ClaudeExecutor 维护说明：CPA ParseClaudeUsage/ParseClaudeStreamUsage 的 input 不含 cache read/creation；
// NewExecutorUsageReporter 通过 ExecutorTypeName 写入该类型名。CPA 更新时需同步复核这三个符号与 reporter 创建位置。
// 公共链路还需复核 normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，避免 CPA Total 补值变化破坏 Claude 合并口径。
var claudeExecutorDefinition = executorDefinition{
	// alias 必须与 CPA Go 类型名完全对应，resolver 才能把本次 parser 视为强合同。
	alias: "ClaudeExecutor",
	// Claude handler 负责把显式 read/creation 合入 Input，并最终保持 cached=read。
	handlerID: HandlerClaude,
}

type claudeHandler struct{}

func (claudeHandler) ID() HandlerID { return HandlerClaude }

func (claudeHandler) Normalize(context normalizationContext) handlerResult {
	// Claude 只信任 CPA 显式 read/creation；legacy cached 永远不参与 Input 推导。
	tokens := context.nonNegative
	canonicalInput, ok := safeAddTokenCounts(tokens.InputTokens, tokens.CacheReadTokens, tokens.CacheCreationTokens)
	actions := make([]Action, 0, 2)
	violations := make([]Violation, 0, 1)
	authority := totalAuthorityZeroOnly
	if context.clamped.hasAny("input_tokens", "cache_read_tokens", "cache_creation_tokens") {
		// Claude Input fold 的任一依赖字段被 clamp 时，不能把清零后的值参与合并或 Total 证明。
		// 这里只撤销强合同；不依赖损坏子项的旧零 Total 回填仍由 final reconciler 独立判断。
		authority = totalAuthorityZeroOnly
		violations = append(violations, Violation{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"input_tokens", "cache_read_tokens", "cache_creation_tokens"}, Reason: "Claude input fold depends on clamped fields"})
	} else if !ok {
		// Input fold 溢出时保留父级原值并禁止 Total 改写，但 cached=read 兼容仍可独立完成。
		authority = totalAuthorityPreserve
		violations = append(violations, Violation{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "cache_read_tokens", "cache_creation_tokens"}, Reason: "Claude canonical input exceeds int64"})
	} else {
		// 安全加法成功后一次性写回 canonical Input，避免部分 fold 留下半成品。
		if canonicalInput != tokens.InputTokens {
			actions = append(actions, Action{Code: ActionNormalizeClaudeInput, Field: "input_tokens", Before: tokens.InputTokens, After: canonicalInput, EvidenceSource: context.resolution.evidenceSource, Rule: "claude_input_excludes_cache", outcome: TokenOutcomeNormalized})
		}
		tokens.InputTokens = canonicalInput
		// 已知 ClaudeExecutor 直接证明 parser 合同；identity 只有精确旧 CPA 指纹才能升级非零 Total 权限。
		if context.resolution.evidenceStrength == EvidenceParserContract {
			authority = totalAuthorityCanonical
		} else if claudeIdentityProvesLegacyMissingCache(context, canonicalInput) {
			authority = totalAuthorityCanonical
		}
	}

	// 只有已知 ClaudeExecutor 能证明 cached/read 是同一读取量的兼容别名；identity 提示不具备这项强合同。
	if context.resolution.evidenceStrength == EvidenceParserContract && tokens.CachedTokens > 0 && tokens.CacheReadTokens > 0 && tokens.CachedTokens != tokens.CacheReadTokens {
		// 两个上游明确值不一致时记录合同冲突，但仍以结构化的显式 read 为准，不撤销 Input 与 Total 的正确计算。
		violations = append(violations, Violation{Code: ViolationCacheReadAliasConflict, Fields: []string{"cached_tokens", "cache_read_tokens"}, Reason: "Claude cached/read aliases disagree under parser contract"})
	}

	// Claude handler 返回前无条件保持 cached=read；该兼容字段不会反向参与 Input、Total 或计价。
	if tokens.CachedTokens != tokens.CacheReadTokens {
		actions = append(actions, Action{Code: ActionNormalizeClaudeCachedAlias, Field: "cached_tokens", Before: tokens.CachedTokens, After: tokens.CacheReadTokens, EvidenceSource: context.resolution.evidenceSource, Rule: "claude_cached_equals_explicit_read", outcome: TokenOutcomeNormalized})
	}
	tokens.CachedTokens = tokens.CacheReadTokens
	return handlerResult{tokens: tokens, authority: authority, actions: actions, violations: violations}
}

func claudeIdentityProvesLegacyMissingCache(context normalizationContext, canonicalInput int64) bool {
	// 该历史指纹依赖 Input、Output、read、creation、Total；任一字段被 clamp 都不能命中。
	if context.clamped.hasAny("input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens") {
		return false
	}
	// 没有显式 cache 子项就不存在“旧 Input/Total 同时漏 cache”的已验证问题。
	cacheTotal, ok := safeAddTokenCounts(context.nonNegative.CacheReadTokens, context.nonNegative.CacheCreationTokens)
	if !ok || cacheTotal <= 0 {
		return false
	}
	// 旧 Total 必须精确等于旧 Input+Output，才能证明两者同时遗漏全部 cache。
	rawTotal, ok := safeAddTokenCounts(context.nonNegative.InputTokens, context.nonNegative.OutputTokens)
	if !ok || rawTotal != context.nonNegative.TotalTokens {
		return false
	}
	// canonical Total 必须与旧 Total 不同，否则没有错误非零 Total 需要纠正。
	canonicalTotal, ok := safeAddTokenCounts(canonicalInput, context.nonNegative.OutputTokens)
	return ok && canonicalTotal != context.nonNegative.TotalTokens
}
