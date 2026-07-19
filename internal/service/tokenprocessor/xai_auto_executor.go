package tokenprocessor

// XAIAutoExecutor 维护说明：CPA Auto 当前委托 HTTP/WebSocket executor，仍需兼容历史或未来 reporter 归属；
// CPA 更新时需复核委托路径、NewExecutorUsageReporter 与最终 ExecutorTypeName。
// 还需复核 ParseCodexUsage/ParseOpenAIUsage、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，确认 Auto alias 仍符合 Responses 口径。
var xaiAutoExecutorDefinition = executorDefinition{
	// 防御性 alias 避免 Auto 类型出现时退回 identity 查询。
	alias: "XAIAutoExecutor",
	// Auto 委托不改变 Responses Token 语义，因此复用 Responses handler。
	handlerID: HandlerResponsesInclusive,
}
