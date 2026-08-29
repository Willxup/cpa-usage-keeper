package tokenprocessor

// GeminiExecutor 维护说明：CPA ParseGeminiUsage/Stream 将 candidates 与 thoughts 分列；
// NewExecutorUsageReporter/ExecutorTypeName 决定该 alias。CPA 更新时需复核 parser 与 reporter 创建位置。
// 公共链路还需复核 normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，防止 reasoning 被 reporter/queue 重复计入 Total。
var geminiExecutorDefinition = executorDefinition{
	// alias 与 CPA Go 类型名一一对应，避免按模型名猜测 Gemini 语义。
	alias: "GeminiExecutor",
	// Gemini handler 负责把独立 reasoning 合入 Output，并在合同成立时校验 Total。
	handlerID: HandlerGemini,
}

type geminiHandler struct{}

func (geminiHandler) ID() HandlerID { return HandlerGemini }

func (geminiHandler) Normalize(context normalizationContext) handlerResult {
	// Gemini 不是 Claude，先保留非 Claude cached→read 单向兼容。
	tokens, actions, cacheViolations := normalizeNonClaudeCacheRead(context)
	violations := append([]Violation(nil), cacheViolations...)
	authority := totalAuthorityZeroOnly
	shouldFold := false

	if context.clamped.hasAny("output_tokens", "reasoning_tokens") {
		// Gemini fold 的父项或子项被 clamp 时，不能用清零值构造新的 canonical Output。
		violations = append(violations, Violation{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"output_tokens", "reasoning_tokens"}, Reason: "Gemini output fold depends on clamped fields"})
	} else if context.resolution.evidenceStrength == EvidenceParserContract {
		// 已知 Gemini/Vertex/AIStudio/Antigravity parser 已证明 Output 与 reasoning 分列。
		shouldFold = tokens.ReasoningTokens > 0
		// 强 Total 还要求所有父子校验字段都未被 clamp；否则只保留与损坏字段无关的旧零值补法。
		if len(violations) == 0 && !context.clamped.hasAny("input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens", "cache_creation_tokens") {
			authority = totalAuthorityCanonical
		}
	} else {
		// identity fallback 必须原样保留旧四分支判断，且只使用未格式改写的非负快照。
		var foldViolations []Violation
		shouldFold, foldViolations = shouldFoldGeminiIdentityReasoning(context)
		violations = append(violations, foldViolations...)
	}

	if shouldFold {
		// reasoning fold 必须走唯一安全加法，溢出时不能写回半成品 Output。
		canonicalOutput, ok := safeAddTokenCounts(tokens.OutputTokens, tokens.ReasoningTokens)
		if !ok {
			violations = append(violations, Violation{Code: ViolationTokenArithmeticOverflow, Fields: []string{"output_tokens", "reasoning_tokens"}, Reason: "Gemini canonical output exceeds int64"})
			authority = totalAuthorityPreserve
		} else {
			actions = append(actions, Action{Code: ActionNormalizeGeminiOutput, Field: "output_tokens", Before: tokens.OutputTokens, After: canonicalOutput, EvidenceSource: context.resolution.evidenceSource, Rule: "gemini_output_excludes_reasoning", outcome: TokenOutcomeNormalized})
			tokens.OutputTokens = canonicalOutput
		}
	}
	return handlerResult{tokens: tokens, authority: authority, actions: actions, violations: violations}
}

func shouldFoldGeminiIdentityReasoning(context normalizationContext) (bool, []Violation) {
	// 等式和 total==0 分支都依赖 Input/Output/Reasoning/Total；任何 clamp 都必须先于分支判断拒绝规则。
	if context.clamped.hasAny("input_tokens", "output_tokens", "reasoning_tokens", "total_tokens") {
		return false, []Violation{{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"input_tokens", "output_tokens", "reasoning_tokens", "total_tokens"}, Reason: "Gemini identity decision depends on clamped fields"}}
	}
	// reasoning 不存在时无需 fold，也不执行无意义的等式推断。
	if context.nonNegative.ReasoningTokens <= 0 {
		return false, nil
	}
	// 旧规则在 Total 缺失时默认 Gemini reasoning 分列，随后由 zero-only fallback 补 Total。
	if context.nonNegative.TotalTokens == 0 {
		return true, nil
	}
	// Input+Output 已等于 Total 时，当前 Output 已是 inclusive，必须避免重复 fold。
	inclusiveTotal, ok := safeAddTokenCounts(context.nonNegative.InputTokens, context.nonNegative.OutputTokens)
	if !ok {
		return false, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "output_tokens"}, Reason: "Gemini identity inclusive total exceeds int64"}}
	}
	if inclusiveTotal == context.nonNegative.TotalTokens {
		return false, nil
	}
	// 只有 Input+Output+Reasoning 精确等于原 Total 时，旧 identity 规则才证明 reasoning 分列。
	separatedTotal, ok := safeAddTokenCounts(context.nonNegative.InputTokens, context.nonNegative.OutputTokens, context.nonNegative.ReasoningTokens)
	if !ok {
		return false, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "output_tokens", "reasoning_tokens"}, Reason: "Gemini identity separated total exceeds int64"}}
	}
	return separatedTotal == context.nonNegative.TotalTokens, nil
}
