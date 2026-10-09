package test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestNormalizeCodexSubscription(t *testing.T) {
	tests := []struct {
		name string
		plan string
		want string
	}{
		{name: "free", plan: "free", want: "free"},
		{name: "plus", plan: "Plus", want: "plus"},
		{name: "team", plan: "team", want: "team"},
		{name: "pro", plan: " PRO ", want: "pro-20x"},
		{name: "prolite", plan: "prolite", want: "pro-5x"},
		{name: "pro-dash-lite", plan: "pro-lite", want: "pro-5x"},
		{name: "pro-underscore-lite", plan: "pro_lite", want: "pro-5x"},
		{name: "enterprise", plan: "enterprise", want: "enterprise"},
		{name: "unknown", plan: " ChatGPT-Pro-Monthly ", want: "ChatGPT-Pro-Monthly"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := quota.NormalizeSubscription(quota.ProviderOutput{
				Provider: " CoDeX ",
				Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{
					PlanType: test.plan,
				}},
			})
			if got == nil || got.Provider != "codex" || got.Plan != test.want || got.TierID != "" || got.TierName != "" {
				t.Fatalf("NormalizeSubscription() = %#v, want provider=codex plan=%s", got, test.want)
			}
		})
	}
}

func TestNormalizeClaudeSubscription(t *testing.T) {
	tests := []struct {
		name    string
		profile *quota.ClaudeProfileResponse
		want    string
	}{
		{
			name: "max wins over every lower tier",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(true), HasClaudePro: new(true)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_team", SubscriptionStatus: "active"},
			},
			want: "max",
		},
		{
			name: "max 5x rate limit tier",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(true)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_max", RateLimitTier: "default_claude_max_5x"},
			},
			want: "max-5x",
		},
		{
			name: "max 20x rate limit tier",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(true)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_max", RateLimitTier: "default_claude_max_20x"},
			},
			want: "max-20x",
		},
		{
			name: "max without rate limit tier keeps plain max",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(true)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_max"},
			},
			want: "max",
		},
		{
			name: "pro wins over team",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(false), HasClaudePro: new(true)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_team", SubscriptionStatus: "active"},
			},
			want: "pro",
		},
		{
			name: "active team wins over free",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(false), HasClaudePro: new(false)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: " CLAUDE_TEAM ", SubscriptionStatus: " ACTIVE "},
			},
			want: "team",
		},
		{
			name: "free requires both explicit false values",
			profile: &quota.ClaudeProfileResponse{
				Account: &quota.ClaudeProfileAccount{HasClaudeMax: new(false), HasClaudePro: new(false)},
			},
			want: "free",
		},
		{
			name: "explicit free organization remains free",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(false), HasClaudePro: new(false)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_free"},
			},
			want: "free",
		},
		{
			name: "enterprise organization is not free",
			profile: &quota.ClaudeProfileResponse{
				Account:      &quota.ClaudeProfileAccount{HasClaudeMax: new(false), HasClaudePro: new(false)},
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_enterprise"},
			},
		},
		{
			name: "team does not require account flags",
			profile: &quota.ClaudeProfileResponse{
				Organization: &quota.ClaudeProfileOrganization{OrganizationType: "claude_team", SubscriptionStatus: "active"},
			},
			want: "team",
		},
		{
			name: "one missing flag is not free",
			profile: &quota.ClaudeProfileResponse{
				Account: &quota.ClaudeProfileAccount{HasClaudePro: new(false)},
			},
		},
		{name: "missing profile"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := quota.NormalizeSubscription(quota.ProviderOutput{
				Provider: " Claude ",
				Result:   quota.ClaudeResult{Usage: &quota.ClaudeUsagePayload{}, Profile: test.profile},
			})
			if test.want == "" {
				if got != nil {
					t.Fatalf("NormalizeSubscription() = %#v, want nil", got)
				}
				return
			}
			if got == nil || got.Provider != "claude" || got.Plan != test.want || got.TierID != "" || got.TierName != "" {
				t.Fatalf("NormalizeSubscription() = %#v, want provider=claude plan=%s", got, test.want)
			}
		})
	}
}

