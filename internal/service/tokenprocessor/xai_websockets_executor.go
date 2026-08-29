package tokenprocessor

// XAIWebsocketsExecutor 维护说明：WebSocket transport 仍消费 Responses usage 字段；
// CPA 更新时需复核 WebSocket response.completed/response.done 的 ParseCodexUsage 与 HTTP 回退委托。
// 同时复核 NewExecutorUsageReporter、ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，确认传输方式不改变字段包含关系。
var xaiWebsocketsExecutorDefinition = executorDefinition{
	// 独立 alias 让 transport 特有变更不污染 XAIExecutor 定义。
	alias: "XAIWebsocketsExecutor",
	// 当前 Token 字段合同与 xAI HTTP 相同，因此复用 Responses handler。
	handlerID: HandlerResponsesInclusive,
}
