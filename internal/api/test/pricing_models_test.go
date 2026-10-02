package test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

const completePricingHTTPBody = `{"model":"openai/gpt-5.6","pricing_style":"openai","base_prices":{"input":0,"output":2,"cache_read":0,"cache_write":0},"model_multiplier":0,"conditional_multipliers":[{"key":"service_tier","value":"priority","multiplier":0}],"branches":[]}`

func TestPricingModelsHTTPPersistsCompleteConfiguration(t *testing.T) {
	db := openAPITestDatabase(t)
	provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
	router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
	put := newPricingRequest(http.MethodPut, "/api/v1/pricing/models", completePricingHTTPBody)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, put)
	if response.Code != http.StatusOK {
		t.Fatalf("save full model status %d: %s", response.Code, response.Body.String())
	}
	var saved servicedto.SavePricingModelResponse
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || saved.Model != "openai/gpt-5.6" || saved.ConfigRevision != 1 {
		t.Fatalf("saved response %+v, err %v", saved, err)
	}
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, newPricingRequest(http.MethodGet, "/api/v1/pricing/models", ""))
	var models servicedto.PricingModelsResponse
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &models) != nil || models.ConfigRevision != 1 || len(models.Models) != 1 || models.Models[0].ModelMultiplier != 0 || len(models.Models[0].ConditionalMultipliers) != 1 {
		t.Fatalf("list complete models status %d: %s", listed.Code, listed.Body.String())
	}
	cost, available := 4.25, true
	if err := db.Create(&entities.UsageEvent{EventKey: "historical-priced-event", Model: saved.Model, AuthType: "oauth", AuthIndex: "auth-a", CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
		t.Fatal(err)
	}
	deleted := httptest.NewRecorder()
	router.ServeHTTP(deleted, newPricingRequest(http.MethodDelete, "/api/v1/pricing/models?model="+url.QueryEscape(saved.Model), ""))
	var deleteResponse servicedto.DeletePricingModelResponse
	if deleted.Code != http.StatusOK || json.Unmarshal(deleted.Body.Bytes(), &deleteResponse) != nil || deleteResponse.ConfigRevision != 2 {
		t.Fatalf("delete configured model status %d: %s", deleted.Code, deleted.Body.String())
	}
	var stored entities.UsageEvent
	if err := db.Where("event_key = ?", "historical-priced-event").Take(&stored).Error; err != nil || stored.CostUSD == nil || *stored.CostUSD != cost || stored.CostAvailable == nil || !*stored.CostAvailable {
		t.Fatalf("delete changed historical fee: %+v err=%v", stored, err)
	}
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, newPricingRequest(http.MethodDelete, "/api/v1/pricing/models?model="+url.QueryEscape(saved.Model), ""))
	var notFound servicedto.PricingErrorResponse
	if missing.Code != http.StatusNotFound || json.Unmarshal(missing.Body.Bytes(), &notFound) != nil || notFound.Code != "model_not_found" {
		t.Fatalf("repeat delete status %d: %s", missing.Code, missing.Body.String())
	}
}

func TestPricingModelsHTTPRoundTripsContextAndPeriodBranch(t *testing.T) {
	db := openAPITestDatabase(t)
	provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
	router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
	branch := `{"id":"night","name":"Night","context":{"type":"gt","threshold":200000},"period":{"type":"window","start":"20:00","end":"08:00"},"prices":{"input":2.5,"output":15,"cache_read":0.25,"cache_write":1}}`
	body := strings.Replace(completePricingHTTPBody, `"branches":[]`, `"branches":[`+branch+`]`, 1)
	saved := httptest.NewRecorder()
	router.ServeHTTP(saved, newPricingRequest(http.MethodPut, "/api/v1/pricing/models", body))
	if saved.Code != http.StatusOK {
		t.Fatalf("save branch status %d: %s", saved.Code, saved.Body.String())
	}
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, newPricingRequest(http.MethodGet, "/api/v1/pricing/models", ""))
	var models servicedto.PricingModelsResponse
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &models) != nil || len(models.Models) != 1 || len(models.Models[0].Branches) != 1 {
		t.Fatalf("list saved branch status %d: %s", listed.Code, listed.Body.String())
	}
	got := models.Models[0].Branches[0]
	if got.ID != "night" || got.Context.Threshold == nil || *got.Context.Threshold != 200000 || got.Period.Start == nil || *got.Period.Start != "20:00" || got.Prices.Output != 15 {
		t.Fatalf("branch roundtrip changed: %+v", got)
	}
}

