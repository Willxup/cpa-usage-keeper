package service

import (
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
)

func TestAuthFileUsageIdentityUsesCodexProviderWhenTypeIsMissing(t *testing.T) {
	accountID := "team-account-from-provider"
	planType := "team"
	identity := authFileUsageIdentity(authfiles.AuthFile{
		AuthIndex: "codex-provider-only-auth",
		Email:     "same-user@example.invalid",
		Provider:  "codex-agent-identity",
		IDToken: &authfiles.AuthFileIDToken{
			AccountID: &accountID,
			PlanType:  &planType,
		},
	})
	if identity.AccountID == nil || *identity.AccountID != accountID || identity.PlanType == nil || *identity.PlanType != planType {
		t.Fatalf("provider-only Codex auth file was not extended: %+v", identity)
	}
}
