package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/service"
)

type credentialStatusProviderStub struct {
	authFileIndex    string
	authFileDisabled bool
	authFileErr      error
	providerIndex    string
	providerDisabled bool
	providerErr      error
}

func (s *credentialStatusProviderStub) SetAuthFileDisabled(_ context.Context, authIndex string, disabled bool) (service.CredentialStatusResponse, error) {
	s.authFileIndex = authIndex
	s.authFileDisabled = disabled
	if s.authFileErr != nil {
		return service.CredentialStatusResponse{}, s.authFileErr
	}
	return service.CredentialStatusResponse{AuthIndex: authIndex, Disabled: disabled}, nil
}

func (s *credentialStatusProviderStub) SetAIProviderDisabled(_ context.Context, authIndex string, disabled bool) (service.CredentialStatusResponse, error) {
	s.providerIndex = authIndex
	s.providerDisabled = disabled
	if s.providerErr != nil {
		return service.CredentialStatusResponse{}, s.providerErr
	}
	return service.CredentialStatusResponse{AuthIndex: authIndex, Disabled: disabled}, nil
}

func performCredentialStatusRequest(t *testing.T, provider service.CredentialStatusProvider, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CredentialStatus: provider})
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestAuthFileStatusRouteTranslatesAuthIndex(t *testing.T) {
	provider := &credentialStatusProviderStub{}
	response := performCredentialStatusRequest(t, provider, "/api/v1/auth-files/idx-a/status", `{"disabled":true}`)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", response.Code, response.Body.String())
	}
	if provider.authFileIndex != "idx-a" || !provider.authFileDisabled {
		t.Fatalf("unexpected provider call: index=%q disabled=%v", provider.authFileIndex, provider.authFileDisabled)
	}
	if !strings.Contains(response.Body.String(), `"auth_index":"idx-a"`) || !strings.Contains(response.Body.String(), `"disabled":true`) {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}

func TestAIProviderStatusRouteTranslatesAuthIndex(t *testing.T) {
	provider := &credentialStatusProviderStub{}
	response := performCredentialStatusRequest(t, provider, "/api/v1/ai-providers/idx-g/status", `{"disabled":false}`)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", response.Code, response.Body.String())
	}
	if provider.providerIndex != "idx-g" || provider.providerDisabled {
		t.Fatalf("unexpected provider call: index=%q disabled=%v", provider.providerIndex, provider.providerDisabled)
	}
}

func TestCredentialStatusRoutesRequireDisabledFlag(t *testing.T) {
	cases := []string{"/api/v1/auth-files/idx-a/status", "/api/v1/ai-providers/idx-g/status"}
	for _, path := range cases {
		provider := &credentialStatusProviderStub{}
		response := performCredentialStatusRequest(t, provider, path, `{}`)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected status 400, got %d", path, response.Code)
		}
		if provider.authFileIndex != "" || provider.providerIndex != "" {
			t.Fatalf("%s: expected provider to stay untouched", path)
		}
	}
}

func TestCredentialStatusRoutesMapServiceErrors(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		// 服务层永远返回 fmt.Errorf("%w: ...") 包装过的哨兵，这里必须复现真实形状：
		// 若把路由里的 errors.Is 换成 ==，这些用例会一起失败，而不是继续通过。
		{name: "validation", err: fmt.Errorf("%w: auth_index is required", service.ErrCredentialStatusValidation), wantStatus: http.StatusBadRequest, wantBody: "invalid credential status request"},
		{name: "not found", err: fmt.Errorf("%w: auth file identity", service.ErrCredentialStatusNotFound), wantStatus: http.StatusNotFound, wantBody: "credential not found"},
		{name: "unsupported", err: fmt.Errorf("%w: provider type %q", service.ErrCredentialStatusUnsupported, "openai"), wantStatus: http.StatusConflict, wantBody: "credential status is not supported"},
		{name: "conflict", err: fmt.Errorf("%w: auth file", service.ErrCredentialStatusConflict), wantStatus: http.StatusConflict, wantBody: "credential status target cannot be changed on its own"},
		{name: "internal", err: fmt.Errorf("update auth file status: %w", errors.New("upstream failed with api-key secret-xyz")), wantStatus: http.StatusInternalServerError, wantBody: "internal server error"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			provider := &credentialStatusProviderStub{authFileErr: tc.err, providerErr: tc.err}
			response := performCredentialStatusRequest(t, provider, "/api/v1/ai-providers/idx-g/status", `{"disabled":true}`)
			if response.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, response.Code, response.Body.String())
			}
			body := response.Body.String()
			if !strings.Contains(body, tc.wantBody) {
				t.Fatalf("expected body to contain %q, got %s", tc.wantBody, body)
			}
			// 服务层错误文本可能带上上游细节，响应体不能把它们回显给客户端。
			if strings.Contains(body, "secret-xyz") {
				t.Fatalf("response leaked upstream detail: %s", body)
			}
		})
	}
}

func TestCredentialStatusRoutesReportUnconfiguredProvider(t *testing.T) {
	response := performCredentialStatusRequest(t, nil, "/api/v1/auth-files/idx-a/status", `{"disabled":true}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", response.Code, response.Body.String())
	}
}
