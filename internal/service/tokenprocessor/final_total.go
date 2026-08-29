package tokenprocessor

func reconcileFinalTotal(tokens TokenValues, authority totalAuthority, clamped tokenFieldSet, resolution handlerResolution) (TokenValues, []Action, []Violation) {
	// authority 由已选 handler 证明；final reconciler 只执行对应权限，不能根据数字自行升级。
	switch authority {
	case totalAuthorityCanonical:
		return reconcileCanonicalTotal(tokens, clamped, resolution)
	case totalAuthorityZeroOnly:
		return reconcileZeroOnlyTotal(tokens, clamped, resolution)
	case totalAuthorityPreserve:
		// handler 已记录导致降级的 clamp/overflow/协议冲突，本层不得再尝试另一条推导链。
		return tokens, nil, nil
	default:
		// 未知 authority 属于实现异常，最安全行为是保留 Total 并留下可见 violation。
		return tokens, nil, []Violation{{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"total_tokens"}, Reason: "token handler returned unknown total authority"}}
	}
}

func reconcileCanonicalTotal(tokens TokenValues, clamped tokenFieldSet, resolution handlerResolution) (TokenValues, []Action, []Violation) {
	// canonical Total 依赖父级 Input/Output、用于验证包含关系的子项，以及 Total 原值是否真实为零；任一被 clamp 都必须降级。
	if clamped.hasAny("input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens") {
		return tokens, nil, []Violation{{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens"}, Reason: "canonical total depends on clamped token fields"}}
	}

	// 先安全计算 cache 子项和，才能验证 inclusive Input 没有遗漏 read/creation。
	cacheComponents, ok := safeAddTokenCounts(tokens.CacheReadTokens, tokens.CacheCreationTokens)
	if !ok {
		// cache 子项溢出只否定强合同校验；旧零 Total 补值仍可独立使用未损坏的 Input 与 Output。
		return reconcileCanonicalConflictWithZeroFallback(tokens, clamped, resolution, Violation{Code: ViolationTokenArithmeticOverflow, Fields: []string{"cache_read_tokens", "cache_creation_tokens"}, Reason: "cache token components exceed int64"})
	}
	if tokens.InputTokens < cacheComponents {
		// 子项大于父项只证明数据冲突，不能通用抬高 Input；但不影响只依赖父级 Input/Output 的旧零值补法。
		return reconcileCanonicalConflictWithZeroFallback(tokens, clamped, resolution, Violation{Code: ViolationInputBelowCache, Fields: []string{"input_tokens", "cache_read_tokens", "cache_creation_tokens"}, Reason: "canonical input is smaller than cache read plus creation"})
	}
	if tokens.OutputTokens < tokens.ReasoningTokens {
		// reasoning 大于 canonical Output 时不能通用抬高 Output；旧零值补法仍只读取现有父级 Input/Output。
		return reconcileCanonicalConflictWithZeroFallback(tokens, clamped, resolution, Violation{Code: ViolationOutputBelowReasoning, Fields: []string{"output_tokens", "reasoning_tokens"}, Reason: "canonical output is smaller than reasoning"})
	}

	// 基础包含关系成立后，Input+Output 是本次合同唯一允许的 canonical Total。
	expectedTotal, ok := safeAddTokenCounts(tokens.InputTokens, tokens.OutputTokens)
	if !ok {
		return tokens, nil, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "output_tokens", "total_tokens"}, Reason: "canonical input plus output exceeds int64"}}
	}
	if tokens.TotalTokens == expectedTotal {
		// 包括合法全零在内，已经满足合同的事件不产生 action 或日志噪音。
		return tokens, nil, nil
	}

	before := tokens.TotalTokens
	tokens.TotalTokens = expectedTotal
	if before == 0 {
		// 零 Total 回填是 Keeper 已有 compatibility，不计为本次新增异常纠正。
		return tokens, []Action{{Code: ActionBackfillZeroTotal, Field: "total_tokens", Before: before, After: expectedTotal, EvidenceSource: resolution.evidenceSource, Rule: "canonical_zero_total", outcome: TokenOutcomeCompatibility}}, nil
	}
	// 只有强合同与全部不变量同时成立时，才覆盖上游错误的非零 Total。
	return tokens, []Action{{Code: ActionCorrectNonzeroTotal, Field: "total_tokens", Before: before, After: expectedTotal, EvidenceSource: resolution.evidenceSource, Rule: "canonical_input_plus_output", outcome: TokenOutcomeCorrected}}, nil
}

