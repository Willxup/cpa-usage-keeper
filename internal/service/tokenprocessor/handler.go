package tokenprocessor

// totalAuthority 是 handler 对最终 Total 可证明范围的内部枚举，调用方不能自行授予权限。
type totalAuthority string

const (
	// totalAuthorityCanonical 表示 Input/Output 已是 Keeper canonical 口径，可由公共 reconciler 强校验 Total。
	totalAuthorityCanonical totalAuthority = "canonical_total"
	// totalAuthorityZeroOnly 表示只允许执行现有 Total 为零 fallback，非零冲突必须保留。
	totalAuthorityZeroOnly totalAuthority = "zero_only_compatibility"
	// totalAuthorityPreserve 表示基础字段损坏或溢出，本次不得改写 Total。
	totalAuthorityPreserve totalAuthority = "preserve_total"
)

// tokenHandler 是包内纯计算边界；数据库、日志和完整 UsageEvent 永远留在 sync 层。
type tokenHandler interface {
	ID() HandlerID
	Normalize(normalizationContext) handlerResult
}

// normalizationContext 将非负快照、损坏字段集合与路由证据一起交给协议 handler。
type normalizationContext struct {
	nonNegative TokenValues
	clamped     tokenFieldSet
	resolution  handlerResolution
}

// handlerResult 让每个协议只声明字段转换和 Total 权限，最终 Total 统一由公共 reconciler 决定。
type handlerResult struct {
	tokens     TokenValues
	authority  totalAuthority
	actions    []Action
	violations []Violation
}

// tokenFieldSet 记录哪些原始字段曾为负数，防止清零后的伪等式获得纠错权限。
type tokenFieldSet map[string]struct{}

func (fields tokenFieldSet) has(field string) bool {
	// clamped set 只记录真实损坏字段；查询不存在字段时明确返回 false。
	_, exists := fields[field]
	return exists
}

func (fields tokenFieldSet) hasAny(names ...string) bool {
	// 任一依赖字段被负数清零，就不能让清零后的等式获得强纠错权限。
	for _, name := range names {
		if fields.has(name) {
			return true
		}
	}
	return false
}

func normalizeNonClaudeCacheRead(context normalizationContext) (TokenValues, []Action, []Violation) {
	// handler 共用 non-negative 工作值，但仍必须读取 clamped set 判断零值是否真实来自上游。
	tokens := context.nonNegative
	if context.clamped.has("cache_read_tokens") {
		// legacy cached 没有值时本来就不会执行别名回填；此时只保留全局负数清零记录，不制造无关冲突。
		if tokens.CachedTokens <= 0 {
			return tokens, nil, nil
		}
		// 损坏的显式 read 不能被 cached 覆盖，否则会把 clamp 伪装成“字段缺失”的正常 alias。
		return tokens, nil, []Violation{{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"cached_tokens", "cache_read_tokens"}, Reason: "cache read alias depends on a clamped read field"}}
	}
	if tokens.CacheReadPresent && context.resolution.evidenceStrength == EvidenceParserContract {
		// 只有已登记 CPA executor 的 parser 合同能证明显式零值是 canonical；custom/unknown 仍需 legacy 兜底。
		return tokens, nil, nil
	}
	// 非 Claude 只有在显式 read 缺失时才允许 legacy cached 单向回填，避免覆盖更精确的 read。
	if tokens.CacheReadTokens != 0 || tokens.CachedTokens <= 0 {
		return tokens, nil, nil
	}

	// cached 是 CPA 历史兼容字段；回填 read 只统一结构，不再把 cached 额外加入 Input 或 Total。
	before := tokens.CacheReadTokens
	tokens.CacheReadTokens = tokens.CachedTokens
	return tokens, []Action{{
		Code:           ActionBackfillCacheReadAlias,
		Field:          "cache_read_tokens",
		Before:         before,
		After:          tokens.CacheReadTokens,
		EvidenceSource: context.resolution.evidenceSource,
		Rule:           "non_claude_cached_to_read_alias",
		outcome:        TokenOutcomeCompatibility,
	}}, nil
}
