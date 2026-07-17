package relayusage

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
)

func TestMatchByBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"glm", "https://open.bigmodel.cn/api/paas/v4", "glm"},
		{"minimax CN", "https://api.minimaxi.com/v1", "minimax"},
		{"minimax global", "https://www.minimax.io/v1", "minimax"},
		{"kimi", "https://api.kimi.com/v1", "kimi"},
		{"moonshot", "https://api.moonshot.cn/v1", "kimi"},
		{"deepseek", "https://api.deepseek.com", "deepseek"},
		{"unknown relay", "https://openrouter.ai/api/v1", ""},
		{"empty", "", ""},
		{"with port", "https://open.bigmodel.cn:443/api", "glm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MatchByBaseURL(c.baseURL); got != c.want {
				t.Errorf("MatchByBaseURL(%q) = %q, want %q", c.baseURL, got, c.want)
			}
		})
	}
}

func TestMatchOverrideTakesPrecedence(t *testing.T) {
	identity := entities.UsageIdentity{ID: 7, BaseURL: "https://open.bigmodel.cn/api/paas/v4"}

	if got := Match(identity, nil); got != "glm" {
		t.Errorf("Match without override = %q, want glm", got)
	}

	overrides := map[string]string{"7": "deepseek"}
	if got := Match(identity, overrides); got != "deepseek" {
		t.Errorf("Match with override = %q, want deepseek", got)
	}

	overrides["7"] = "none"
	if got := Match(identity, overrides); got != "" {
		t.Errorf("Match with none override = %q, want empty", got)
	}

	// override 只影响指定 identity，不影响其他条目按域名匹配。
	other := entities.UsageIdentity{ID: 8, BaseURL: "https://api.deepseek.com"}
	if got := Match(other, overrides); got != "deepseek" {
		t.Errorf("Match other identity = %q, want deepseek", got)
	}
}
