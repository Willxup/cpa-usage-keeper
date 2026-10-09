package test

import (
	"context"
	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/quota"
	"net/http"
	"testing"
	"time"
)

type claudeResetAPIStub struct {
	quotaProviderStub
	authIndex string
}

func TestClaudeResetReadRemainsAdminProtected(t *testing.T) {
	provider := &claudeResetAPIStub{}
	config := AuthConfig{Enabled: true, LoginPassword: "test-password", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, auth.NewSessionManager(time.Hour)), "", OptionalProviders{Quota: provider})
	response := serveAPIGet(router, "/api/v1/quota/claude-reset-grants/claude")
	if response.Code != http.StatusUnauthorized || provider.authIndex != "" {
		t.Fatalf("unauthenticated read reached reset provider: %d %s", response.Code, response.Body.String())
	}
}

func (s *claudeResetAPIStub) GetClaudeResetGrants(_ context.Context, authIndex string) (quota.ClaudeResetGrantsResponse, error) {
	s.authIndex = authIndex
	return quota.ClaudeResetGrantsResponse{AuthIndex: authIndex, SelectedGrantID: "spring", OrganizationID: "11111111-1111-1111-1111-111111111111"}, nil
}
func TestClaudeResetRoutesForwardGrantAndOrganizationWithoutChangingCodex(t *testing.T) {
	provider := &claudeResetAPIStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	response := serveAPIGet(router, "/api/v1/quota/claude-reset-grants/claude")
	if response.Code != http.StatusOK || provider.authIndex != "claude" {
		t.Fatalf("read: %d %s", response.Code, response.Body.String())
	}
	response = serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/reset", `{"auth_index":"claude","grant_id":"spring","organization_id":"11111111-1111-1111-1111-111111111111"}`)
	if response.Code != http.StatusOK || provider.resetRequest.GrantID != "spring" || provider.resetRequest.OrganizationID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("parameters lost: %+v", provider.resetRequest)
	}
	response = serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/reset", `{"auth_index":"codex"}`)
	if response.Code != http.StatusOK || provider.resetRequest.GrantID != "" || provider.resetRequest.OrganizationID != "" {
		t.Fatal("Codex contract changed")
	}
}
