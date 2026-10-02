package pricing

import (
	"time"

	"cpa-usage-keeper/internal/helper"
)

// CostSubject 是所有 usage 来源进入计价领域的唯一固定输入。
type CostSubject struct {
	Dimensions UsageDimensions
	Tokens     helper.UsageTokenCostInput
	// Timestamp 是已存 CPA 事件时间，供每日时段分支匹配。
	Timestamp time.Time
}

// NewCostSubject 规范化事件身份字段；时间由调用方从 CPA 已存 timestamp 填入。
func NewCostSubject(dimensions UsageDimensions, tokens helper.UsageTokenCostInput) CostSubject {
	return CostSubject{
		Dimensions: canonicalizeUsageDimensions(dimensions),
		Tokens:     tokens,
	}
}

// Resolver 在创建时固定绑定一个 Snapshot，确保同一事件批次不会混用新旧价格。
type Resolver struct {
	snapshot *Snapshot
}

// CalculateFee 用本批固定配置选择整条请求的单价及倍率，仅返回 USD 总额和可用性。
// 仅事件入库、首次回填及显式重算写回调用；不写数据库，也不暴露分项或匹配追溯。
func (r Resolver) CalculateFee(subject CostSubject) FeeResult {
	model, found := r.matchModel(subject.Dimensions)
	if !found {
		return FeeResult{Available: !helper.UsageTokenInputRequiresPricing(subject.Tokens)}
	}

	// 分支在编译时已校验互斥；这里仅按归一化输入量及已存 CPA 时间挑一组整请求单价。
	selected := model.pricing
	for _, branch := range model.branches {
		if !branch.matches(subject, r.snapshot.location) {
			continue
		}
		selected.PromptPricePer1M = branch.prices.Input
		selected.CompletionPricePer1M = branch.prices.Output
		selected.CacheReadPricePer1M = branch.prices.CacheRead
		selected.CacheWritePricePer1M = branch.prices.CacheWrite
		break
	}
	total := helper.CalculateUsageTokenCost(subject.Tokens, selected)
	if model.pricing.PriceMultiplier == nil || *model.pricing.PriceMultiplier != 0 {
		total *= matchingRuleMultiplier(model.rules, subject.Dimensions)
	}
	return FeeResult{TotalCostUSD: total, Available: true}
}

// matchModel 先匹配实际 model，未命中才使用 alias，保持既有模型选择顺序。
func (r Resolver) matchModel(dimensions UsageDimensions) (compiledModel, bool) {
	if r.snapshot == nil {
		return compiledModel{}, false
	}
	if model, ok := r.snapshot.modelsByName[dimensions.Model]; ok {
		return model, true
	}
	if model, ok := r.snapshot.modelsByName[dimensions.ModelAlias]; ok {
		return model, true
	}
	return compiledModel{}, false
}

// matchingRuleMultiplier 连乘所有精确匹配的条件；任一零倍率使整条请求免费。
func matchingRuleMultiplier(rules []compiledRule, dimensions UsageDimensions) float64 {
	multiplier := 1.0
	for _, rule := range rules {
		if dimensions.Value(rule.field) != rule.value {
			continue
		}
		if rule.multiplier == 0 {
			return 0
		}
		multiplier *= rule.multiplier
	}
	return multiplier
}
