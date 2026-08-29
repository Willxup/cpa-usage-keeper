package repository

import "cpa-usage-keeper/internal/pricing"

type usagePricingDimensionColumn struct {
	field  pricing.RuleField
	column string
}

var optionalUsagePricingDimensionColumns = [...]usagePricingDimensionColumn{
	{field: pricing.RuleFieldAPIGroupKey, column: "api_group_key"},
	{field: pricing.RuleFieldAuthIndex, column: "auth_index"},
	{field: pricing.RuleFieldServiceTier, column: "service_tier"},
	{field: pricing.RuleFieldResponseServiceTier, column: "response_service_tier"},
	{field: pricing.RuleFieldReasoningEffort, column: "reasoning_effort"},
	{field: pricing.RuleFieldEndpoint, column: "endpoint"},
	{field: pricing.RuleFieldExecutorType, column: "executor_type"},
}

// UsagePricingDimensionColumns 只从编译枚举生成固定 SQL 列；model/model_alias 始终保留。
func UsagePricingDimensionColumns(active pricing.ActiveFields) []string {
	columns := make([]string, 0, 2+active.Len())
	columns = append(columns, "model", "model_alias")
	for _, dimension := range optionalUsagePricingDimensionColumns {
		if active.Has(dimension.field) {
			columns = append(columns, dimension.column)
		}
	}
	return columns
}
