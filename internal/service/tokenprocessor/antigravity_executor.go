package tokenprocessor

// AntigravityExecutor 维护说明：CPA Antigravity usage 仍按 Gemini prompt/candidates/thoughts 语义映射；
// CPA 更新时需复核 ParseAntigravityUsage/ParseAntigravityStreamUsage、EnsurePublished 与 reporter 创建位置。
// 同时复核 NewExecutorUsageReporter、ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，避免委托或补值语义漂移。
var antigravityExecutorDefinition = executorDefinition{
	// 独立 alias 防止 Antigravity 后续协议变化污染 Gemini 全局路由。
	alias: "AntigravityExecutor",
	// 当前已验证字段合同与 Gemini 相同，因此复用 Gemini handler。
	handlerID: HandlerGemini,
}
