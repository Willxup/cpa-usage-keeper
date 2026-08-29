package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

const (
	standardSessionCookieName     = "cpa_usage_keeper_session"
	embedSessionCookieName        = "cpa_usage_keeper_embed_session"
	embedHeaderName               = "X-CPA-Usage-Keeper-Embed"
	embedSessionHeaderName        = "X-CPA-Usage-Keeper-Embed-Session"
	requestIntentHeaderName       = "X-CPA-Usage-Keeper-Request"
	requestIntentHeaderValueFetch = "fetch"
)

func TestPasswordLoginSetsStandardCookieForNormalRequest(t *testing.T) {
	router, _ := newEmbedAuthRouter(time.Hour)

	resp := httptest.NewRecorder()
	req := newPasswordLoginRequest(false, true)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	cookie := requireCookie(t, resp.Result().Cookies(), standardSessionCookieName)
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected standard cookie SameSite=Lax, got %v", cookie.SameSite)
	}
	if cookie.Secure {
		t.Fatal("expected standard httptest cookie not to force Secure")
	}
	if cookie.Partitioned {
		t.Fatal("expected standard cookie not to be Partitioned")
	}
	if findCookie(resp.Result().Cookies(), embedSessionCookieName) != nil {
		t.Fatalf("expected normal login not to set embed cookie, got %+v", resp.Result().Cookies())
	}
}

func TestPasswordLoginSetsEmbedCookieForCPAMCEmbedRequest(t *testing.T) {
	router, _ := newEmbedAuthRouter(time.Hour)

	resp := httptest.NewRecorder()
	req := newPasswordLoginRequest(true, true)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("expected embed cookie SameSite=None, got %v", cookie.SameSite)
	}
	if !cookie.Secure {
		t.Fatal("expected embed cookie to force Secure")
	}
	if !cookie.Partitioned {
		t.Fatal("expected embed cookie to be Partitioned")
	}
	if findCookie(resp.Result().Cookies(), standardSessionCookieName) != nil {
		t.Fatalf("expected embed login not to set standard cookie, got %+v", resp.Result().Cookies())
	}
	token := requireEmbedSessionToken(t, resp)
	if token != cookie.Value {
		t.Fatalf("expected embed login response token to match cookie, got response %q cookie %q", token, cookie.Value)
	}
}

