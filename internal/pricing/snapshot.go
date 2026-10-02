package pricing

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
)

// RuleConfig 是持久化规则的领域表示。CompileSnapshot 会统一规范化 Key 和 Value。
type RuleConfig struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Multiplier float64 `json:"multiplier"`
}

// UnmarshalJSON 保留显式零倍率，并拒绝遗漏倍率的条件规则。
func (r *RuleConfig) UnmarshalJSON(data []byte) error {
	var wire struct {
		Key        *string  `json:"key"`
		Value      *string  `json:"value"`
		Multiplier *float64 `json:"multiplier"`
	}
	if err := decodePricingObject(data, &wire); err != nil {
		return err
	}
	if wire.Key == nil || wire.Value == nil || wire.Multiplier == nil {
		switch {
		case wire.Key == nil:
			return invalidPricingField("key", "required", "conditional multiplier key is required")
		case wire.Value == nil:
			return invalidPricingField("value", "required", "conditional multiplier value is required")
		default:
			return invalidPricingField("multiplier", "required", "conditional multiplier value is required")
		}
	}
	*r = RuleConfig{Key: *wire.Key, Value: *wire.Value, Multiplier: *wire.Multiplier}
	return nil
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
	pricing  entities.ModelPriceSetting
	rules    []compiledRule
	branches []compiledBranch
}

// Snapshot 是编译后只读的完整价格目录。内部集合在发布后不再修改。
type Snapshot struct {
	modelsByName   map[string]compiledModel
	modelConfigs   []ModelConfig
	pricingConfigs []ModelPricingConfig
	location       *time.Location
}

// CompileSnapshot 规范化并校验完整价格集合，只有整个候选集合安全时才返回快照。
func CompileSnapshot(configs []ModelConfig) (*Snapshot, error) {
	snapshot := &Snapshot{
		modelsByName: make(map[string]compiledModel, len(configs)),
		modelConfigs: make([]ModelConfig, 0, len(configs)),
	}
	for index := range configs {
		compiled, normalized, err := compileModelConfig(configs[index])
		if err != nil {
			return nil, fmt.Errorf("compile model price at index %d: %w", index, err)
		}
		model := normalized.Pricing.Model
		if _, exists := snapshot.modelsByName[model]; exists {
			return nil, fmt.Errorf("duplicate model price %q", model)
		}
		snapshot.modelsByName[model] = compiled
		snapshot.modelConfigs = append(snapshot.modelConfigs, normalized)
	}
	sort.Slice(snapshot.modelConfigs, func(i, j int) bool {
		return snapshot.modelConfigs[i].Pricing.Model < snapshot.modelConfigs[j].Pricing.Model
	})
	return snapshot, nil
}

// compileModelConfig 校验并冻结单个模型价格和规则，保留规范化配置供显式读取与写回。
func compileModelConfig(config ModelConfig) (compiledModel, ModelConfig, error) {
	pricing := cloneModelPriceSetting(config.Pricing)
	pricing.Model = strings.TrimSpace(pricing.Model)
	if pricing.Model == "" {
		return compiledModel{}, ModelConfig{}, invalidPricingField("model", "required", "model is required")
	}
	if err := validatePricingNumbers(pricing); err != nil {
		return compiledModel{}, ModelConfig{}, err
	}

	normalizedRules := make([]RuleConfig, 0, len(config.Rules))
	compiledRules := make([]compiledRule, 0, len(config.Rules))
	seen := make(map[ruleIdentity]struct{}, len(config.Rules))
	for index := range config.Rules {
		rule, compiledRuleValue, err := compileRule(config.Rules[index])
		if err != nil {
			return compiledModel{}, ModelConfig{}, prefixPricingValidationError(fmt.Sprintf("conditional_multipliers[%d]", index), err)
		}
		identity := ruleIdentity{key: rule.Key, value: rule.Value}
		if _, exists := seen[identity]; exists {
			return compiledModel{}, ModelConfig{}, invalidPricingField(fmt.Sprintf("conditional_multipliers[%d].key", index), "invalid", "duplicate rule key and value")
		}
		seen[identity] = struct{}{}
		normalizedRules = append(normalizedRules, rule)
		if rule.Multiplier == 1 {
			continue
		}
		compiledRules = append(compiledRules, compiledRuleValue)
	}

	if err := validateWorstCaseCost(pricing, compiledRules); err != nil {
		return compiledModel{}, ModelConfig{}, err
	}
	normalized := ModelConfig{Pricing: cloneModelPriceSetting(pricing), Rules: cloneRules(normalizedRules)}
	return compiledModel{pricing: pricing, rules: compiledRules}, normalized, nil
}

