package tokenprocessor

// DevinExecutor 维护说明：CPA DevinExecutor 的 Execute/ExecuteStream 都通过
// ParseInteractionsUsage/ParseInteractionsStreamUsage 处理 Interactions usage。
// parser 将 total_input_tokens 与 total_tool_use_tokens 合并为 Input，将
// total_output_tokens 保持为非 reasoning Output，将 total_thought_tokens 单列，
// 并在完整合同成立时令 Total=Input+Output+Reasoning。CPA 同时允许 partial/error
// payload 缺失部分字段而保留非零 Total；在 Keeper 未读取 token_breakdown 完整性
// 之前不能授予 canonical correction，因此 Devin 有意保持 strict pass-through。
// CPA 更新时需复核
// devin_executor.go、helps/usage_helpers.go 的 Interactions parser 与 reporter 路径。
var devinExecutorDefinition = executorDefinition{
	// alias 与 CPA DevinExecutor Go 类型完全对应，不通过 provider 名称猜测规则。
	alias: "DevinExecutor",
	// partial/error usage 的完整性无法从当前 Redis scalar DTO 证明，保留 strict 安全边界。
	handlerID: HandlerStrictPassThrough,
}