func TestAPIKeyLoginSetsEmbedCookieAndSourceForCPAMCEmbedRequest(t *testing.T) {
	router, sessions := newEmbedAuthRouterWithOptions(time.Hour, "", &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-cpa-viewer", DisplayKey: "sk-...viewer"}})

	resp := httptest.NewRecorder()
	req := newAPIKeyLoginRequest("/api/v1/auth/api-key-login", true, true)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected API key login status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.SameSite != http.SameSiteNoneMode || !cookie.Secure || !cookie.Partitioned {
		t.Fatalf("expected API key embed login to set embed cookie attributes, got %+v", cookie)
	}
	if findCookie(resp.Result().Cookies(), standardSessionCookieName) != nil {
		t.Fatalf("expected API key embed login not to set standard cookie, got %+v", resp.Result().Cookies())
	}
	token := requireEmbedSessionToken(t, resp)
	if token != cookie.Value {
		t.Fatalf("expected API key embed login response token to match cookie, got response %q cookie %q", token, cookie.Value)
	}
	session, ok := sessions.Get(token)
	if !ok {
		t.Fatal("expected API key embed login session to be stored")
	}
	if session.Role != auth.RoleAPIKeyViewer || session.Source != auth.SessionSourceEmbed || session.CPAAPIKeyID != 42 {
		t.Fatalf("expected API key embed session with source and key id, got %+v", session)
	}
}

func TestAPIKeyViewerEmbedHeaderTokenGetsSession(t *testing.T) {
	router, sessions := newEmbedAuthRouterWithOptions(time.Hour, "", &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-cpa-viewer", DisplayKey: "sk-...viewer", KeyAlias: "Team Key"}})
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewerWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(embedSessionHeaderName, token)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected API key viewer session status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"authenticated":true`) || !strings.Contains(resp.Body.String(), `"role":"api_key_viewer"`) || !strings.Contains(resp.Body.String(), `"alias":"Team Key"`) {
		t.Fatalf("expected header-only API key viewer embed session response, got %s", resp.Body.String())
	}
}

func TestEmbedRequestDoesNotUseStandardCookie(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("expected embed request not to use standard cookie, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestEmbedHeaderRejectsStandardSourceSession(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(embedSessionHeaderName, token)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected embed header with standard session source to be rejected, got %d %s", resp.Code, resp.Body.String())
	}
	if !sessions.Validate(token) {
		t.Fatal("expected rejecting mismatched header source not to delete the original standard session")
	}
	if cookie := findCookie(resp.Result().Cookies(), embedSessionCookieName); cookie != nil {
		t.Fatalf("expected mismatched header source not to clear cookie, got %+v", cookie)
	}
}

func TestEmbedCookieRejectsStandardSourceSession(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected source-mismatched embed request to be rejected, got %d %s", resp.Code, resp.Body.String())
	}
	if !sessions.Validate(token) {
		t.Fatal("expected rejecting mismatched source not to delete the original standard session")
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.MaxAge >= 0 || cookie.Name != embedSessionCookieName {
		t.Fatalf("expected mismatched embed request to clear embed cookie only, got %+v", cookie)
	}
}

func TestEmbedRequestUsesEmbedCookieWhenBothSessionCookiesExist(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	standardToken, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	embedToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	items := listManagedSessionItemsWithHeader(t, router, &http.Cookie{Name: embedSessionCookieName, Value: embedToken}, "", true, &http.Cookie{Name: standardSessionCookieName, Value: standardToken})
	requireCurrentSession(t, items, auth.SessionTokenHash(embedToken), "embed")
}

func TestEmbedRequestUsesHeaderTokenWhenCookieMissing(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(embedSessionHeaderName, token)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":true`) {
		t.Fatalf("expected embed header token to authenticate, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestEmbedRequestFallsBackToHeaderWhenCookieIsInvalid(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(embedSessionHeaderName, token)
	req.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: "stale-token"})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":true`) {
		t.Fatalf("expected embed request to fall back to header token, got %d %s", resp.Code, resp.Body.String())
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.MaxAge >= 0 {
		t.Fatalf("expected invalid embed cookie to be cleared after header fallback, got %+v", cookie)
	}
}

func TestEmbedRequestPrefersValidCookieOverHeaderToken(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	cookieToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource cookie returned error: %v", err)
	}
	headerToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource header returned error: %v", err)
	}

	items := listManagedSessionItemsWithHeader(t, router, &http.Cookie{Name: embedSessionCookieName, Value: cookieToken}, headerToken, true)
	requireCurrentSession(t, items, auth.SessionTokenHash(cookieToken), "embed")
}

func TestNormalRequestIgnoresEmbedSessionHeader(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set(embedSessionHeaderName, token)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("expected normal request not to use embed session header, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestNormalRequestDoesNotUseEmbedCookie(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("expected normal request not to use embed cookie, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestStandardCookieRejectsEmbedSourceSession(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("expected source-mismatched standard request to be unauthenticated, got %d %s", resp.Code, resp.Body.String())
	}
	if !sessions.Validate(token) {
		t.Fatal("expected rejecting mismatched source not to delete the original embed session")
	}
	cookie := requireCookie(t, resp.Result().Cookies(), standardSessionCookieName)
	if cookie.MaxAge >= 0 || cookie.Name != standardSessionCookieName {
		t.Fatalf("expected mismatched standard request to clear standard cookie only, got %+v", cookie)
	}
}

func TestManagedSessionsUseHeaderOnlyEmbedToken(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	currentToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource current returned error: %v", err)
	}
	otherToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource other returned error: %v", err)
	}

	items := listManagedSessionItemsWithHeader(t, router, nil, currentToken, true)
	requireCurrentSession(t, items, auth.SessionTokenHash(currentToken), "embed")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/"+auth.SessionTokenHash(otherToken), nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(embedSessionHeaderName, currentToken)
	req.Header.Set(requestIntentHeaderName, "fetch")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected header-only revoke status 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !sessions.Validate(currentToken) {
		t.Fatal("expected current header-only embed session to remain valid")
	}
	if sessions.Validate(otherToken) {
		t.Fatal("expected other embed session to be revoked by header-only request")
	}
	if cookie := findCookie(resp.Result().Cookies(), embedSessionCookieName); cookie != nil {
		t.Fatalf("expected header-only revoke of another session not to clear cookie, got %+v", cookie)
	}
}

