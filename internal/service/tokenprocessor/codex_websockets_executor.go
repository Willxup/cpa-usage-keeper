package tokenprocessor

// CodexWebsocketsExecutor 维护说明：WebSocket 只改变传输，usage 仍由 Codex/Responses parser 产生；
// CPA 更新时需复核 WebSocket reporter、HTTP fallback 与 ParseCodexUsage/ParseOpenAIUsage。
// 同时复核 NewExecutorUsageReporter、ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，确认传输回退没有改变 Total 合同。
var codexWebsocketsExecutorDefinition = executorDefinition{
	// 独立 alias 保留 transport 变更后的精确维护入口。
	alias: "CodexWebsocketsExecutor",
	// Token 字段合同与 Codex HTTP 相同，因此复用 Responses handler。
	handlerID: HandlerResponsesInclusive,
}
