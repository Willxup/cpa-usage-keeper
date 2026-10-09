package pricingmetadata

import (
	"encoding/json"
	"slices"
	"strings"
)

type liteLLMModel struct {
	Provider         string   `json:"litellm_provider"`
	Mode             string   `json:"mode"`
	OutputModalities []string `json:"supported_output_modalities"`
	Input            *float64 `json:"input_cost_per_token"`
	Output           *float64 `json:"output_cost_per_token"`
	CacheRead        *float64 `json:"cache_read_input_token_cost"`
	CacheHit         *float64 `json:"input_cost_per_token_cache_hit"`
	CacheWrite       *float64 `json:"cache_creation_input_token_cost"`
}

func decodeLiteLLM(decoder *json.Decoder) ([]Entry, error) {
	var models map[string]liteLLMModel
	if err := decoder.Decode(&models); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(models))
	for _, id := range sortedKeys(models) {
		model := models[id]
		// 只映射基础文本 token 价格；图片、音频、请求数和上下文阶梯有独立计价语义。
		if model.Mode != "chat" && model.Mode != "completion" && model.Mode != "responses" {
			continue
		}
		// chat 也包含纯音频模型；只排除明确没有文本输出的条目，缺少字段时保持兼容。
		if model.OutputModalities != nil && !slices.Contains(model.OutputModalities, "text") {
			continue
		}
		if strings.TrimSpace(id) == "" || model.Input == nil || model.Output == nil {
			continue
		}
		provider := liteLLMProviderID(model.Provider)
		if provider == "" {
			continue
		}
		// 部分目录条目仅提供 cache_hit；标准字段存在时保留其值，包括显式零价。
		cacheRead := model.CacheRead
		if cacheRead == nil {
			cacheRead = model.CacheHit
		}
		entries = append(entries, Entry{ProviderID: provider, ProviderName: provider, Model: Model{
			ID: id, Name: id,
			Cost: Cost{Input: perMillion(model.Input), Output: perMillion(model.Output), CacheRead: perMillion(cacheRead), CacheWrite: perMillion(model.CacheWrite)},
		}})
	}
	return entries, nil
}

func perMillion(price *float64) *float64 {
	if price == nil {
		return nil
	}
	value := *price * 1_000_000
	return &value
}

func liteLLMProviderID(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	// 归一化供应商 ID 后复用既有优先级，不从 CPA 可自定义的模型前缀猜供应商。
	switch provider {
	case "text-completion-openai":
		return "openai"
	case "gemini":
		return "google"
	case "vertex_ai", "vertex_ai-language-models":
		return "google-vertex"
	case "vertex_ai-anthropic_models":
		return "google-vertex-anthropic"
	case "bedrock", "bedrock_converse":
		return "amazon-bedrock"
	case "x_ai":
		return "xai"
	case "moonshot":
		return "moonshotai"
	case "dashscope", "qwen_ai_platform":
		return "alibaba-cn"
	case "qwencloud":
		return "alibaba"
	case "cohere_chat":
		return "cohere"
	default:
		return provider
	}
}
