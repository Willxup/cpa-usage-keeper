package pricing

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/entities"
)

// RuleConfig 是持久化规则的领域表示。CompileSnapshot 会统一规范化 Key 和 Value。
type RuleConfig struct {
	Key        string
	Value      string
	Multiplier float64
}

// ModelConfig 将一条模型价格与只属于该价格的规则集合绑定。
type ModelConfig struct {
	Pricing entities.ModelPriceSetting
	Rules   []RuleConfig
}

type compiledRule struct {
	field      RuleField
	value      string
	multiplier float64
}

type compiledModel struct {
	pricing entities.ModelPriceSetting
	rules   []compiledRule
}

// Snapshot 是编译后只读的完整价格目录。内部集合在发布后不再修改。
type Snapshot struct {
	modelsByName map[string]compiledModel
	modelConfigs []ModelConfig
	activeFields ActiveFields
}

// CompileSnapshot 规范化并校验完整价格集合，只有整个候选集合安全时才返回快照。
func CompileSnapshot(configs []ModelConfig) (*Snapshot, error) {
	snapshot := &Snapshot{
		modelsByName: make(map[string]compiledModel, len(configs)),
		modelConfigs: make([]ModelConfig, 0, len(configs)),
	}
	for index := range configs {
		compiled, normalized, activeFields, err := compileModelConfig(configs[index])
		if err != nil {
			return nil, fmt.Errorf("compile model price at index %d: %w", index, err)
		}
		model := normalized.Pricing.Model
		if _, exists := snapshot.modelsByName[model]; exists {
			return nil, fmt.Errorf("duplicate model price %q", model)
		}
		snapshot.modelsByName[model] = compiled
		snapshot.modelConfigs = append(snapshot.modelConfigs, normalized)
		snapshot.activeFields |= activeFields
	}
	sort.Slice(snapshot.modelConfigs, func(i, j int) bool {
		return snapshot.modelConfigs[i].Pricing.Model < snapshot.modelConfigs[j].Pricing.Model
	})
	return snapshot, nil
}

func compileModelConfig(config ModelConfig) (compiledModel, ModelConfig, ActiveFields, error) {
	pricing := cloneModelPriceSetting(config.Pricing)
	pricing.Model = strings.TrimSpace(pricing.Model)
	if pricing.Model == "" {
		return compiledModel{}, ModelConfig{}, 0, fmt.Errorf("model is required")
	}
	if err := validatePricingNumbers(pricing); err != nil {
		return compiledModel{}, ModelConfig{}, 0, err
	}

	normalizedRules := make([]RuleConfig, 0, len(config.Rules))
	compiledRules := make([]compiledRule, 0, len(config.Rules))
	seen := make(map[ruleIdentity]struct{}, len(config.Rules))
	var activeFields ActiveFields
	for index := range config.Rules {
		rule, compiledRuleValue, err := compileRule(config.Rules[index])
		if err != nil {
			return compiledModel{}, ModelConfig{}, 0, fmt.Errorf("rule at index %d: %w", index, err)
		}
		identity := ruleIdentity{key: rule.Key, value: rule.Value}
		if _, exists := seen[identity]; exists {
			return compiledModel{}, ModelConfig{}, 0, fmt.Errorf("duplicate rule %s=%q", rule.Key, rule.Value)
		}
		seen[identity] = struct{}{}
		normalizedRules = append(normalizedRules, rule)
		if rule.Multiplier == 1 {
			continue
		}
		compiledRules = append(compiledRules, compiledRuleValue)
		activeFields = activeFields.with(compiledRuleValue.field)
	}

	if err := validateWorstCaseCost(pricing, compiledRules); err != nil {
		return compiledModel{}, ModelConfig{}, 0, err
	}
	normalized := ModelConfig{Pricing: cloneModelPriceSetting(pricing), Rules: cloneRules(normalizedRules)}
	return compiledModel{pricing: pricing, rules: compiledRules}, normalized, activeFields, nil
}

type ruleIdentity struct {
	key   string
	value string
}

func compileRule(input RuleConfig) (RuleConfig, compiledRule, error) {
	field, err := ParseRuleField(input.Key)
	if err != nil {
		return RuleConfig{}, compiledRule{}, err
	}
	value := strings.TrimSpace(input.Value)
	if value == "" {
		return RuleConfig{}, compiledRule{}, fmt.Errorf("rule value is required")
	}
	if !isNonNegativeFinite(input.Multiplier) {
		return RuleConfig{}, compiledRule{}, fmt.Errorf("rule multiplier must be a finite non-negative number")
	}
	normalized := RuleConfig{Key: field.String(), Value: value, Multiplier: input.Multiplier}
	return normalized, compiledRule{field: field, value: value, multiplier: input.Multiplier}, nil
}

