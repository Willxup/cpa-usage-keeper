package test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReadOnlySessionBindingFailsClosed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "session.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&entities.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	// Missing credential storage must not accept previously issued read-only cookies.
	manager := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	token, _, err := manager.CreateReadOnlyWithSourceAndMetadata(auth.SessionSourceStandard, auth.SessionClientMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "admin-test-password", ReadOnlyPassword: "read-only-test-password"}
	router := NewRouter(nil, nil, &readOnlyUsageStub{}, nil, config, NewAuthHandler(config, manager), "")
	cookie := &http.Cookie{Name: "cpa_usage_keeper_session", Value: token}
	if serveAPIGet(router, "/api/v1/read-only/overview", cookie).Code != 401 {
		t.Fatal("read-only storage failure did not fail closed")
	}
	state := serveAPIGet(router, "/api/v1/auth/session", cookie)
	if state.Code != 200 || !strings.Contains(state.Body.String(), `"authenticated":false`) {
		t.Fatal("invalid read-only session reported authenticated")
	}
	admin, _, err := manager.Create()
	if err != nil {
		t.Fatal(err)
	}
	if !manager.Validate(admin) {
		t.Fatal("read-only failure revoked admin")
	}
}

func TestLoginCookieChoiceForAllRoles(t *testing.T) {
	for _, mode := range []struct {
		path, field, credential string
		role                    auth.Role
	}{
		{"login", "password", "admin-test-password", auth.RoleAdmin},
		{"read-only-login", "password", "read-only-test-password", auth.RoleReadOnly},
		{"api-key-login", "apiKey", "test-client-key", auth.RoleAPIKeyViewer},
	} {
		for _, choice := range []string{"false", "true", "legacy"} {
			t.Run(mode.path+"/"+choice, func(t *testing.T) {
				manager := auth.NewSessionManager(7 * 24 * time.Hour)
				config := AuthConfig{Enabled: true, LoginPassword: "admin-test-password", ReadOnlyPassword: "read-only-test-password"}
				keys := &authLoginKeyStub{row: entities.CPAAPIKey{ID: 1, APIKey: "test-client-key"}}
				router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, manager), "", OptionalProviders{CPAAPIKeys: keys})
				payload := map[string]any{mode.field: mode.credential}
				if choice != "legacy" {
					payload["rememberMe"] = choice == "true"
				}
				body, _ := json.Marshal(payload)
				response := serveCredentialMutation(router, "POST", "/api/v1/auth/"+mode.path, string(body))
				if response.Code != 204 {
					t.Fatal(response.Code)
				}
				cookie := response.Result().Cookies()[0]
				if choice == "false" {
					if !cookie.Expires.IsZero() || cookie.MaxAge != 0 {
						t.Fatal("session cookie had persistent expiry")
					}
				} else if cookie.Expires.IsZero() || cookie.MaxAge < 6*24*60*60 {
					t.Fatal("persistent cookie lost expiry")
				}
				if !cookie.HttpOnly || cookie.Path != "/" {
					t.Fatal("cookie protections/path changed")
				}
				session, ok := manager.Get(cookie.Value)
				if !ok || session.Role != mode.role || session.ExpiresAt.Sub(session.CreatedAt) != 7*24*time.Hour {
					t.Fatal("choice changed role or server expiry")
				}
				for i := 0; i < 2; i++ {
					state := serveAPIGet(router, "/api/v1/auth/session", cookie)
					if state.Code != 200 || !strings.Contains(state.Body.String(), `"authenticated":true`) {
						t.Fatal("reload/another tab lost login")
					}
				}
			})
		}
	}
}