func TestNormalizeAntigravitySubscription(t *testing.T) {
	tests := []struct {
		name         string
		subscription *quota.AntigravitySubscriptionPayload
		wantPlan     string
		wantTierID   string
		wantTierName string
	}{
		{
			name: "paid tier wins over current tier",
			subscription: &quota.AntigravitySubscriptionPayload{
				CurrentTier: &quota.GeminiCliUserTier{ID: "free-tier", Name: "Free"},
				PaidTier:    &quota.GeminiCliUserTier{ID: "g1-ultra-tier", Name: "Ultra"},
			},
			wantPlan: "ultra", wantTierID: "g1-ultra-tier", wantTierName: "Ultra",
		},
		{
			name: "paid tier without id falls back to current tier",
			subscription: &quota.AntigravitySubscriptionPayload{
				CurrentTier: &quota.GeminiCliUserTier{ID: "g1-pro-tier", Name: "Pro"},
				PaidTier:    &quota.GeminiCliUserTier{Name: "Paid preview"},
			},
			wantPlan: "pro", wantTierID: "g1-pro-tier", wantTierName: "Pro",
		},
		{name: "free", subscription: &quota.AntigravitySubscriptionPayload{CurrentTier: &quota.GeminiCliUserTier{ID: "free-tier", Name: "Free"}}, wantPlan: "free", wantTierID: "free-tier", wantTierName: "Free"},
		{name: "ultra lite", subscription: &quota.AntigravitySubscriptionPayload{CurrentTier: &quota.GeminiCliUserTier{ID: "g1-ultra-lite-tier", Name: "Ultra Lite"}}, wantPlan: "ultra-lite", wantTierID: "g1-ultra-lite-tier", wantTierName: "Ultra Lite"},
		{name: "unknown id", subscription: &quota.AntigravitySubscriptionPayload{CurrentTier: &quota.GeminiCliUserTier{ID: "future-tier", Name: "Future"}}, wantPlan: "unknown", wantTierID: "future-tier", wantTierName: "Future"},
		{name: "name only", subscription: &quota.AntigravitySubscriptionPayload{CurrentTier: &quota.GeminiCliUserTier{Name: "Preview"}}, wantPlan: "unknown", wantTierName: "Preview"},
		{name: "missing tiers", subscription: &quota.AntigravitySubscriptionPayload{}},
		{name: "missing subscription"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := quota.NormalizeSubscription(quota.ProviderOutput{
				Provider: " Antigravity ",
				Result:   quota.AntigravityResult{Subscription: test.subscription},
			})
			if test.wantPlan == "" {
				if got != nil {
					t.Fatalf("NormalizeSubscription() = %#v, want nil", got)
				}
				return
			}
			if got == nil || got.Provider != "antigravity" || got.Plan != test.wantPlan || got.TierID != test.wantTierID || got.TierName != test.wantTierName {
				t.Fatalf("NormalizeSubscription() = %#v, want plan=%q tierId=%q tierName=%q", got, test.wantPlan, test.wantTierID, test.wantTierName)
			}
		})
	}
}

func TestNormalizeSubscriptionRejectsMissingOrUnregisteredValues(t *testing.T) {
	for _, output := range []quota.ProviderOutput{
		{},
		{Provider: "codex", Result: quota.CodexResult{}},
		{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{PlanType: "   "}}},
		{Provider: "claude", Result: quota.ClaudeResult{}},
		{Provider: "gemini-cli", Result: quota.GeminiCLIResult{}},
		{Provider: "xai", Result: quota.XAIResult{}},
	} {
		if got := quota.NormalizeSubscription(output); got != nil {
			t.Fatalf("NormalizeSubscription(%#v) = %#v, want nil", output, got)
		}
	}
}

func TestResolveIdentitySubscriptionOnlyPublishesCodexMetadata(t *testing.T) {
	pro := " pro "
	got := quota.ResolveIdentitySubscription(entities.UsageIdentity{
		AuthType: entities.UsageIdentityAuthTypeAuthFile,
		Type:     "codex",
		Provider: "Codex",
		PlanType: &pro,
	})
	if got == nil || got.Provider != "codex" || got.Plan != "pro-20x" {
		t.Fatalf("ResolveIdentitySubscription() = %#v, want codex pro-20x", got)
	}

	for _, identity := range []entities.UsageIdentity{
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, Type: "claude", Provider: "Claude", PlanType: &pro},
		{AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "codex", Provider: "Codex", PlanType: &pro},
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, Type: "codex", Provider: "Codex"},
	} {
		if got := quota.ResolveIdentitySubscription(identity); got != nil {
			t.Fatalf("ResolveIdentitySubscription(%#v) = %#v, want nil", identity, got)
		}
	}
}

func TestNormalizeSubscriptionSupportsPointerResults(t *testing.T) {
	for _, tc := range []struct {
		provider, plan string
		result         any
	}{
		{"codex", "plus", &quota.CodexResult{Usage: &quota.CodexUsagePayload{PlanType: "plus"}}},
		{"claude", "max", &quota.ClaudeResult{Profile: &quota.ClaudeProfileResponse{Account: &quota.ClaudeProfileAccount{HasClaudeMax: new(true)}}}},
		{"antigravity", "ultra", &quota.AntigravityResult{Subscription: &quota.AntigravitySubscriptionPayload{CurrentTier: &quota.GeminiCliUserTier{ID: "g1-ultra-tier", Name: "Ultra"}}}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			got := quota.NormalizeSubscription(quota.ProviderOutput{Provider: tc.provider, Result: tc.result})
			if got == nil || got.Provider != tc.provider || got.Plan != tc.plan {
				t.Fatalf("unexpected pointer result subscription: %#v", got)
			}
		})
	}
}
