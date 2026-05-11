package test

import (
	"testing"

	"cpa-usage-keeper/internal/quota"
)

func TestDefaultProviderConfigsContainsSevenAPICallTemplates(t *testing.T) {
	configs := quota.DefaultProviderConfigs()
	templates := configs.APICallTemplates()
	if len(templates) != 9 {
		t.Fatalf("expected 9 api-call templates, got %d", len(templates))
	}
	if len(configs.Antigravity) != 3 {
		t.Fatalf("expected 3 antigravity api-call templates, got %d", len(configs.Antigravity))
	}

	if configs.Antigravity[0].Method != "POST" || configs.Antigravity[0].URL != "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels" {
		t.Fatalf("unexpected antigravity config: %+v", configs.Antigravity)
	}
	if configs.Antigravity[1].URL != "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels" || configs.Antigravity[2].URL != "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels" {
		t.Fatalf("unexpected antigravity fallback configs: %+v", configs.Antigravity)
	}
	if configs.Codex.Method != "GET" || configs.Codex.URL != "https://chatgpt.com/backend-api/wham/usage" {
		t.Fatalf("unexpected codex config: %+v", configs.Codex)
	}
	if configs.GeminiCLI.Method != "POST" || configs.GeminiCLI.URL != "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota" {
		t.Fatalf("unexpected gemini cli config: %+v", configs.GeminiCLI)
	}
	if configs.GeminiCLICodeAssist.Method != "POST" || configs.GeminiCLICodeAssist.URL != "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" {
		t.Fatalf("unexpected gemini cli code assist config: %+v", configs.GeminiCLICodeAssist)
	}
	if configs.ClaudeUsage.Method != "GET" || configs.ClaudeUsage.URL != "https://api.anthropic.com/api/oauth/usage" {
		t.Fatalf("unexpected claude usage config: %+v", configs.ClaudeUsage)
	}
	if configs.ClaudeProfile.Method != "GET" || configs.ClaudeProfile.URL != "https://api.anthropic.com/api/oauth/profile" {
		t.Fatalf("unexpected claude profile config: %+v", configs.ClaudeProfile)
	}
	if configs.Kimi.Method != "GET" || configs.Kimi.URL != "https://api.kimi.com/coding/v1/usages" {
		t.Fatalf("unexpected kimi config: %+v", configs.Kimi)
	}

	if configs.Antigravity[0].Headers["Authorization"] != "Bearer $TOKEN$" || configs.Antigravity[0].Headers["Content-Type"] != "application/json" || configs.Antigravity[0].Headers["User-Agent"] != "antigravity/1.11.5 windows/amd64" {
		t.Fatalf("unexpected antigravity headers: %+v", configs.Antigravity[0].Headers)
	}
	if configs.Codex.Headers["Authorization"] != "Bearer $TOKEN$" || configs.Codex.Headers["Content-Type"] != "application/json" || configs.Codex.Headers["User-Agent"] != "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal" {
		t.Fatalf("unexpected codex headers: %+v", configs.Codex.Headers)
	}
	if configs.GeminiCLI.Headers["Authorization"] != "Bearer $TOKEN$" || configs.GeminiCLI.Headers["Content-Type"] != "application/json" {
		t.Fatalf("unexpected gemini cli headers: %+v", configs.GeminiCLI.Headers)
	}
	if configs.GeminiCLICodeAssist.Headers["Authorization"] != "Bearer $TOKEN$" || configs.GeminiCLICodeAssist.Headers["Content-Type"] != "application/json" {
		t.Fatalf("unexpected gemini cli code assist headers: %+v", configs.GeminiCLICodeAssist.Headers)
	}
	if configs.ClaudeUsage.Headers["Authorization"] != "Bearer $TOKEN$" || configs.ClaudeUsage.Headers["Content-Type"] != "application/json" || configs.ClaudeUsage.Headers["anthropic-beta"] != "oauth-2025-04-20" {
		t.Fatalf("unexpected claude usage headers: %+v", configs.ClaudeUsage.Headers)
	}
	if configs.ClaudeProfile.Headers["Authorization"] != "Bearer $TOKEN$" || configs.ClaudeProfile.Headers["Content-Type"] != "application/json" || configs.ClaudeProfile.Headers["anthropic-beta"] != "oauth-2025-04-20" {
		t.Fatalf("unexpected claude profile headers: %+v", configs.ClaudeProfile.Headers)
	}
	if configs.Kimi.Headers["Authorization"] != "Bearer $TOKEN$" {
		t.Fatalf("unexpected kimi headers: %+v", configs.Kimi.Headers)
	}
}
