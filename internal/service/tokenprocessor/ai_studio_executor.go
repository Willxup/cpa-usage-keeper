package tokenprocessor

// AIStudioExecutor 维护说明：CPA AI Studio 返回 Gemini usage 结构并由 Gemini parser 映射；
// CPA 更新时需复核 ParseGeminiUsage/Stream、NewExecutorUsageReporter 与 AI Studio reporter 创建位置。
// 同时复核 ExecutorTypeName、normalizeUsageDetailTotal 与 usageQueuePlugin.HandleUsage，确认 Output/reasoning/Total 仍保持 Gemini 口径。
var aiStudioExecutorDefinition = executorDefinition{
	// 独立 alias 让 AI Studio 上游变化只修改本文件和中央 registry 引用。
	alias: "AIStudioExecutor",
	// 当前字段合同与 Gemini 相同，因此复用 Gemini handler。
	handlerID: HandlerGemini,
}
