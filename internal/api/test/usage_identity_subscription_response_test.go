package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestUsageIdentityResponsePublishesSubscriptionAndRemovesPlanType(t *testing.T) {
	db := openProviderMetadataSecretDatabase(t)
	planType := "pro"
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	identity := entities.UsageIdentity{
		Name:         "Codex Account",
		AuthType:     entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     "codex-auth",
		Type:         "codex",
		Provider:     "Codex",
		PlanType:     &planType,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}

	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
	for _, path := range []string{
		"/api/v1/usage/identities",
		"/api/v1/usage/identities/page?auth_type=1&page=1&page_size=10",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(response, request)
		body := response.Body.String()
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, body)
		}
		if !strings.Contains(body, `"subscription":{"provider":"codex","plan":"pro-20x"}`) {
			t.Fatalf("%s missing subscription: %s", path, body)
		}
		if strings.Contains(body, `"plan_type"`) {
			t.Fatalf("%s retained legacy plan_type: %s", path, body)
		}
	}
}