func validatePricingNumbers(pricing entities.ModelPriceSetting) error {
	for name, value := range map[string]float64{
		"prompt price":      pricing.PromptPricePer1M,
		"completion price":  pricing.CompletionPricePer1M,
		"cache read price":  pricing.CacheReadPricePer1M,
		"cache write price": pricing.CacheWritePricePer1M,
	} {
		if !isNonNegativeFinite(value) {
			return fmt.Errorf("%s must be a finite non-negative number", name)
		}
	}
	multiplier := 1.0
	if pricing.PriceMultiplier != nil {
		multiplier = *pricing.PriceMultiplier
	}
	if !isNonNegativeFinite(multiplier) {
		return fmt.Errorf("model price multiplier must be a finite non-negative number")
	}
	pricing.PriceMultiplier = &multiplier
	return nil
}

func validateWorstCaseCost(pricing entities.ModelPriceSetting, rules []compiledRule) error {
	modelMultiplier := 1.0
	if pricing.PriceMultiplier != nil {
		modelMultiplier = *pricing.PriceMultiplier
	}
	// 模型整体倍率为 0 时所有 token 成本恒为 0，无需计算可能很大的规则乘积。
	if modelMultiplier == 0 {
		return nil
	}

	maxByField := [ruleFieldCount]float64{}
	for field := RuleFieldAPIGroupKey; field < ruleFieldCount; field++ {
		maxByField[field] = 1
	}
	for _, rule := range rules {
		if rule.multiplier > maxByField[rule.field] {
			maxByField[rule.field] = rule.multiplier
		}
	}
	maxRuleMultiplier := 1.0
	for field := RuleFieldAPIGroupKey; field < ruleFieldCount; field++ {
		var ok bool
		maxRuleMultiplier, ok = safeMultiply(maxRuleMultiplier, maxByField[field])
		if !ok {
			return fmt.Errorf("combined pricing rule multiplier is not finite")
		}
	}
	totalMultiplier, ok := safeMultiply(modelMultiplier, maxRuleMultiplier)
	if !ok {
		return fmt.Errorf("combined model and rule multiplier is not finite")
	}

	maxTokensPerMillion := float64(math.MaxInt64) / 1_000_000
	unscaledTotal := 0.0
	scaledTotal := 0.0
	for _, price := range []float64{
		pricing.PromptPricePer1M,
		pricing.CompletionPricePer1M,
		pricing.CacheReadPricePer1M,
		pricing.CacheWritePricePer1M,
	} {
		segment, segmentOK := safeMultiply(price, maxTokensPerMillion)
		if !segmentOK || unscaledTotal > math.MaxFloat64-segment {
			return fmt.Errorf("unscaled worst-case token cost is not finite")
		}
		unscaledTotal += segment

		segment, segmentOK = safeMultiply(segment, totalMultiplier)
		if !segmentOK || scaledTotal > math.MaxFloat64-segment {
			return fmt.Errorf("worst-case token cost is not finite")
		}
		scaledTotal += segment
	}
	return nil
}

func safeMultiply(left, right float64) (float64, bool) {
	if left == 0 || right == 0 {
		return 0, true
	}
	if left > math.MaxFloat64/right {
		return 0, false
	}
	result := left * right
	return result, !math.IsNaN(result) && !math.IsInf(result, 0)
}

func isNonNegativeFinite(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func cloneModelPriceSetting(input entities.ModelPriceSetting) entities.ModelPriceSetting {
	cloned := input
	multiplier := 1.0
	if input.PriceMultiplier != nil {
		multiplier = *input.PriceMultiplier
	}
	cloned.PriceMultiplier = &multiplier
	return cloned
}

func cloneRules(input []RuleConfig) []RuleConfig {
	if input == nil {
		return nil
	}
	return append([]RuleConfig(nil), input...)
}

// ModelConfigs 返回稳定排序的深拷贝，调用方不能修改 Snapshot 内部集合。
func (s *Snapshot) ModelConfigs() []ModelConfig {
	if s == nil {
		return []ModelConfig{}
	}
	result := make([]ModelConfig, len(s.modelConfigs))
	for index := range s.modelConfigs {
		result[index] = ModelConfig{
			Pricing: cloneModelPriceSetting(s.modelConfigs[index].Pricing),
			Rules:   cloneRules(s.modelConfigs[index].Rules),
		}
	}
	return result
}

// ModelConfig 返回指定模型配置的深拷贝。
func (s *Snapshot) ModelConfig(model string) (ModelConfig, bool) {
	if s == nil {
		return ModelConfig{}, false
	}
	compiled, ok := s.modelsByName[strings.TrimSpace(model)]
	if !ok {
		return ModelConfig{}, false
	}
	for index := range s.modelConfigs {
		if s.modelConfigs[index].Pricing.Model == compiled.pricing.Model {
			return ModelConfig{
				Pricing: cloneModelPriceSetting(s.modelConfigs[index].Pricing),
				Rules:   cloneRules(s.modelConfigs[index].Rules),
			}, true
		}
	}
	return ModelConfig{}, false
}

func (s *Snapshot) ActiveFields() ActiveFields {
	if s == nil {
		return 0
	}
	return s.activeFields
}
