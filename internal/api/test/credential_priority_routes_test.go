package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/service"
)

type priorityProviderStub struct {
	authIndex string
	priority  int
	err       error
}

func (s *priorityProviderStub) SetAuthFilePriority(_ context.Context, authIndex string, priority int) (service.CredentialPriorityResponse, error) {
	s.authIndex, s.priority = authIndex, priority
	if s.err != nil {
		return service.CredentialPriorityResponse{}, s.err
	}
	return service.CredentialPriorityResponse{AuthIndex: authIndex, Priority: priority}, nil
}

func (s *priorityProviderStub) SetAIProviderPriority(ctx context.Context, authIndex string, priority int) (service.CredentialPriorityResponse, error) {
	return s.SetAuthFilePriority(ctx, authIndex, priority)
}

func priorityRouteRequest(t *testing.T, provider service.CredentialPriorityProvider, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CredentialPriority: provider})
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestCredentialPriorityRoutesAcceptSignedAndZeroIntegers(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		want       int
	}{
		{"/api/v1/auth-files/auth-1/priority", `{"priority":0}`, 0},
		{"/api/v1/ai-providers/ai-1/priority", `{"priority":-12}`, -12},
		{"/api/v1/ai-providers/ai-1/priority", `{"priority":10}`, 10},
	} {
		provider := &priorityProviderStub{}
		response := priorityRouteRequest(t, provider, tc.path, tc.body)
		if response.Code != http.StatusOK || provider.priority != tc.want || provider.authIndex == "" {
			t.Fatalf("path=%s body=%s status=%d provider=%+v", tc.path, tc.body, response.Code, provider)
		}
	}
}

func TestCredentialPriorityRoutesRejectInvalidNumbers(t *testing.T) {
	for _, body := range []string{`{}`, `{"priority":null}`, `{"priority":1.2}`, `{"priority":1e1}`, `{"priority":"1"}`, `{"priority":9007199254740992}`, `{"priority":-9007199254740992}`} {
		provider := &priorityProviderStub{}
		response := priorityRouteRequest(t, provider, "/api/v1/auth-files/auth-1/priority", body)
		if response.Code != http.StatusBadRequest || provider.authIndex != "" {
			t.Fatalf("body=%s status=%d provider=%+v", body, response.Code, provider)
		}
	}
}

func TestCredentialPriorityRoutesMapUpstreamRejectionWithoutLeakingDetails(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{service.ErrCredentialPriorityNotFound, http.StatusNotFound},
		{service.ErrCredentialPriorityConflict, http.StatusConflict},
		{service.ErrCredentialPriorityUnsupported, http.StatusConflict},
		{errors.New("upstream secret-xyz"), http.StatusInternalServerError},
	} {
		response := priorityRouteRequest(t, &priorityProviderStub{err: tc.err}, "/api/v1/ai-providers/ai-1/priority", `{"priority":1}`)
		if response.Code != tc.want || strings.Contains(response.Body.String(), "secret-xyz") {
			t.Fatalf("err=%v status=%d body=%s", tc.err, response.Code, response.Body.String())
		}
	}
}
