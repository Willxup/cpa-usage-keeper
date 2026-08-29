package test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestPricingRulesGETReturnsModelAndNormalizedRules(t *testing.T) {
	provider := &pricingStub{rules: []servicedto.PricingRule{{Key: "service_tier", Value: "priority", Multiplier: 2}}}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/rules?model=openai%2Fgpt-5.6", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"model":"openai/gpt-5.6"`) || !contains(resp.Body.String(), `"key":"service_tier"`) || !contains(resp.Body.String(), `"multiplier":2`) {
		t.Fatalf("unexpected pricing rules response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestPricingRulesPUTPreservesOmittedNullAndExplicitZeroMultipliers(t *testing.T) {
	for name, body := range map[string]string{
		"omitted": `{"model":"model-a","rules":[{"key":"service_tier","value":"priority"}]}`,
		"null":    `{"model":"model-a","rules":[{"key":"service_tier","value":"priority","multiplier":null}]}`,
		"zero":    `{"model":"model-a","rules":[{"key":"service_tier","value":"priority","multiplier":0}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := &pricingStub{rules: []servicedto.PricingRule{{Key: "service_tier", Value: "priority", Multiplier: 1}}}
			router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
			req := httptest.NewRequest(http.MethodPut, "/api/v1/pricing/rules", strings.NewReader(body))
			req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK || provider.lastRules == nil || len(provider.lastRules.Rules) != 1 {
				t.Fatalf("unexpected %s response: %d %s input=%+v", name, resp.Code, resp.Body.String(), provider.lastRules)
			}
			multiplier := provider.lastRules.Rules[0].Multiplier
			if name == "zero" {
				if multiplier == nil || *multiplier != 0 {
					t.Fatalf("expected explicit zero pointer, got %+v", multiplier)
				}
			} else if multiplier != nil {
				t.Fatalf("expected %s multiplier to remain nil for service defaulting, got %+v", name, multiplier)
			}
		})
	}
}

func TestPricingRulesPUTAllowsEmptyArrayToClearRules(t *testing.T) {
	provider := &pricingStub{rules: []servicedto.PricingRule{}}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pricing/rules", strings.NewReader(`{"model":"model-a","rules":[]}`))
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || provider.lastRules == nil || provider.lastRules.Rules == nil || len(provider.lastRules.Rules) != 0 || !contains(resp.Body.String(), `"rules":[]`) {
		t.Fatalf("expected complete empty replacement, got %d %s input=%+v", resp.Code, resp.Body.String(), provider.lastRules)
	}
}

func TestPricingRulesRoutesMapValidationAndMissingModelErrors(t *testing.T) {
	for _, test := range []struct {
		name        string
		method      string
		url         string
		body        string
		providerErr error
		want        int
	}{
		{name: "missing query model", method: http.MethodGet, url: "/api/v1/pricing/rules", want: http.StatusBadRequest},
		{name: "missing body model", method: http.MethodPut, url: "/api/v1/pricing/rules", body: `{"rules":[]}`, want: http.StatusBadRequest},
		{name: "unknown model", method: http.MethodGet, url: "/api/v1/pricing/rules?model=missing", providerErr: service.ErrPricingModelNotFound, want: http.StatusNotFound},
		{name: "unknown field", method: http.MethodPut, url: "/api/v1/pricing/rules", body: `{"model":"model-a","rules":[{"key":"provider","value":"openai","multiplier":2}]}`, providerErr: service.ErrInvalidPricingRule, want: http.StatusBadRequest},
		{name: "duplicate", method: http.MethodPut, url: "/api/v1/pricing/rules", body: `{"model":"model-a","rules":[{"key":"service_tier","value":"priority","multiplier":2},{"key":"service_tier","value":"priority","multiplier":3}]}`, providerErr: service.ErrInvalidPricingRule, want: http.StatusBadRequest},
		{name: "huge finite combination", method: http.MethodPut, url: "/api/v1/pricing/rules", body: `{"model":"model-a","rules":[{"key":"service_tier","value":"priority","multiplier":1e308}]}`, providerErr: service.ErrInvalidPricingRule, want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &pricingStub{err: test.providerErr}
			router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
			req := httptest.NewRequest(test.method, test.url, strings.NewReader(test.body))
			if test.method == http.MethodPut {
				req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
				req.Header.Set("Content-Type", "application/json")
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			if resp.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", resp.Code, test.want, resp.Body.String())
			}
		})
	}
}

func TestPricingRulesPUTRejectsNonJSONFiniteNumbers(t *testing.T) {
	for _, value := range []string{"NaN", "Infinity", "-Infinity"} {
		provider := &pricingStub{}
		router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
		body := `{"model":"model-a","rules":[{"key":"service_tier","value":"priority","multiplier":` + value + `}]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/pricing/rules", strings.NewReader(body))
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest || provider.lastRules != nil {
			t.Fatalf("expected %s to fail JSON decoding, got %d %s", value, resp.Code, resp.Body.String())
		}
	}
}

func TestPricingRulesRoutesRequireAdminAuthentication(t *testing.T) {
	provider := &pricingStub{err: errors.New("must not be called")}
	config := AuthConfig{Enabled: true, LoginPassword: "secret"}
	router := NewRouter(nil, nil, nil, provider, config, NewAuthHandler(config, nil), "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/rules?model=model-a", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated pricing rules status 401, got %d %s", resp.Code, resp.Body.String())
	}
}