type ruleIdentity struct {
	key   string
	value string
}

func compileRule(input RuleConfig) (RuleConfig, compiledRule, error) {
	field, err := ParseRuleField(input.Key)
	if err != nil {
		return RuleConfig{}, compiledRule{}, invalidPricingField("key", "invalid", err.Error())
	}
	value := strings.TrimSpace(input.Value)
	if value == "" {
		return RuleConfig{}, compiledRule{}, invalidPricingField("value", "required", "rule value is required")
	}
	if !isNonNegativeFinite(input.Multiplier) {
		return RuleConfig{}, compiledRule{}, invalidPricingField("multiplier", "invalid", "rule multiplier must be a finite non-negative number")
	}
	normalized := RuleConfig{Key: field.String(), Value: value, Multiplier: input.Multiplier}
	return normalized, compiledRule{field: field, value: value, multiplier: input.Multiplier}, nil
}

func validatePricingNumbers(pricing entities.ModelPriceSetting) error {
	for _, field := range []struct {
		path, name string
		value      float64
	}{
		{"base_prices.input", "prompt price", pricing.PromptPricePer1M},
		{"base_prices.output", "completion price", pricing.CompletionPricePer1M},
		{"base_prices.cache_read", "cache read price", pricing.CacheReadPricePer1M},
		{"base_prices.cache_write", "cache write price", pricing.CacheWritePricePer1M},
	} {
		if !isNonNegativeFinite(field.value) {
			return invalidPricingField(field.path, "invalid", field.name+" must be a finite non-negative number")
		}
	}
	multiplier := 1.0
	if pricing.PriceMultiplier != nil {
		multiplier = *pricing.PriceMultiplier
	}
	if !isNonNegativeFinite(multiplier) {
		return invalidPricingField("model_multiplier", "invalid", "model price multiplier must be a finite non-negative number")
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
			return invalidPricingField("conditional_multipliers", "invalid", "combined pricing rule multiplier is not finite")
		}
	}
	totalMultiplier, ok := safeMultiply(modelMultiplier, maxRuleMultiplier)
	if !ok {
		return invalidPricingField("model_multiplier", "invalid", "combined model and rule multiplier is not finite")
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
			return invalidPricingField("base_prices", "invalid", "unscaled worst-case token cost is not finite")
		}
		unscaledTotal += segment

		segment, segmentOK = safeMultiply(segment, totalMultiplier)
		if !segmentOK || scaledTotal > math.MaxFloat64-segment {
			return invalidPricingField("base_prices", "invalid", "worst-case token cost is not finite")
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
	result := make([]RuleConfig, len(input))
	copy(result, input)
	return result
}

// PricingStyleForModel 只从本次固定的当前配置读取展示风格，优先真实 model，缺配置才回退 alias；不计算费用。
func (s *Snapshot) PricingStyleForModel(model, alias string) string {
	if s == nil {
		return ""
	}
	if config, ok := s.modelsByName[canonicalRequiredDimension(model)]; ok {
		return config.pricing.PricingStyle
	}
	if config, ok := s.modelsByName[strings.TrimSpace(alias)]; ok {
		return config.pricing.PricingStyle
	}
	return ""
}
