package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/service"
)

type authFileManagementProviderStub struct {
	statusNames    []string
	statusDisabled bool
	statusResponse service.AuthFilesManagementResponse
	statusErr      error
	deleteNames    []string
	deleteResponse service.AuthFilesManagementResponse
	deleteErr      error
}

func (s *authFileManagementProviderStub) SetAuthFilesDisabled(ctx context.Context, names []string, disabled bool) (service.AuthFilesManagementResponse, error) {
	s.statusNames = names
	s.statusDisabled = disabled
	if s.statusErr != nil {
		return service.AuthFilesManagementResponse{}, s.statusErr
	}
	return s.statusResponse, nil
}

func (s *authFileManagementProviderStub) DeleteAuthFiles(ctx context.Context, names []string) (service.AuthFilesManagementResponse, error) {
	s.deleteNames = names
	if s.deleteErr != nil {
		return service.AuthFilesManagementResponse{}, s.deleteErr
	}
	return s.deleteResponse, nil
}

func TestAuthFilesStatusRouteDisablesSelectedNames(t *testing.T) {
	provider := &authFileManagementProviderStub{statusResponse: service.AuthFilesManagementResponse{Names: []string{"a.json", "b.json"}, Affected: 2}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{AuthFiles: provider})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/auth-files/status", strings.NewReader(`{"names":[" a.json ","b.json"],"disabled":true}`))

	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Join(provider.statusNames, ",") != " a.json ,b.json" || !provider.statusDisabled {
		t.Fatalf("unexpected provider request: names=%+v disabled=%v", provider.statusNames, provider.statusDisabled)
	}
	body := resp.Body.String()
	if !contains(body, `"affected":2`) || !contains(body, `"names":["`) || !contains(body, `"a.json"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestAuthFilesDeleteRouteDeletesSelectedNames(t *testing.T) {
	provider := &authFileManagementProviderStub{deleteResponse: service.AuthFilesManagementResponse{Names: []string{"a.json", "b.json"}, Affected: 2}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{AuthFiles: provider})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth-files", strings.NewReader(`{"names":["a.json"," b.json "]}`))

	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Join(provider.deleteNames, ",") != "a.json, b.json " {
		t.Fatalf("unexpected provider request: names=%+v", provider.deleteNames)
	}
	if body := resp.Body.String(); !contains(body, `"affected":2`) || !contains(body, `"b.json"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestAuthFilesManagementRoutesRejectEmptyNames(t *testing.T) {
	provider := &authFileManagementProviderStub{
		statusErr: service.ErrAuthFilesManagementValidation,
		deleteErr: service.ErrAuthFilesManagementValidation,
	}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{AuthFiles: provider})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPatch, path: "/api/v1/auth-files/status", body: `{"names":[" "],"disabled":true}`},
		{method: http.MethodDelete, path: "/api/v1/auth-files", body: `{"names":[]}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: expected status 400, got %d body=%s", tc.method, tc.path, resp.Code, resp.Body.String())
		}
		if body := resp.Body.String(); !contains(body, `"names are required"`) {
			t.Fatalf("%s %s: unexpected response body: %s", tc.method, tc.path, body)
		}
	}
}

func TestAuthFilesManagementRoutesMapValidationErrors(t *testing.T) {
	provider := &authFileManagementProviderStub{statusErr: service.ErrAuthFilesManagementValidation, deleteErr: service.ErrAuthFilesManagementValidation}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{AuthFiles: provider})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPatch, path: "/api/v1/auth-files/status", body: `{"names":["a.json"],"disabled":true}`},
		{method: http.MethodDelete, path: "/api/v1/auth-files", body: `{"names":["a.json"]}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: expected status 400, got %d body=%s", tc.method, tc.path, resp.Code, resp.Body.String())
		}
	}
}

func TestAuthFilesManagementRoutesReturnInternalError(t *testing.T) {
	provider := &authFileManagementProviderStub{statusErr: errors.New("upstream failed")}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{AuthFiles: provider})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/auth-files/status", strings.NewReader(`{"names":["a.json"],"disabled":true}`))

	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", resp.Code, resp.Body.String())
	}
}
