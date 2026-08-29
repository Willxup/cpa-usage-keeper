package tokenprocessor

// GeminiCLIExecutor 维护说明：当前 CPA 已不再注册该类型，但历史 payload 仍可能携带此 alias；
// CPA 更新时需搜索 ExecutorTypeName 和历史 Gemini CLI reporter，确认是否恢复、改名或彻底移除。
// 若重新出现，还需复核 NewExecutorUsageReporter、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage 后才能继续授予 Gemini 合同。
var geminiCLIExecutorDefinition = executorDefinition{
	// 历史 alias 必须继续 exact match，避免旧 inbox 退回不明确 identity 路径。
	alias: "GeminiCLIExecutor",
	// 历史字段合同与 Gemini 相同，因此复用 Gemini handler。
	handlerID: HandlerGemini,
}