func reconcileCanonicalConflictWithZeroFallback(tokens TokenValues, clamped tokenFieldSet, resolution handlerResolution, conflict Violation) (TokenValues, []Action, []Violation) {
	// 非零 Total 需要完整强合同才能覆盖；父子冲突或子项溢出后必须保留原值。
	if tokens.TotalTokens != 0 {
		return tokens, nil, []Violation{conflict}
	}
	// 真实零 Total 继续走 Keeper 已验证的旧补值规则，该规则只使用自己明确检查过的字段。
	fallbackTokens, fallbackActions, fallbackViolations := reconcileZeroOnlyTotal(tokens, clamped, resolution)
	// 原始父子冲突必须始终保留，避免补齐 Total 后把上游数据问题隐藏掉。
	violations := append([]Violation{conflict}, fallbackViolations...)
	// 同时返回旧补值动作和全部冲突，让结果既可入库又可被异常日志观察。
	return fallbackTokens, fallbackActions, violations
}

func reconcileZeroOnlyTotal(tokens TokenValues, clamped tokenFieldSet, resolution handlerResolution) (TokenValues, []Action, []Violation) {
	if tokens.TotalTokens != 0 {
		// strict handler 不知道 Output 是否含 reasoning，非零差异不构成可确认 violation，更不能被修改。
		if resolution.handlerID == HandlerStrictPassThrough {
			return tokens, nil, nil
		}
		// 其它 identity/compatibility handler 已知目标 canonical 口径，但证据不足以决定哪一个基础字段错了。
		expectedTotal, ok := safeAddTokenCounts(tokens.InputTokens, tokens.OutputTokens)
		if !ok {
			return tokens, nil, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "output_tokens", "total_tokens"}, Reason: "zero-only comparison exceeds int64"}}
		}
		if expectedTotal != tokens.TotalTokens {
			return tokens, nil, []Violation{{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"input_tokens", "output_tokens", "total_tokens"}, Reason: "nonzero total differs from canonical fields without correction authority"}}
		}
		return tokens, nil, nil
	}

	// Input、Output 或 Total 曾为负数时，清零后的值不能制造“Total 真实缺失”的 zero-only 命中条件。
	if clamped.hasAny("input_tokens", "output_tokens", "total_tokens") {
		return tokens, nil, []Violation{{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"input_tokens", "output_tokens", "total_tokens"}, Reason: "zero total fallback depends on clamped fields"}}
	}
	// 现有第一层 fallback 固定使用当前工作值 Input+Output，并通过统一安全加法防回绕。
	expectedTotal, ok := safeAddTokenCounts(tokens.InputTokens, tokens.OutputTokens)
	if !ok {
		return tokens, nil, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "output_tokens", "total_tokens"}, Reason: "zero-only input plus output exceeds int64"}}
	}
	if expectedTotal > 0 {
		before := tokens.TotalTokens
		tokens.TotalTokens = expectedTotal
		return tokens, []Action{{Code: ActionBackfillZeroTotal, Field: "total_tokens", Before: before, After: expectedTotal, EvidenceSource: resolution.evidenceSource, Rule: "existing_zero_total_input_output", outcome: TokenOutcomeCompatibility}}, nil
	}

	// 父级和仍为零时才执行 legacy cache-read fallback；这只是兼容，不证明 canonical Input 正确。
	if tokens.CacheReadTokens > 0 {
		before := tokens.TotalTokens
		tokens.TotalTokens = tokens.CacheReadTokens
		return tokens, []Action{{Code: ActionBackfillZeroTotal, Field: "total_tokens", Before: before, After: tokens.TotalTokens, EvidenceSource: resolution.evidenceSource, Rule: "existing_zero_total_cache_read", outcome: TokenOutcomeCompatibility}}, nil
	}
	return tokens, nil, nil
}
