package tokenprocessor

// XAIExecutor 维护说明：CPA 当前调用 /v1/responses，Input 已含 cache、Output 已含 reasoning；
// CPA 更新时需复核 /responses 的 ParseCodexUsage、/compact 的 ParseOpenAIUsage 以及 reporter 创建位置。
// 同时复核 NewExecutorUsageReporter、ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，避免端点变化破坏 inclusive Total 合同。
var xaiExecutorDefinition = executorDefinition{
	// alias 与 CPA xAI HTTP executor 类型完全对应。
	alias: "XAIExecutor",
	// 当前字段合同与 Responses inclusive 相同，因此复用 Responses handler。
	handlerID: HandlerResponsesInclusive,
}
