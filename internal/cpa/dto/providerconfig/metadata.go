package providerconfig

import (
	"encoding/json"
	"fmt"
)

// ProviderMetadataConfig 是各 AI provider 管理接口配置的聚合视图，不是单个 CPA endpoint 的原始响应 DTO。
type ProviderMetadataConfig struct {
	GeminiAPIKeys       []ProviderKeyConfig         `json:"gemini-api-key"`
	ClaudeAPIKeys       []ProviderKeyConfig         `json:"claude-api-key"`
	CodexAPIKeys        []ProviderKeyConfig         `json:"codex-api-key"`
	VertexAPIKeys       []ProviderKeyConfig         `json:"vertex-api-key"`
	OpenAICompatibility []OpenAICompatibilityConfig `json:"openai-compatibility"`
}

// ProviderKeyConfig 是 gemini/claude/codex/vertex API key 配置的兼容归一化视图，支持 CPA 返回的多种 key 命名。
type ProviderKeyConfig struct {
	APIKey    string
	Prefix    string
	Name      string
	BaseURL   string
	AuthIndex string
	Priority  *int
	Disabled  *bool
	Note      *string
}

func (p *ProviderKeyConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode provider key config: %w", err)
	}
	p.APIKey = firstString(raw, "apiKey", "api-key", "key")
	p.Prefix = firstString(raw, "prefix")
	p.Name = firstString(raw, "name")
	p.BaseURL = firstString(raw, "base-url", "base_url", "baseURL")
	p.AuthIndex = firstString(raw, "auth-index", "auth_index", "authIndex")
	p.Priority = firstInt(raw, "priority")
	p.Disabled = firstBool(raw, "disabled")
	p.Note = firstStringPtr(raw, "note")
	return nil
}

// OpenAICompatibilityConfig 是 openai-compatibility provider 配置的兼容归一化视图，不直接等同于 CPA 原始 JSON。
type OpenAICompatibilityConfig struct {
	Name          string
	Prefix        string
	BaseURL       string
	Priority      *int
	Disabled      *bool
	Note          *string
	APIKeyEntries []OpenAIApiKeyEntry
}

func (c *OpenAICompatibilityConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode openai compatibility config: %w", err)
	}
	c.Name = firstString(raw, "name", "id")
	c.Prefix = firstString(raw, "prefix")
	c.BaseURL = firstString(raw, "base-url", "base_url", "baseURL")
	c.Priority = firstInt(raw, "priority")
	c.Disabled = firstBool(raw, "disabled")
	c.Note = firstStringPtr(raw, "note")
	c.APIKeyEntries = nil
	for _, key := range []string{"apiKeyEntries", "api-key-entries", "api-keys"} {
		value, ok := raw[key]
		if !ok {
			continue
		}
		entries, err := decodeOpenAIApiKeyEntries(value)
		if err != nil {
			return err
		}
		c.APIKeyEntries = entries
		break
	}
	return nil
}

// OpenAIApiKeyEntry 是 openai-compatibility 中 api key entry 的兼容归一化视图，支持字符串和对象两种 CPA 返回形态。
type OpenAIApiKeyEntry struct {
	APIKey    string
	AuthIndex string
}

func (e *OpenAIApiKeyEntry) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode openai api key entry: %w", err)
	}
	entry, err := decodeOpenAIApiKeyEntry(raw)
	if err != nil {
		return err
	}
	*e = entry
	return nil
}
