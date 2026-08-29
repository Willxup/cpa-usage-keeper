package tokenprocessor

// KimiExecutor 维护说明：CPA Kimi 普通路径调用 ParseOpenAIUsage，但这不能证明 Output 是否包含 reasoning；
// Claude 入站会直接委托 ClaudeExecutor。CPA 更新时需复核两条 reporter 创建与委托路径。
// 同时复核 NewExecutorUsageReporter、ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage；没有新证据时继续保持 strict。
var kimiExecutorDefinition = executorDefinition{
	// alias 只证明通用 OpenAI parser 来源，不能按名称升级为 Responses inclusive。
	alias: "KimiExecutor",
	// 普通 Kimi 路径保持现有 strict 规则；Claude 委托由 ClaudeExecutor 自然路由。
	handlerID: HandlerStrictPassThrough,
}

type strictPassThroughHandler struct{}

func (strictPassThroughHandler) ID() HandlerID { return HandlerStrictPassThrough }

func (strictPassThroughHandler) Normalize(context normalizationContext) handlerResult {
	// Kimi/unknown 不拥有 reasoning inclusion 合同，只保留非 Claude cache alias 和 zero-only Total。
	tokens, actions, violations := normalizeNonClaudeCacheRead(context)
	// cache alias 自己的损坏不能关闭只依赖 Input/Output 的既有零 Total 回填。
	return handlerResult{tokens: tokens, authority: totalAuthorityZeroOnly, actions: actions, violations: violations}
}