func TestPricingModelsHTTPKeepsAdminBoundaryAndModelOptionsSource(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatal(err)
	}
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(nil, nil, nil, &pricingStub{usedModels: []string{"openai/gpt-5.6", "claude-sonnet"}}, config, keeperapi.NewAuthHandler(config, sessions), "")
	for _, route := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/v1/pricing/models", ""},
		{http.MethodGet, "/api/v1/pricing/model-options", ""},
		{http.MethodPut, "/api/v1/pricing/models", completePricingHTTPBody},
		{http.MethodDelete, "/api/v1/pricing/models?model=openai%2Fgpt-5.6", ""},
	} {
		anonymous := httptest.NewRecorder()
		router.ServeHTTP(anonymous, newPricingRequest(route.method, route.target, route.body))
		if anonymous.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s status %d", route.method, route.target, anonymous.Code)
		}
		viewerRequest := newPricingRequest(route.method, route.target, route.body)
		viewerRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: viewerToken})
		viewer := httptest.NewRecorder()
		router.ServeHTTP(viewer, viewerRequest)
		if viewer.Code != http.StatusForbidden {
			t.Fatalf("viewer %s %s status %d", route.method, route.target, viewer.Code)
		}
	}
	adminRequest := newPricingRequest(http.MethodGet, "/api/v1/pricing/model-options", "")
	adminRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: adminToken})
	options := httptest.NewRecorder()
	router.ServeHTTP(options, adminRequest)
	var models servicedto.PricingModelOptionsResponse
	if options.Code != http.StatusOK || json.Unmarshal(options.Body.Bytes(), &models) != nil || len(models.Models) != 2 || models.Models[0] != "openai/gpt-5.6" {
		t.Fatalf("admin model options status %d: %s", options.Code, options.Body.String())
	}
}

func TestPricingModelsHTTPInternalErrorDoesNotExposeSQL(t *testing.T) {
	router := keeperapi.NewRouter(nil, nil, nil, &pricingStub{err: errors.New("SELECT secret FROM model_price_settings: database disk image is malformed")}, keeperapi.AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, newPricingRequest(http.MethodGet, "/api/v1/pricing/models", ""))
	var failure servicedto.PricingErrorResponse
	if response.Code != http.StatusInternalServerError || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != "internal_error" || strings.Contains(response.Body.String(), "SELECT") || strings.Contains(response.Body.String(), "malformed") {
		t.Fatalf("unsafe internal error status %d: %s", response.Code, response.Body.String())
	}
}

