package quota

type APICallConfig struct {
	Method  string
	URL     string
	Headers map[string]string
}

type ProviderConfigs struct {
	Antigravity         []APICallConfig
	Codex               APICallConfig
	GeminiCLI           APICallConfig
	GeminiCLICodeAssist APICallConfig
	ClaudeUsage         APICallConfig
	ClaudeProfile       APICallConfig
	Kimi                APICallConfig
}

func DefaultProviderConfigs() ProviderConfigs {
	return ProviderConfigs{
		Antigravity: []APICallConfig{
			{
				Method: "POST",
				URL:    "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
				Headers: map[string]string{
					"Authorization": "Bearer $TOKEN$",
					"Content-Type":  "application/json",
					"User-Agent":    "antigravity/1.11.5 windows/amd64",
				},
			},
			{
				Method: "POST",
				URL:    "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels",
				Headers: map[string]string{
					"Authorization": "Bearer $TOKEN$",
					"Content-Type":  "application/json",
					"User-Agent":    "antigravity/1.11.5 windows/amd64",
				},
			},
			{
				Method: "POST",
				URL:    "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
				Headers: map[string]string{
					"Authorization": "Bearer $TOKEN$",
					"Content-Type":  "application/json",
					"User-Agent":    "antigravity/1.11.5 windows/amd64",
				},
			},
		},
		Codex: APICallConfig{
			Method: "GET",
			URL:    "https://chatgpt.com/backend-api/wham/usage",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
				"User-Agent":    "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
			},
		},
		GeminiCLI: APICallConfig{
			Method: "POST",
			URL:    "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
			},
		},
		GeminiCLICodeAssist: APICallConfig{
			Method: "POST",
			URL:    "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
			},
		},
		ClaudeUsage: APICallConfig{
			Method: "GET",
			URL:    "https://api.anthropic.com/api/oauth/usage",
			Headers: map[string]string{
				"Authorization":  "Bearer $TOKEN$",
				"Content-Type":   "application/json",
				"anthropic-beta": "oauth-2025-04-20",
			},
		},
		ClaudeProfile: APICallConfig{
			Method: "GET",
			URL:    "https://api.anthropic.com/api/oauth/profile",
			Headers: map[string]string{
				"Authorization":  "Bearer $TOKEN$",
				"Content-Type":   "application/json",
				"anthropic-beta": "oauth-2025-04-20",
			},
		},
		Kimi: APICallConfig{
			Method: "GET",
			URL:    "https://api.kimi.com/coding/v1/usages",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
			},
		},
	}
}

func (c ProviderConfigs) APICallTemplates() []APICallConfig {
	templates := make([]APICallConfig, 0, len(c.Antigravity)+6)
	templates = append(templates, c.Antigravity...)
	templates = append(templates,
		c.Codex,
		c.GeminiCLI,
		c.GeminiCLICodeAssist,
		c.ClaudeUsage,
		c.ClaudeProfile,
		c.Kimi,
	)
	return templates
}
