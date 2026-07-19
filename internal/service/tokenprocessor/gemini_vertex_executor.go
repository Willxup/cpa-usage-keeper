package tokenprocessor

// GeminiVertexExecutor 维护说明：CPA Vertex 路径复用 Gemini prompt/candidates/thoughts 字段合同；
// CPA 更新时需复核 ParseGeminiUsage/Stream、NewExecutorUsageReporter 与 Vertex reporter 创建位置。
// 同时复核 ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，确认 Vertex 仍可复用 Gemini Total 权限。
var geminiVertexExecutorDefinition = executorDefinition{
	// 独立 alias 保留 Vertex executor 的唯一维护入口。
	alias: "GeminiVertexExecutor",
	// 字段包含关系与 Gemini 相同，因此直接引用 Gemini handler，不复制算法。
	handlerID: HandlerGemini,
}