func TestPricingModelsHTTPReturnsStructuredValidationErrorsWithoutWriting(t *testing.T) {
	base := completePricingHTTPBody
	branch := `{"id":"night","name":"Night","context":{"type":"gt","threshold":9007199254740991},"period":{"type":"all"},"prices":{"input":1,"output":0,"cache_read":0,"cache_write":0}}`
	validBranch := strings.Replace(branch, `"threshold":9007199254740991`, `"threshold":1`, 1)
	conflictBranches := `[{"id":"a","name":"A","context":{"type":"gt","threshold":0},"period":{"type":"all"},"prices":{"input":1,"output":0,"cache_read":0,"cache_write":0}},{"id":"b","name":"B","context":{"type":"gt","threshold":0},"period":{"type":"all"},"prices":{"input":2,"output":0,"cache_read":0,"cache_write":0}}]`
	for _, testCase := range []struct {
		name, body, code, path, fieldCode string
	}{
		{"missing branches", strings.Replace(base, `,"branches":[]`, "", 1), "invalid_pricing", "branches", "required"},
		{"null branches", strings.Replace(base, `"branches":[]`, `"branches":null`, 1), "invalid_pricing", "branches", "required"},
		{"missing rule multiplier", strings.Replace(base, `,"multiplier":0`, "", 1), "invalid_pricing", "conditional_multipliers[0].multiplier", "required"},
		{"null rule multiplier", strings.Replace(base, `"multiplier":0`, `"multiplier":null`, 1), "invalid_pricing", "conditional_multipliers[0].multiplier", "required"},
		{"missing cache write price", strings.Replace(base, `,"cache_write":0`, "", 1), "invalid_pricing", "base_prices.cache_write", "required"},
		{"missing branch price", strings.Replace(base, `"branches":[]`, `"branches":[`+strings.Replace(branch, `,"cache_write":0`, "", 1)+`]`, 1), "invalid_pricing", "branches[0].prices.cache_write", "required"},
		{"invalid days", strings.Replace(base, `"branches":[]`, `"branches":[`+strings.Replace(validBranch, `"period":{"type":"all"}`, `"days":"holiday","period":{"type":"all"}`, 1)+`]`, 1), "invalid_pricing", "branches[0].days", "invalid"},
		{"null days", strings.Replace(base, `"branches":[]`, `"branches":[`+strings.Replace(branch, `"period":{"type":"all"}`, `"days":null,"period":{"type":"all"}`, 1)+`]`, 1), "invalid_pricing", "branches[0].days", "invalid"},
		{"weekday cross midnight", strings.Replace(base, `"branches":[]`, `"branches":[`+strings.Replace(validBranch, `"period":{"type":"all"}`, `"days":"weekday","period":{"type":"window","start":"20:00","end":"08:00"}`, 1)+`]`, 1), "invalid_pricing", "branches[0].period.end", "cross_day"},
		{"negative branch price", strings.Replace(base, `"branches":[]`, `"branches":[`+strings.Replace(strings.Replace(branch, "9007199254740991", "200000", 1), `"output":0`, `"output":-1`, 1)+`]`, 1), "invalid_pricing", "branches[0].prices.output", "invalid"},
		{"unconditional branch", strings.Replace(base, `"branches":[]`, `"branches":[`+strings.Replace(branch, `"context":{"type":"gt","threshold":9007199254740991}`, `"context":{"type":"all"}`, 1)+`]`, 1), "invalid_pricing", "branches[0].context", "default_conflict"},
		{"invalid branch period end", strings.Replace(base, `"branches":[]`, `"branches":[`+strings.Replace(strings.Replace(branch, "9007199254740991", "200000", 1), `"period":{"type":"all"}`, `"period":{"type":"window","start":"20:00","end":"25:00"}`, 1)+`]`, 1), "invalid_pricing", "branches[0].period.end", "invalid"},
		{"negative price", strings.Replace(base, `"input":0`, `"input":-1`, 1), "invalid_pricing", "base_prices.input", "invalid"},
		{"unsafe gt threshold", strings.Replace(base, `"branches":[]`, `"branches":[`+branch+`]`, 1), "invalid_pricing", "branches[0].context.threshold", "invalid"},
		{"conflicting branches", strings.Replace(base, `"branches":[]`, `"branches":`+conflictBranches, 1), "branch_conflict", "branches[0].context", "conflict"},
		{"unknown field", strings.Replace(base, `,"branches":[]`, `,"branches":[],"unknown":1`, 1), "invalid_request", "", ""},
		{"unknown nested field", strings.Replace(base, `"cache_write":0`, `"cache_write":0,"hidden":1`, 1), "invalid_request", "", ""},
		{"trailing JSON", base + ` true`, "invalid_request", "", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openAPITestDatabase(t)
			provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
			router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, newPricingRequest(http.MethodPut, "/api/v1/pricing/models", testCase.body))
			var failure servicedto.PricingErrorResponse
			if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != testCase.code || failure.Message == "" {
				t.Fatalf("invalid save status %d: %s", response.Code, response.Body.String())
			}
			if testCase.path != "" {
				found := false
				for _, field := range failure.Fields {
					if field.Path == testCase.path && field.Code == testCase.fieldCode {
						if testCase.code == "branch_conflict" && (len(field.BranchIDs) != 2 || field.BranchIDs[0] != "a" || field.BranchIDs[1] != "b") {
							t.Fatalf("conflict omitted both branch IDs: %+v", field)
						}
						found = true
					}
				}
				if !found {
					t.Fatalf("missing field %s/%s: %+v", testCase.path, testCase.fieldCode, failure.Fields)
				}
			}
			listed := httptest.NewRecorder()
			router.ServeHTTP(listed, newPricingRequest(http.MethodGet, "/api/v1/pricing/models", ""))
			var current servicedto.PricingModelsResponse
			if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &current) != nil || current.ConfigRevision != 0 || len(current.Models) != 0 {
				t.Fatalf("invalid save changed revision/configuration: %d %s", listed.Code, listed.Body.String())
			}
		})
	}
}
