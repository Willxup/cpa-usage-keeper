package pricing

import (
	"fmt"
	"strings"
)

// RuleField 是允许参与价格规则匹配的固定 usage 维度。
type RuleField uint8

const (
	RuleFieldInvalid RuleField = iota
	RuleFieldAPIGroupKey
	RuleFieldModel
	RuleFieldAuthIndex
	RuleFieldModelAlias
	RuleFieldServiceTier
	RuleFieldResponseServiceTier
	RuleFieldReasoningEffort
	RuleFieldEndpoint
	RuleFieldExecutorType
	ruleFieldCount
)

var ruleFieldNames = [...]string{
	RuleFieldAPIGroupKey:         "api_group_key",
	RuleFieldModel:               "model",
	RuleFieldAuthIndex:           "auth_index",
	RuleFieldModelAlias:          "model_alias",
	RuleFieldServiceTier:         "service_tier",
	RuleFieldResponseServiceTier: "response_service_tier",
	RuleFieldReasoningEffort:     "reasoning_effort",
	RuleFieldEndpoint:            "endpoint",
	RuleFieldExecutorType:        "executor_type",
}

// ParseRuleField 将用户输入编译为固定枚举，避免任意字符串进入查询构造。
func ParseRuleField(value string) (RuleField, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for field := RuleFieldAPIGroupKey; field < ruleFieldCount; field++ {
		if ruleFieldNames[field] == normalized {
			return field, nil
		}
	}
	return RuleFieldInvalid, fmt.Errorf("unknown pricing rule field %q", normalized)
}

func (f RuleField) String() string {
	if f <= RuleFieldInvalid || f >= ruleFieldCount {
		return ""
	}
	return ruleFieldNames[f]
}

// ActiveFields 是编译后的字段位图，仅包含确实会改变价格的规则字段。
type ActiveFields uint16

func (f ActiveFields) Has(field RuleField) bool {
	if field <= RuleFieldInvalid || field >= ruleFieldCount {
		return false
	}
	return f&(1<<uint(field-1)) != 0
}

func (f ActiveFields) Len() int {
	count := 0
	for field := RuleFieldAPIGroupKey; field < ruleFieldCount; field++ {
		if f.Has(field) {
			count++
		}
	}
	return count
}

func (f ActiveFields) with(field RuleField) ActiveFields {
	if field <= RuleFieldInvalid || field >= ruleFieldCount {
		return f
	}
	return f | (1 << uint(field-1))
}

// UsageDimensions 是所有计价来源共享的固定字段映射，不使用反射或动态 map。
type UsageDimensions struct {
	APIGroupKey         string
	Model               string
	AuthIndex           string
	ModelAlias          string
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
}

func (d UsageDimensions) Value(field RuleField) string {
	switch field {
	case RuleFieldAPIGroupKey:
		return d.APIGroupKey
	case RuleFieldModel:
		return d.Model
	case RuleFieldAuthIndex:
		return d.AuthIndex
	case RuleFieldModelAlias:
		return d.ModelAlias
	case RuleFieldServiceTier:
		return d.ServiceTier
	case RuleFieldResponseServiceTier:
		return d.ResponseServiceTier
	case RuleFieldReasoningEffort:
		return d.ReasoningEffort
	case RuleFieldEndpoint:
		return d.Endpoint
	case RuleFieldExecutorType:
		return d.ExecutorType
	default:
		return ""
	}
}

func canonicalizeUsageDimensions(dimensions UsageDimensions) UsageDimensions {
	dimensions.APIGroupKey = canonicalRequiredDimension(dimensions.APIGroupKey)
	dimensions.Model = canonicalRequiredDimension(dimensions.Model)
	dimensions.AuthIndex = strings.TrimSpace(dimensions.AuthIndex)
	dimensions.ModelAlias = strings.TrimSpace(dimensions.ModelAlias)
	dimensions.ServiceTier = strings.TrimSpace(dimensions.ServiceTier)
	dimensions.ResponseServiceTier = strings.TrimSpace(dimensions.ResponseServiceTier)
	dimensions.ReasoningEffort = strings.TrimSpace(dimensions.ReasoningEffort)
	dimensions.Endpoint = strings.TrimSpace(dimensions.Endpoint)
	dimensions.ExecutorType = strings.TrimSpace(dimensions.ExecutorType)
	return dimensions
}

func canonicalRequiredDimension(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
