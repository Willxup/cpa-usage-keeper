package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPasswordLoginCapturesSessionClientMetadataFromRightmostForwardedIP(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, sessions), "")
	userAgent := "Keeper-Test/" + strings.Repeat("a", 600) + "/tail-marker"

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	loginRequest.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("User-Agent", userAgent)
	loginRequest.Header.Set("X-Forwarded-For", "198.51.100.7, invalid, 203.0.113.9")
	loginRequest.RemoteAddr = "172.17.0.1:42310"
	router.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	session, ok := sessions.Get(cookies[0].Value)
	if !ok {
		t.Fatal("expected created session")
	}
	if session.LoginIP != "203.0.113.9" || session.LastSeenIP != "203.0.113.9" || session.UserAgent != userAgent {
		t.Fatalf("unexpected captured metadata: %+v", session)
	}

	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	listRequest.AddCookie(cookies[0])
	listRequest.Header.Set("X-Forwarded-For", "203.0.113.9")
	listRequest.RemoteAddr = "172.17.0.1:42311"
	router.ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK {
		t.Fatalf("expected session list status 200, got %d body=%s", list.Code, list.Body.String())
	}
	var parsed struct {
		Items []struct {
			LoginIP    string `json:"loginIp"`
			LastSeenIP string `json:"lastSeenIp"`
			UserAgent  string `json:"userAgent"`
			LastSeen   string `json:"lastSeenAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode session list: %v", err)
	}
	if len(parsed.Items) != 1 {
		t.Fatalf("expected one session, got %+v", parsed.Items)
	}
	item := parsed.Items[0]
	if item.LoginIP != "203.0.113.9" || item.LastSeenIP != "203.0.113.9" || item.UserAgent != userAgent || item.LastSeen == "" {
		t.Fatalf("unexpected session response metadata: %+v", item)
	}
	for _, parsedField := range []string{`"browser":`, `"os":`, `"device":`} {
		if strings.Contains(list.Body.String(), parsedField) {
			t.Fatalf("session response must expose only the raw User-Agent, got parsed field %s in %s", parsedField, list.Body.String())
		}
	}
}

func TestPasswordLoginFallsBackToObservedClientIPWithoutForwardedHeader(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, sessions), "")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "172.17.0.1:42310"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d body=%s", response.Code, response.Body.String())
	}
	session, ok := sessions.Get(response.Result().Cookies()[0].Value)
	if !ok || session.LoginIP != "172.17.0.1" {
		t.Fatalf("expected observed Docker gateway IP fallback, got %+v", session)
	}
}

func TestManagedSessionsSortCurrentFirstThenRecentActivityDescending(t *testing.T) {
	db := openSessionMetadataAPIDatabase(t)
	manager := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	currentToken, _, err := manager.CreateWithSourceAndMetadata(auth.SessionSourceStandard, auth.SessionClientMetadata{IP: "203.0.113.1"})
	if err != nil {
		t.Fatalf("create current session: %v", err)
	}
	olderToken, _, err := manager.CreateWithSourceAndMetadata(auth.SessionSourceStandard, auth.SessionClientMetadata{IP: "203.0.113.2"})
	if err != nil {
		t.Fatalf("create older session: %v", err)
	}
	newerToken, _, err := manager.CreateWithSourceAndMetadata(auth.SessionSourceStandard, auth.SessionClientMetadata{IP: "203.0.113.3"})
	if err != nil {
		t.Fatalf("create newer session: %v", err)
	}
	now := timeutil.NormalizeStorageTime(time.Now())
	setSessionLastSeen(t, db, olderToken, now.Add(-20*time.Minute))
	setSessionLastSeen(t, db, newerToken, now.Add(-10*time.Minute))

	restarted := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, restarted), "")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	request.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: currentToken})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected session list status 200, got %d body=%s", response.Code, response.Body.String())
	}
	var parsed struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode session list: %v", err)
	}
	want := []string{auth.SessionTokenHash(currentToken), auth.SessionTokenHash(newerToken), auth.SessionTokenHash(olderToken)}
	if len(parsed.Items) != len(want) {
		t.Fatalf("unexpected session count: %+v", parsed.Items)
	}
	for index, id := range want {
		if parsed.Items[index].ID != id {
			t.Fatalf("session %d: expected %s, got %+v", index, id, parsed.Items)
		}
	}
	if !parsed.Items[0].Current {
		t.Fatalf("expected first session to be current, got %+v", parsed.Items)
	}
}

func openSessionMetadataAPIDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth-session-api.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.AutoMigrate(&entities.AuthSession{}); err != nil {
		t.Fatalf("auto migrate auth sessions: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func setSessionLastSeen(t *testing.T, db *gorm.DB, token string, value time.Time) {
	t.Helper()
	if err := db.Model(&entities.AuthSession{}).
		Where("token_hash = ?", auth.SessionTokenHash(token)).
		Update("last_seen_at", value).Error; err != nil {
		t.Fatalf("set session last seen: %v", err)
	}
}
