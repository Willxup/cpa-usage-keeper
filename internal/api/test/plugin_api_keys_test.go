package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestPluginAPIKeyRoutesExposeOnlySafeLabelsAndCompleteSource(t *testing.T) {
	db := openAPITestDatabase(t)
	group := helper.PluginAPIKeyGroupKey("plugin:example:auth", "secret-plugin-principal")
	key := entities.CPAAPIKey{APIKey: group, SourceType: entities.CPAAPIKeySourcePlugin, AccessProvider: "plugin:example:auth"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})
	for _, path := range []string{"/api/v1/usage/api-keys", "/api/v1/usage/api-keys/settings", "/api/v1/usage/api-keys/options"} {
		resp := serveAPIGet(router, path)
		body := resp.Body.String()
		if resp.Code != http.StatusOK || strings.Contains(body, "secret-plugin-principal") || strings.Contains(body, `\"plugin:example:auth\"`) || strings.Contains(body, `"apiKey"`) {
			t.Fatalf("unsafe plugin response for %s: %d %s", path, resp.Code, body)
		}
		if !strings.Contains(body, "Third-party Identity #") {
			t.Fatalf("missing stable identity label: %s", body)
		}
		if strings.HasSuffix(path, "settings") && (!strings.Contains(body, `"sourceType":"plugin"`) || !strings.Contains(body, `"accessProvider":"plugin:example:auth"`) || !strings.Contains(body, `"lastSyncedAt":null`)) {
			t.Fatalf("missing plugin source metadata: %s", body)
		}
	}
	response := serveCredentialMutation(router, http.MethodPatch, "/api/v1/usage/api-keys/1", `{"keyAlias":"Third-party team"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"label":"Third-party team"`) || strings.Contains(response.Body.String(), "secret-plugin-principal") {
		t.Fatalf("unsafe plugin alias result: %d %s", response.Code, response.Body.String())
	}
}

func TestPluginAPIKeyCannotCreateOrUseViewerSession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authLoginKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: helper.PluginAPIKeyGroupKey("plugin:example:auth", "principal"), SourceType: entities.CPAAPIKeySourcePlugin}}
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, sessions), "", keeperapi.OptionalProviders{CPAAPIKeys: keyProvider})
	body, _ := json.Marshal(map[string]string{"apiKey": keyProvider.row.APIKey})
	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/auth/api-key-login", string(body))
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("plugin principal logged in: %d %s", resp.Code, resp.Body.String())
	}
	token, _, err := sessions.CreateAPIKeyViewer(keyProvider.row.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp = serveAPIGet(router, "/api/v1/auth/session", &http.Cookie{Name: standardSessionCookieName, Value: token})
	if strings.Contains(resp.Body.String(), `"authenticated":true`) {
		t.Fatalf("plugin record retained a viewer session: %s", resp.Body.String())
	}
}

func TestPluginAPIKeyUsageDisplayBeforeAndAfterRegistrationNeverLeaksInternalIdentity(t *testing.T) {
	group := helper.PluginAPIKeyGroupKey("plugin:example:auth", "private-team-principal")
	for _, registered := range []bool{false, true} {
		usage := &usageEventsStub{events: []servicedto.UsageEventRecord{{ID: 49, Timestamp: time.Now(), APIGroupKey: group, Model: "model"}}}
		var keys service.CPAAPIKeyProvider
		want := "Third-party Identity"
		if registered {
			keys = &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 7, APIKey: group, SourceType: entities.CPAAPIKeySourcePlugin, KeyAlias: "Team alias"}}
			want = "Team alias"
		}
		router := keeperapi.NewRouter(nil, nil, usage, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{CPAAPIKeys: keys})
		resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")
		if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), want) || strings.Contains(resp.Body.String(), "private-team-principal") || strings.Contains(resp.Body.String(), "plugin:example:auth") {
			t.Fatalf("unsafe usage display: %d %s", resp.Code, resp.Body.String())
		}
	}
}
