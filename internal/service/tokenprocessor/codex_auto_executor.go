package tokenprocessor

// CodexAutoExecutor 维护说明：CPA Auto 当前委托 HTTP/WebSocket executor，历史或未来 payload 仍可能写入 Auto 类型；
// CPA 更新时需复核委托路径、NewExecutorUsageReporter 与最终 ExecutorTypeName 归属。
// 还需复核 ParseCodexUsage/ParseOpenAIUsage、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，确认 Auto alias 仍能复用 Responses 合同。
var codexAutoExecutorDefinition = executorDefinition{
	// 防御性 alias 避免 Auto reporter 归属调整后退回 identity 猜测。
	alias: "CodexAutoExecutor",
	// Auto 委托不改变 Responses Token 语义，因此复用 Responses handler。
	handlerID: HandlerResponsesInclusive,
}
