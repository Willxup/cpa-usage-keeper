package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/models"
)

type authFileStub struct {
	files []models.AuthFile
	err   error
}

func (s authFileStub) ListAuthFiles(context.Context) ([]models.AuthFile, error) {
	return s.files, s.err
}

func TestAuthFilesRouteReturnsEmptyResponseWithoutProvider(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth-files", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"files":[]`) {
		t.Fatalf("unexpected response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthFilesRouteReturnsStoredMetadata(t *testing.T) {
	router := NewRouter(nil, nil, nil, authFileStub{files: []models.AuthFile{{
		AuthIndex: "2",
		Name:      "Claude Desktop",
		Email:     "user@example.com",
		Type:      "claude",
		Provider:  "anthropic",
	}}}, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth-files", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"auth_index":"2"`) && contains(body, `"email":"user@example.com"`) && contains(body, `"provider":"anthropic"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}
