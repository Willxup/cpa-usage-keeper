package cpa

import (
	"encoding/json"
	"fmt"
	"time"
)

type UsageExport struct {
	Version    int                `json:"version"`
	ExportedAt time.Time          `json:"exported_at"`
	Usage      StatisticsSnapshot `json:"usage"`
}

type StatisticsSnapshot struct {
	TotalRequests  int64                  `json:"total_requests"`
	SuccessCount   int64                  `json:"success_count"`
	FailureCount   int64                  `json:"failure_count"`
	TotalTokens    int64                  `json:"total_tokens"`
	APIs           map[string]APISnapshot `json:"apis"`
	RequestsByDay  map[string]int64       `json:"requests_by_day"`
	RequestsByHour map[string]int64       `json:"requests_by_hour"`
	TokensByDay    map[string]int64       `json:"tokens_by_day"`
	TokensByHour   map[string]int64       `json:"tokens_by_hour"`
}

type APISnapshot struct {
	DisplayName   string                   `json:"display_name,omitempty"`
	TotalRequests int64                    `json:"total_requests"`
	SuccessCount  int64                    `json:"success_count"`
	FailureCount  int64                    `json:"failure_count"`
	TotalTokens   int64                    `json:"total_tokens"`
	Models        map[string]ModelSnapshot `json:"models"`
}

type ModelSnapshot struct {
	TotalRequests int64           `json:"total_requests"`
	SuccessCount  int64           `json:"success_count"`
	FailureCount  int64           `json:"failure_count"`
	TotalTokens   int64           `json:"total_tokens"`
	Details       []RequestDetail `json:"details"`
}

type RequestDetail struct {
	Timestamp     time.Time  `json:"timestamp"`
	LatencyMS     int64      `json:"latency_ms"`
	Source        string     `json:"source"`
	SourceRaw     string     `json:"source_raw,omitempty"`
	SourceDisplay string     `json:"source_display,omitempty"`
	SourceType    string     `json:"source_type,omitempty"`
	SourceKey     string     `json:"source_key,omitempty"`
	AuthIndex     string     `json:"auth_index"`
	Failed        bool       `json:"failed"`
	Tokens        TokenStats `json:"tokens"`
}

type TokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type APIKeysResult struct {
	StatusCode int
	Body       []byte
	Payload    APIKeysResponse
}

type APIKeysResponse struct {
	APIKeys []string `json:"api-keys"`
}

type ModelsResult struct {
	StatusCode int
	Body       []byte
	Payload    ModelsResponse
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type AuthFilesResult struct {
	StatusCode int
	Body       []byte
	Payload    AuthFilesResponse
}

type AuthFilesResponse struct {
	Files []AuthFile `json:"files"`
}

type AuthFile struct {
	AuthIndex   string `json:"auth_index"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Type        string `json:"type"`
	Provider    string `json:"provider"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	Disabled    bool   `json:"disabled"`
	Unavailable bool   `json:"unavailable"`
	RuntimeOnly bool   `json:"runtime_only"`
}

type ProviderKeyConfigResult struct {
	StatusCode int
	Body       []byte
	Payload    []ProviderKeyConfig
}

type OpenAICompatibilityResult struct {
	StatusCode int
	Body       []byte
	Payload    []OpenAICompatibilityConfig
}

type ManagementConfig struct {
	APIKeys             []string                    `json:"api-keys"`
	GeminiAPIKeys       []ProviderKeyConfig         `json:"gemini-api-key"`
	ClaudeAPIKeys       []ProviderKeyConfig         `json:"claude-api-key"`
	CodexAPIKeys        []ProviderKeyConfig         `json:"codex-api-key"`
	VertexAPIKeys       []ProviderKeyConfig         `json:"vertex-api-key"`
	OpenAICompatibility []OpenAICompatibilityConfig `json:"openai-compatibility"`
}

type ProviderKeyConfig struct {
	APIKey string
	Prefix string
	Name   string
}

func (p *ProviderKeyConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode provider key config: %w", err)
	}
	p.APIKey = firstString(raw, "apiKey", "api-key", "key")
	p.Prefix = firstString(raw, "prefix")
	p.Name = firstString(raw, "name")
	return nil
}

type OpenAICompatibilityConfig struct {
	Name          string
	Prefix        string
	APIKeyEntries []OpenAIApiKeyEntry
}

func (c *OpenAICompatibilityConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode openai compatibility config: %w", err)
	}
	c.Name = firstString(raw, "name", "id")
	c.Prefix = firstString(raw, "prefix")
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

type OpenAIApiKeyEntry struct {
	APIKey string
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

func decodeOpenAIApiKeyEntries(value any) ([]OpenAIApiKeyEntry, error) {
	rawEntries, ok := value.([]any)
	if !ok {
		return nil, nil
	}
	entries := make([]OpenAIApiKeyEntry, 0, len(rawEntries))
	for _, rawEntry := range rawEntries {
		entry, err := decodeOpenAIApiKeyEntry(rawEntry)
		if err != nil {
			return nil, err
		}
		if entry.APIKey == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func decodeOpenAIApiKeyEntry(raw any) (OpenAIApiKeyEntry, error) {
	switch value := raw.(type) {
	case string:
		return OpenAIApiKeyEntry{APIKey: value}, nil
	case map[string]any:
		return OpenAIApiKeyEntry{APIKey: firstString(value, "apiKey", "api-key", "key")}, nil
	case nil:
		return OpenAIApiKeyEntry{}, nil
	default:
		return OpenAIApiKeyEntry{}, fmt.Errorf("decode openai api key entry: unsupported value %T", raw)
	}
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if text != "" {
			return text
		}
	}
	return ""
}
