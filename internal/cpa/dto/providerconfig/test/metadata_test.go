package providerconfig_test

import (
	"encoding/json"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

// TestProviderKeyConfigDecodesSupportedFieldAliases 锁定标准 API Key endpoint 的兼容字段来源。
func TestProviderKeyConfigDecodesSupportedFieldAliases(t *testing.T) {
	// cases 覆盖当前 client 接受的 key、base URL 和 auth-index 命名组合。
	cases := []struct {
		// name 标识当前字段别名组合。
		name string
		// body 是直接传给 endpoint DTO 的单条 JSON。
		body string
	}{
		// kebab-case 是当前 CPA management response 的主格式。
		{name: "kebab-case", body: `{"api-key":"provider-key","prefix":"team","name":"Provider","base-url":"https://provider.example/v1","auth-index":"provider-auth","priority":8,"disabled":false,"note":"primary"}`},
		// snake-case 保留旧响应兼容。
		{name: "snake-case", body: `{"apiKey":"provider-key","prefix":"team","name":"Provider","base_url":"https://provider.example/v1","auth_index":"provider-auth","priority":8,"disabled":false,"note":"primary"}`},
		// camel-case 保留 client 既有别名兼容。
		{name: "camel-case", body: `{"key":"provider-key","prefix":"team","name":"Provider","baseURL":"https://provider.example/v1","authIndex":"provider-auth","priority":8,"disabled":false,"note":"primary"}`},
	}

	// 每种字段别名都必须得到相同归一化 DTO。
	for _, tc := range cases {
		// 复制循环变量，避免子测试闭包共享内容。
		tc := tc
		// 为当前字段组合运行独立断言。
		t.Run(tc.name, func(t *testing.T) {
			// cfg 接收 DTO 自定义 UnmarshalJSON 的结果。
			var cfg providerconfig.ProviderKeyConfig
			// 使用标准 json 包触发生产解码逻辑。
			if err := json.Unmarshal([]byte(tc.body), &cfg); err != nil {
				t.Fatalf("unmarshal provider key config: %v", err)
			}
			// 标准字段必须全部落入各自目标字段。
			if cfg.APIKey != "provider-key" || cfg.Prefix != "team" || cfg.Name != "Provider" || cfg.BaseURL != "https://provider.example/v1" || cfg.AuthIndex != "provider-auth" {
				t.Fatalf("provider key fields = %+v", cfg)
			}
			// 可选同步字段必须保留指针语义和值。
			if cfg.Priority == nil || *cfg.Priority != 8 || cfg.Disabled == nil || *cfg.Disabled || cfg.Note == nil || *cfg.Note != "primary" {
				t.Fatalf("provider sync fields = %+v", cfg)
			}
		})
	}
}

// TestOpenAICompatibilityConfigDecodesProviderAndEntryFields 锁定 provider 层字段与多 key entry 的兼容组合。
func TestOpenAICompatibilityConfigDecodesProviderAndEntryFields(t *testing.T) {
	// body 同时使用 legacy id/key 与 snake auth_index，验证旧 CPA 响应仍可归一化。
	body := `{"id":"OpenRouter","prefix":"openrouter","base-url":"https://openrouter.ai/api/v1","priority":4,"disabled":true,"note":"shared","api-key-entries":[{"key":"first-key","auth_index":"first-auth"},{"api-key":"second-key","auth-index":"second-auth"}]}`
	// cfg 接收 OpenAI Compatibility 专属 DTO。
	var cfg providerconfig.OpenAICompatibilityConfig
	// 触发生产自定义 JSON 解码。
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("unmarshal openai compatibility config: %v", err)
	}
	// provider 层展示与连接字段必须完整保留。
	if cfg.Name != "OpenRouter" || cfg.Prefix != "openrouter" || cfg.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("openai provider fields = %+v", cfg)
	}
	// provider 层同步字段必须保留指针语义和值。
	if cfg.Priority == nil || *cfg.Priority != 4 || cfg.Disabled == nil || !*cfg.Disabled || cfg.Note == nil || *cfg.Note != "shared" {
		t.Fatalf("openai sync fields = %+v", cfg)
	}
	// 两个 key entry 必须保持各自 API Key 与 auth-index 配对。
	if len(cfg.APIKeyEntries) != 2 || cfg.APIKeyEntries[0].APIKey != "first-key" || cfg.APIKeyEntries[0].AuthIndex != "first-auth" || cfg.APIKeyEntries[1].APIKey != "second-key" || cfg.APIKeyEntries[1].AuthIndex != "second-auth" {
		t.Fatalf("openai key entries = %+v", cfg.APIKeyEntries)
	}
}
