package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	keeperapi "cpa-usage-keeper/internal/api"
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
	return s.statusResponse, s.statusErr
}

func (s *authFileManagementProviderStub) DeleteAuthFiles(ctx context.Context, names []string) (service.AuthFilesManagementResponse, error) {
	s.deleteNames = names
	return s.deleteResponse, s.deleteErr
}

func TestAuthFilesStatusRouteDisablesSelectedNames(t *testing.T) {
	provider := &authFileManagementProviderStub{statusResponse: service.AuthFilesManagementResponse{Names: []string{"a.json", "b.json"}, Affected: 2}}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{AuthFiles: provider})

	resp := serveCredentialMutation(router, http.MethodPatch, "/api/v1/auth-files/status", `{"names":[" a.json ","b.json"],"disabled":true}`)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !slices.Equal(provider.statusNames, []string{" a.json ", "b.json"}) || !provider.statusDisabled {
		t.Fatalf("unexpected provider request: names=%+v disabled=%v", provider.statusNames, provider.statusDisabled)
	}
	var parsed service.AuthFilesManagementResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(parsed, provider.statusResponse) {
		t.Fatalf("unexpected response: %+v", parsed)
	}
}

func TestAuthFilesDeleteRouteDeletesSelectedNames(t *testing.T) {
	provider := &authFileManagementProviderStub{deleteResponse: service.AuthFilesManagementResponse{Names: []string{"a.json", "b.json"}, Affected: 2}}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{AuthFiles: provider})

	resp := serveCredentialMutation(router, http.MethodDelete, "/api/v1/auth-files", `{"names":["a.json"," b.json "]}`)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !slices.Equal(provider.deleteNames, []string{"a.json", " b.json "}) {
		t.Fatalf("unexpected provider request: names=%+v", provider.deleteNames)
	}
	var parsed service.AuthFilesManagementResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(parsed, provider.deleteResponse) {
		t.Fatalf("unexpected response: %+v", parsed)
	}
}

func TestAuthFilesManagementRoutesRejectEmptyNames(t *testing.T) {
	provider := &authFileManagementProviderStub{
		statusErr: service.ErrAuthFilesManagementValidation,
		deleteErr: service.ErrAuthFilesManagementValidation,
	}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{AuthFiles: provider})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPatch, path: "/api/v1/auth-files/status", body: `{"names":[" "],"disabled":true}`},
		{method: http.MethodDelete, path: "/api/v1/auth-files", body: `{"names":[]}`},
	} {
		resp := serveCredentialMutation(router, tc.method, tc.path, tc.body)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: expected status 400, got %d body=%s", tc.method, tc.path, resp.Code, resp.Body.String())
		}
		if body := resp.Body.String(); !strings.Contains(body, `"names are required"`) {
			t.Fatalf("%s %s: unexpected response body: %s", tc.method, tc.path, body)
		}
	}
}

func TestAuthFilesManagementRoutesMapValidationErrors(t *testing.T) {
	provider := &authFileManagementProviderStub{statusErr: service.ErrAuthFilesManagementValidation, deleteErr: service.ErrAuthFilesManagementValidation}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{AuthFiles: provider})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPatch, path: "/api/v1/auth-files/status", body: `{"names":["a.json"],"disabled":true}`},
		{method: http.MethodDelete, path: "/api/v1/auth-files", body: `{"names":["a.json"]}`},
	} {
		resp := serveCredentialMutation(router, tc.method, tc.path, tc.body)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: expected status 400, got %d body=%s", tc.method, tc.path, resp.Code, resp.Body.String())
		}
	}
}

func TestAuthFilesManagementRoutesReturnInternalError(t *testing.T) {
	provider := &authFileManagementProviderStub{statusErr: errors.New("upstream failed")}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{AuthFiles: provider})

	resp := serveCredentialMutation(router, http.MethodPatch, "/api/v1/auth-files/status", `{"names":["a.json"],"disabled":true}`)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", resp.Code, resp.Body.String())
	}
}