func TestLogoutClearsEmbedCookieForCPAMCEmbedRequest(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(requestIntentHeaderName, "fetch")
	req.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected logout status 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected embed logout to delete embed session")
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.MaxAge >= 0 || cookie.SameSite != http.SameSiteNoneMode || !cookie.Secure || !cookie.Partitioned {
		t.Fatalf("expected embed logout to clear embed cookie with matching attributes, got %+v", cookie)
	}
}

func TestLogoutUsesHeaderOnlyEmbedToken(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(embedSessionHeaderName, token)
	req.Header.Set(requestIntentHeaderName, "fetch")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected header-only logout status 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected header-only embed logout to delete embed session")
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.MaxAge >= 0 || cookie.SameSite != http.SameSiteNoneMode || !cookie.Secure || !cookie.Partitioned {
		t.Fatalf("expected header-only embed logout to clear embed cookie with matching attributes, got %+v", cookie)
	}
}

func TestManagedSessionsExposeSourcesAfterLogin(t *testing.T) {
	router, _ := newEmbedAuthRouter(time.Hour)

	standardLogin := httptest.NewRecorder()
	router.ServeHTTP(standardLogin, newPasswordLoginRequest(false, true))
	if standardLogin.Code != http.StatusNoContent {
		t.Fatalf("expected standard login status 204, got %d body=%s", standardLogin.Code, standardLogin.Body.String())
	}
	standardCookie := requireCookie(t, standardLogin.Result().Cookies(), standardSessionCookieName)

	embedLogin := httptest.NewRecorder()
	router.ServeHTTP(embedLogin, newPasswordLoginRequest(true, true))
	if embedLogin.Code != http.StatusOK {
		t.Fatalf("expected embed login status 200, got %d body=%s", embedLogin.Code, embedLogin.Body.String())
	}
	embedCookie := requireCookie(t, embedLogin.Result().Cookies(), embedSessionCookieName)

	standardItems := listManagedSessionItems(t, router, standardCookie, false)
	requireCurrentSource(t, standardItems, "standard")

	embedItems := listManagedSessionItems(t, router, embedCookie, true)
	requireCurrentSource(t, embedItems, "embed")
}

func TestRevokeOtherEmbedSessionDoesNotClearCurrentEmbedCookie(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	currentToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource current returned error: %v", err)
	}
	otherToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource other returned error: %v", err)
	}
	otherSessionID := auth.SessionTokenHash(otherToken)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/"+otherSessionID, nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(requestIntentHeaderName, "fetch")
	req.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: currentToken})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected revoke status 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !sessions.Validate(currentToken) {
		t.Fatal("expected current embed session to remain valid")
	}
	if sessions.Validate(otherToken) {
		t.Fatal("expected other embed session to be revoked")
	}
	if cookie := findCookie(resp.Result().Cookies(), embedSessionCookieName); cookie != nil {
		t.Fatalf("expected revoking another embed session not to clear current cookie, got %+v", cookie)
	}
}

func TestRevokeCurrentEmbedSessionClearsEmbedCookie(t *testing.T) {
	router, sessions := newEmbedAuthRouter(time.Hour)
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}
	sessionID := auth.SessionTokenHash(token)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/"+sessionID, nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.Header.Set(requestIntentHeaderName, "fetch")
	req.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected revoke status 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected revoked embed session to be invalid")
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.MaxAge >= 0 || cookie.Name != embedSessionCookieName {
		t.Fatalf("expected current embed revoke to clear embed cookie, got %+v", cookie)
	}
}

