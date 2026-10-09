package tokenprocessor

// MetaExecutor 维护说明：CPA MetaExecutor 的 Execute/ExecuteStream 都把上游
// Responses response.completed/response.incomplete 交给 ParseCodexUsage；该
// parser 的 input_tokens 已含 cache、output_tokens 已含 reasoning，total_tokens
// 是两者之和。因此 Meta 只复用 Responses inclusive handler，不按 provider 名称路由。
// CPA 更新时需复核 meta_executor_execute.go、meta_executor_stream.go 以及
// helps.ParseCodexUsage 的字段合同和 reporter 发布路径。
var metaExecutorDefinition = executorDefinition{
	// alias 与 CPA MetaExecutor Go 类型完全对应，保留 executor-first 路由证据。
	alias: "MetaExecutor",
	// Meta 使用与 Codex 相同的 Responses inclusive token 合同。
	handlerID: HandlerResponsesInclusive,
}
