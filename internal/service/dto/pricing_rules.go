package dto

// PricingRule 是规范化后返回给 API/UI 的规则。
type PricingRule struct {
	Key        string
	Value      string
	Multiplier float64
}

// PricingRuleInput 用指针区分省略/null 默认值与显式 0。
type PricingRuleInput struct {
	Key        string
	Value      string
	Multiplier *float64
}

type ReplacePricingRulesInput struct {
	Model string
	Rules []PricingRuleInput
}
