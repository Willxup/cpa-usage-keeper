package dto

// ModelPriceRuleInput 是 repository 已规范化的完整规则写入项。
type ModelPriceRuleInput struct {
	Key        string
	Value      string
	Multiplier float64
}