func TestBasePathSessionCookiesUseBasePath(t *testing.T) {
	router, sessions := newEmbedAuthRouterWithOptions(time.Hour, "/cpa", nil)

	loginResp := httptest.NewRecorder()
	router.ServeHTTP(loginResp, newPasswordLoginRequestForPath("/cpa/api/v1/auth/login", false, true))
	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected base path login status 204, got %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	if cookie := requireCookie(t, loginResp.Result().Cookies(), standardSessionCookieName); cookie.Path != "/cpa" {
		t.Fatalf("expected base path login cookie Path=/cpa, got %+v", cookie)
	}

	logoutToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource logout returned error: %v", err)
	}
	logoutResp := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/cpa/api/v1/auth/logout", nil)
	logoutReq.Header.Set(embedHeaderName, "cpamc")
	logoutReq.Header.Set(requestIntentHeaderName, "fetch")
	logoutReq.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: logoutToken})
	router.ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("expected base path logout status 204, got %d body=%s", logoutResp.Code, logoutResp.Body.String())
	}
	if cookie := requireCookie(t, logoutResp.Result().Cookies(), embedSessionCookieName); cookie.Path != "/cpa" {
		t.Fatalf("expected base path logout cookie Path=/cpa, got %+v", cookie)
	}

	revokeToken, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource revoke returned error: %v", err)
	}
	revokeResp := httptest.NewRecorder()
	revokeReq := httptest.NewRequest(http.MethodDelete, "/cpa/api/v1/auth/sessions/"+auth.SessionTokenHash(revokeToken), nil)
	revokeReq.Header.Set(embedHeaderName, "cpamc")
	revokeReq.Header.Set(requestIntentHeaderName, "fetch")
	revokeReq.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: revokeToken})
	router.ServeHTTP(revokeResp, revokeReq)
	if revokeResp.Code != http.StatusNoContent {
		t.Fatalf("expected base path revoke status 204, got %d body=%s", revokeResp.Code, revokeResp.Body.String())
	}
	if cookie := requireCookie(t, revokeResp.Result().Cookies(), embedSessionCookieName); cookie.Path != "/cpa" {
		t.Fatalf("expected base path revoke cookie Path=/cpa, got %+v", cookie)
	}
}

func TestKeyOverviewClearsEmbedCookieWhenViewerAPIKeyIsInactive(t *testing.T) {
	router, sessions := newEmbedAuthRouterWithOptions(time.Hour, "", inactiveCPAAPIKeyProvider{})
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewerWithSource returned error: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?range=8h", nil)
	req.Header.Set(embedHeaderName, "cpamc")
	req.AddCookie(&http.Cookie{Name: embedSessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected inactive API key viewer to be rejected, got %d body=%s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected inactive API key viewer session to be deleted")
	}
	cookie := requireCookie(t, resp.Result().Cookies(), embedSessionCookieName)
	if cookie.MaxAge >= 0 || cookie.SameSite != http.SameSiteNoneMode || !cookie.Secure || !cookie.Partitioned {
		t.Fatalf("expected key overview to clear embed cookie with matching attributes, got %+v", cookie)
	}
}

func TestMutatingAPIRequiresRequestIntentHeader(t *testing.T) {
	router, _ := newEmbedAuthRouter(time.Hour)

	resp := httptest.NewRecorder()
	req := newPasswordLoginRequest(false, false)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected missing request intent to return 403, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMutatingAPIAllowsFetchIntentHeader(t *testing.T) {
	router, _ := newEmbedAuthRouter(time.Hour)

	resp := httptest.NewRecorder()
	req := newPasswordLoginRequest(false, true)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected fetch request intent to allow login, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestGetSessionDoesNotRequireRequestIntentHeader(t *testing.T) {
	router, _ := newEmbedAuthRouter(time.Hour)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected GET session to skip request intent guard, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func newEmbedAuthRouter(ttl time.Duration) (http.Handler, *auth.SessionManager) {
	return newEmbedAuthRouterWithOptions(ttl, "", nil)
}

func newEmbedAuthRouterWithOptions(ttl time.Duration, basePath string, cpaAPIKeys service.CPAAPIKeyProvider) (http.Handler, *auth.SessionManager) {
	sessions := auth.NewSessionManager(ttl)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: ttl, BasePath: basePath}
	return NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), basePath, OptionalProviders{CPAAPIKeys: cpaAPIKeys}), sessions
}

func newPasswordLoginRequest(embed bool, intent bool) *http.Request {
	return newPasswordLoginRequestForPath("/api/v1/auth/login", embed, intent)
}

func newPasswordLoginRequestForPath(path string, embed bool, intent bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	if embed {
		req.Header.Set(embedHeaderName, "cpamc")
	}
	if intent {
		req.Header.Set(requestIntentHeaderName, "fetch")
	}
	return req
}

func newAPIKeyLoginRequest(path string, embed bool, intent bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"apiKey":"sk-cpa-viewer"}`))
	req.Header.Set("Content-Type", "application/json")
	if embed {
		req.Header.Set(embedHeaderName, "cpamc")
	}
	if intent {
		req.Header.Set(requestIntentHeaderName, "fetch")
	}
	return req
}

func listManagedSessionItems(t *testing.T, router http.Handler, cookie *http.Cookie, embed bool) []struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Current bool   `json:"current"`
} {
	return listManagedSessionItemsWithHeader(t, router, cookie, "", embed)
}

func listManagedSessionItemsWithHeader(t *testing.T, router http.Handler, cookie *http.Cookie, headerToken string, embed bool, extraCookies ...*http.Cookie) []struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Current bool   `json:"current"`
} {
	t.Helper()
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	if embed {
		req.Header.Set(embedHeaderName, "cpamc")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for _, extraCookie := range extraCookies {
		if extraCookie != nil {
			req.AddCookie(extraCookie)
		}
	}
	if headerToken != "" {
		req.Header.Set(embedSessionHeaderName, headerToken)
	}
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected session list status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var parsed struct {
		Items []struct {
			ID      string `json:"id"`
			Source  string `json:"source"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode managed sessions: %v", err)
	}
	return parsed.Items
}

