package tokenprocessor

// Process 是生产 Token 处理的唯一入口；它只编排纯 Token 快照、协议 handler 和最终结果汇总。
func Process(values TokenValues, resolution HandlerResolution) ProcessResult {
	// 生成非负工作值时，clamp action 直接记录原值，clamped 集合保留字段曾经损坏的事实。
	nonNegative, clampActions, clamped := clampTokenValues(values)
	// 包外无法实现带私有标记的 resolution；nil 仍按 strict default 防御，绝不授予 parser contract。
	resolved, ok := resolution.(handlerResolution)
	if !ok {
		resolved = handlerResolution{handlerID: HandlerStrictPassThrough, evidenceSource: EvidenceDefault, evidenceStrength: EvidenceUnknown}
	}

	// handler registry 只按 resolver 已选 HandlerID 取实现，不再次读取 executor/provider 名称做第二套路由。
	handler, exists := tokenHandlers[resolved.handlerID]
	if !exists {
		// 静态 registry 理论上已阻止悬空引用；防御分支仍保持 strict，避免意外数据修改。
		handler = strictPassThroughHandler{}
		resolved.handlerID = HandlerStrictPassThrough
		resolved.evidenceSource = EvidenceDefault
		resolved.evidenceStrength = EvidenceUnknown
	}

	// handler 只看到 Token 快照和不可伪造的证据，不接触数据库、日志或完整 UsageEvent。
	handled := handler.Normalize(normalizationContext{nonNegative: nonNegative, clamped: clamped, resolution: resolved})
	// 负数清零是全局 ingress 动作，必须与协议 handler 的全部 action 一起保留。
	actions := append(clampActions, handled.actions...)
	// 所有 handler 都把 authority 交给同一个 final reconciler，禁止各协议私自实现第二份 Total helper。
	tokens, totalActions, totalViolations := reconcileFinalTotal(handled.tokens, handled.authority, clamped, resolved)
	actions = append(actions, totalActions...)
	violations := append(handled.violations, totalViolations...)

	return ProcessResult{Tokens: tokens, Actions: actions, Violations: violations, Outcome: summarizeTokenOutcome(actions, violations)}
}

func clampTokenValues(values TokenValues) (TokenValues, []Action, tokenFieldSet) {
	// 每个负数字段都单独记录 before/after，便于日志说明清零了什么且防止规则使用伪造等式。
	clamped := tokenFieldSet{}
	actions := make([]Action, 0)
	clamp := func(field string, value *int64) {
		if *value >= 0 {
			return
		}
		before := *value
		*value = 0
		clamped[field] = struct{}{}
		actions = append(actions, Action{Code: ActionClampNegativeToken, Field: field, Before: before, After: 0, EvidenceSource: EvidenceDefault, Rule: "non_negative_ingress", outcome: TokenOutcomeCorrected})
	}

	// 七个 Token 字段都必须在任何协议等式前 clamp，避免负值直接进入数据库或安全加法。
	clamp("input_tokens", &values.InputTokens)
	clamp("output_tokens", &values.OutputTokens)
	clamp("reasoning_tokens", &values.ReasoningTokens)
	clamp("cached_tokens", &values.CachedTokens)
	clamp("cache_read_tokens", &values.CacheReadTokens)
	clamp("cache_creation_tokens", &values.CacheCreationTokens)
	clamp("total_tokens", &values.TotalTokens)
	return values, actions, clamped
}

func summarizeTokenOutcome(actions []Action, violations []Violation) TokenOutcome {
	// overflow 是最严重且最明确的算术失败，必须压过其它正常转换 action。
	for _, violation := range violations {
		if violation.Code == ViolationTokenArithmeticOverflow {
			return TokenOutcomeOverflow
		}
	}
	// 任何剩余 violation 都表示已确认冲突但无法唯一修复。
	if len(violations) > 0 {
		return TokenOutcomeAmbiguous
	}
	// 每个 handler 只给自己的 action 标记通用 outcome 影响，公共汇总不认识任何 executor 专属 code。
	actionOutcome := TokenOutcomeValid
	for _, action := range actions {
		if action.outcome == TokenOutcomeCorrected {
			actionOutcome = TokenOutcomeCorrected
			continue
		}
		if action.outcome == TokenOutcomeCompatibility && actionOutcome != TokenOutcomeCorrected {
			actionOutcome = TokenOutcomeCompatibility
			continue
		}
		if action.outcome == TokenOutcomeNormalized && actionOutcome == TokenOutcomeValid {
			actionOutcome = TokenOutcomeNormalized
		}
	}
	if actionOutcome != TokenOutcomeValid {
		return actionOutcome
	}
	// 未标记的未来 action 防御性归为 normalized，避免修改被错误汇总成 valid。
	if len(actions) > 0 {
		return TokenOutcomeNormalized
	}
	return TokenOutcomeValid
}
