package test

import (
	"testing"

	"cpa-usage-keeper/internal/quota"
)

func TestDefaultProviderConfigsContainsAPICallTemplates(t *testing.T) {
	configs := quota.DefaultProviderConfigs()
	templates := configs.APICallTemplates()
	if len(templates) != 14 {
		t.Fatalf("expected 14 api-call templates, got %d", len(templates))
	}
	if len(configs.Antigravity) != 3 {
		t.Fatalf("expected 3 antigravity api-call templates, got %d", len(configs.Antigravity))
	}

	if len(configs.AntigravitySubscriptions) != 2 {
		t.Fatalf("expected 2 antigravity subscription templates, got %d", len(configs.AntigravitySubscriptions))
	}
	for _, tc := range []struct {
		config      quota.APICallConfig
		method, url string
	}{
		{configs.Antigravity[0], "POST", "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"},
		{configs.Antigravity[1], "POST", "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary"},
		{configs.Antigravity[2], "POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"},
		{configs.AntigravitySubscriptions[0], "POST", "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"},
		{configs.AntigravitySubscriptions[1], "POST", "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"},
		{configs.Codex, "GET", "https://chatgpt.com/backend-api/wham/usage"},
		{configs.GeminiCLI, "POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"},
		{configs.GeminiCLICodeAssist, "POST", "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"},
		{configs.ClaudeUsage, "GET", "https://api.anthropic.com/api/oauth/usage?cedar_ember=1&skip_spend=1"},
		{configs.ClaudeProfile, "GET", "https://api.anthropic.com/api/oauth/profile"},
		{configs.Kimi, "GET", "https://api.kimi.com/coding/v1/usages"},
		{configs.KimiAI, "GET", "https://api.kimi.ai/coding/v1/usages"},
		{configs.XAIWeekly, "GET", "https://cli-chat-proxy.grok.com/v1/billing?format=credits"},
		{configs.XAIMonthly, "GET", "https://cli-chat-proxy.grok.com/v1/billing"},
	} {
		if tc.config.Method != tc.method || tc.config.URL != tc.url {
			t.Fatalf("unexpected API template for %s: %+v", tc.url, tc.config)
		}
		if tc.config.Headers["Authorization"] != "Bearer $TOKEN$" {
			t.Fatalf("missing bearer template for %s", tc.url)
		}
	}

	for _, config := range append(configs.Antigravity, configs.AntigravitySubscriptions...) {
		if config.Headers["Content-Type"] != "application/json" || config.Headers["User-Agent"] != "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)" {
			t.Fatalf("unexpected antigravity headers: %+v", config.Headers)
		}
	}
	for _, config := range []quota.APICallConfig{configs.Codex, configs.GeminiCLI, configs.GeminiCLICodeAssist, configs.ClaudeUsage, configs.ClaudeProfile} {
		if config.Headers["Content-Type"] != "application/json" {
			t.Fatalf("missing JSON content type: %+v", config)
		}
	}
	if configs.Codex.Headers["User-Agent"] != "Codex Desktop/0.160.1 (Mac OS 27.0.1; arm64) unknown (Codex Desktop; 26.930.61225)" {
		t.Fatalf("unexpected codex headers: %+v", configs.Codex.Headers)
	}
	for _, config := range []quota.APICallConfig{configs.ClaudeUsage, configs.ClaudeProfile} {
		if config.Headers["anthropic-beta"] != "oauth-2025-04-20" {
			t.Fatalf("unexpected claude headers: %+v", config.Headers)
		}
	}
	for _, config := range []quota.APICallConfig{configs.XAIWeekly, configs.XAIMonthly} {
		if config.Headers["x-xai-token-auth"] != "xai-grok-cli" ||
			config.Headers["x-grok-client-version"] != "0.2.93" ||
			config.Headers["Accept"] != "*/*" ||
			config.Headers["User-Agent"] != "grok-pager/0.2.93 grok-shell/0.2.93 (macos; aarch64)" {
			t.Fatalf("unexpected xai headers: %+v", config.Headers)
		}
		if _, ok := config.Headers["x-userid"]; ok {
			t.Fatalf("x-userid must not be fabricated without a reliable subject: %+v", config.Headers)
		}
	}
}