func requireEmbedSessionToken(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var parsed struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode embed login response: %v body=%q", err, resp.Body.String())
	}
	if parsed.SessionToken == "" {
		t.Fatalf("expected embed login response to include session_token, got %q", resp.Body.String())
	}
	return parsed.SessionToken
}

func requireCurrentSource(t *testing.T, items []struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Current bool   `json:"current"`
}, source string) {
	t.Helper()
	current := requireSingleCurrentSession(t, items)
	if current.Source != source {
		t.Fatalf("expected current session source %q, got %+v", source, current)
	}
}

func requireCurrentSession(t *testing.T, items []struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Current bool   `json:"current"`
}, id string, source string) {
	t.Helper()
	current := requireSingleCurrentSession(t, items)
	if current.ID != id || current.Source != source {
		t.Fatalf("expected current session id=%q source=%q, got %+v in %+v", id, source, current, items)
	}
}

func requireSingleCurrentSession(t *testing.T, items []struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Current bool   `json:"current"`
}) struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Current bool   `json:"current"`
} {
	t.Helper()
	var current *struct {
		ID      string `json:"id"`
		Source  string `json:"source"`
		Current bool   `json:"current"`
	}
	for _, item := range items {
		if item.Current {
			item := item
			if current != nil {
				t.Fatalf("expected exactly one current session, got at least %+v and %+v in %+v", *current, item, items)
			}
			current = &item
		}
	}
	if current == nil {
		t.Fatalf("expected exactly one current session in %+v", items)
	}
	return *current
}

func requireCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	cookie := findCookie(cookies, name)
	if cookie == nil {
		t.Fatalf("expected cookie %q in %+v", name, cookies)
	}
	return cookie
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

type inactiveCPAAPIKeyProvider struct{}

func (inactiveCPAAPIKeyProvider) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return nil, nil
}

func (inactiveCPAAPIKeyProvider) FindActiveCPAAPIKeyByValue(context.Context, string) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, errors.New("not found")
}

func (inactiveCPAAPIKeyProvider) FindActiveCPAAPIKeyByID(context.Context, int64) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, errors.New("not found")
}

func (inactiveCPAAPIKeyProvider) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, errors.New("not found")
}
