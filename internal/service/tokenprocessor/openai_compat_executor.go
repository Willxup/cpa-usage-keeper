package tokenprocessor

import "fmt"

const actionApplyIssue272ReasoningFold = "apply_issue272_reasoning_fold"

// OpenAICompatExecutor 维护说明：CPA 通用 OpenAI parser 不能证明所有兼容 provider 的 reasoning inclusion；
// Issue #272 与未来有证据的 compatibility rules 只能留在本文件。CPA 更新时需复核 ParseOpenAIUsage/Stream。
// 还需复核 NewExecutorUsageReporter、ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，防止兼容方言被误授予标准 OpenAI 合同。
var openAICompatExecutorDefinition = executorDefinition{
	// alias 与 CPA Go 类型完全对应，只授予进入 compatibility 规则链的权限。
	alias: "OpenAICompatExecutor",
	// handler 通过有序规则判断具体方言，不把兼容协议误当标准 Responses。
	handlerID: HandlerOpenAICompatibility,
}

type openAICompatibilityHandler struct{}

func (openAICompatibilityHandler) ID() HandlerID { return HandlerOpenAICompatibility }

// compatibilityRule 把 OpenAI-compatible 方言的匹配和修改封装在本文件，禁止全局 processor 知道 #272。
type compatibilityRule interface {
	Name() string
	Match(normalizationContext) (bool, []Violation)
	Apply(TokenValues, normalizationContext) (TokenValues, []Action, []Violation)
}

// openAICompatibilityRules 是有序 first-match-wins 规则链；#272 必须在任何未来 default 规则之前执行。
var openAICompatibilityRules = []compatibilityRule{
	issue272SeparatedReasoningRule{},
}

func validateOpenAICompatibilityRules() error {
	// 规则名用于 action/log 追踪，重复会让 CPA 升级后的行为无法定位。
	seen := make(map[string]struct{}, len(openAICompatibilityRules))
	for _, rule := range openAICompatibilityRules {
		if rule == nil || rule.Name() == "" {
			return fmt.Errorf("empty OpenAI compatibility rule")
		}
		if _, exists := seen[rule.Name()]; exists {
			return fmt.Errorf("duplicate OpenAI compatibility rule %q", rule.Name())
		}
		seen[rule.Name()] = struct{}{}
	}
	return nil
}

func (openAICompatibilityHandler) Normalize(context normalizationContext) handlerResult {
	// compatibility 仍是非 Claude，先保留 cached→read；规则 Match 始终读取未改写 non-negative snapshot。
	tokens, actions, cacheViolations := normalizeNonClaudeCacheRead(context)
	violations := append([]Violation(nil), cacheViolations...)
	// #272 必须在任何 Total 重算前判断 separated reasoning；顺序错误会让可见 Output 归零并使 Gemini-compatible speed_tps 回归为 null。
	for _, rule := range openAICompatibilityRules {
		// 每条 Match 只读取原始快照与证据，不能看到前一条规则改写后的 Total。
		matched, matchViolations := rule.Match(context)
		violations = append(violations, matchViolations...)
		if !matched {
			continue
		}
		// first-match-wins 保证方言规则互斥；命中后不再让后续规则重复修改同一字段。
		updated, ruleActions, applyViolations := rule.Apply(tokens, context)
		tokens = updated
		actions = append(actions, ruleActions...)
		violations = append(violations, applyViolations...)
		break
	}
	// OpenAI Compatibility 只保留 zero-only Total 权限；#272 命中时原 Total 已与 fold 后 canonical 值一致。
	// compatibility 规则失败只阻止该规则本身；旧零 Total 回填仍由 final reconciler 按 Input/Output/Total 的实际依赖判断。
	return handlerResult{tokens: tokens, authority: totalAuthorityZeroOnly, actions: actions, violations: violations}
}

type issue272SeparatedReasoningRule struct{}

func (issue272SeparatedReasoningRule) Name() string { return "issue_272_separated_reasoning" }

func (issue272SeparatedReasoningRule) Match(context normalizationContext) (bool, []Violation) {
	// #272 的四个依赖字段任一被负数 clamp，都可能制造伪等式，必须直接拒绝。
	if context.clamped.hasAny("input_tokens", "output_tokens", "reasoning_tokens", "total_tokens") {
		return false, []Violation{{Code: ViolationAmbiguousTotalMismatch, Fields: []string{"input_tokens", "output_tokens", "reasoning_tokens", "total_tokens"}, Reason: "#272 decision depends on clamped fields"}}
	}
	tokens := context.nonNegative
	// 没有 reasoning 或 provider Total 时，现有 #272 合同没有足够信息证明 separated reasoning。
	if tokens.ReasoningTokens <= 0 || tokens.TotalTokens <= 0 {
		return false, nil
	}
	// 标准 inclusive OpenAI 形态满足 Input+Output=Total，必须先排除以防重复 fold。
	inclusiveTotal, ok := safeAddTokenCounts(tokens.InputTokens, tokens.OutputTokens)
	if !ok {
		return false, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "output_tokens"}, Reason: "#272 inclusive total exceeds int64"}}
	}
	if inclusiveTotal == tokens.TotalTokens {
		return false, nil
	}
	// 只有 Input+Output+Reasoning 精确等于原 Total，才能证明 Gemini-compatible Output 与 reasoning 分列；这也是保住可见输出速度的唯一修复证据。
	separatedTotal, ok := safeAddTokenCounts(tokens.InputTokens, tokens.OutputTokens, tokens.ReasoningTokens)
	if !ok {
		return false, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"input_tokens", "output_tokens", "reasoning_tokens"}, Reason: "#272 separated total exceeds int64"}}
	}
	return separatedTotal == tokens.TotalTokens, nil
}

func (issue272SeparatedReasoningRule) Apply(tokens TokenValues, context normalizationContext) (TokenValues, []Action, []Violation) {
	// Match 已证明 Output+Reasoning 可参与完整等式，Apply 仍通过同一安全加法防止实现漂移和 speed_tps 再次变空。
	canonicalOutput, ok := safeAddTokenCounts(tokens.OutputTokens, tokens.ReasoningTokens)
	if !ok {
		return tokens, nil, []Violation{{Code: ViolationTokenArithmeticOverflow, Fields: []string{"output_tokens", "reasoning_tokens"}, Reason: "#272 canonical output exceeds int64"}}
	}
	before := tokens.OutputTokens
	tokens.OutputTokens = canonicalOutput
	return tokens, []Action{{Code: actionApplyIssue272ReasoningFold, Field: "output_tokens", Before: before, After: canonicalOutput, EvidenceSource: context.resolution.evidenceSource, Rule: "issue_272_separated_reasoning", outcome: TokenOutcomeCompatibility}}, nil
}
