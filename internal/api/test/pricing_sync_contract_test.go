package test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type pricingSyncHTTPTransport func(*http.Request) (*http.Response, error)

func (transport pricingSyncHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestPricingSyncFetchHTTPUsesNormalizedMetadata(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = pricingSyncHTTPTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://models.dev/api.json" {
			t.Fatalf("unexpected source URL: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"anthropic":{"name":"Anthropic","models":{"claude-sonnet":{"cost":{"input":3,"output":15}}}}}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	db := openAPITestDatabase(t)
	if err := db.Create(&entities.UsageEvent{EventKey: "used-claude", Model: "claude-sonnet"}).Error; err != nil {
		t.Fatal(err)
	}
	provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
	router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, newPricingRequest(http.MethodGet, "/api/v1/pricing/sync/fetch?source=models-dev", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("fetch pricing status %d: %s", response.Code, response.Body.String())
	}
	var fetched servicedto.PricingSyncFetchResponse
	if err := json.Unmarshal(response.Body.Bytes(), &fetched); err != nil || fetched.Source != "models-dev" || len(fetched.Matches) != 1 || fetched.Matches[0].Provider != "Anthropic" || fetched.Matches[0].BasePrices.CacheRead != 0 || fetched.Matches[0].BasePrices.CacheWrite != 0 {
		t.Fatalf("fetch contract %+v, err %v", fetched, err)
	}
}

func TestPricingSyncApplyHTTPPreservesExistingConfigurationAndStoredEvent(t *testing.T) {
	db := openAPITestDatabase(t)
	provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
	threshold := int64(100)
	initial := pricing.ModelPricingConfig{Model: "claude-sonnet", PricingStyle: "claude", BasePrices: pricing.BasePrices{Input: 1, Output: 2}, ModelMultiplier: 0.5,
		ConditionalMultipliers: []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}},
		Branches:               []pricing.PriceBranch{{ID: "large", Name: "Large", Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold}, Period: pricing.PeriodCondition{Type: pricing.PeriodAll}, Prices: pricing.BasePrices{Input: 7}}}}
	if _, err := provider.SavePricingModel(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	cost, available := 4.25, true
	if err := db.Create(&entities.UsageEvent{EventKey: "old-claude-cost", Model: "claude-sonnet", CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
		t.Fatal(err)
	}
	router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, newPricingRequest(http.MethodPost, "/api/v1/pricing/sync/apply", `{"source":"litellm","items":[{"model":"claude-sonnet","base_prices":{"input":3,"output":4,"cache_read":0,"cache_write":0},"pricing_style":"openai"},{"model":"openai/new","base_prices":{"input":5,"output":6,"cache_read":0,"cache_write":0},"pricing_style":"openai"}]}`))
	if response.Code != http.StatusOK {
		t.Fatalf("apply pricing status %d: %s", response.Code, response.Body.String())
	}
	var applied servicedto.PricingSyncApplyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &applied); err != nil || applied.ConfigRevision != 2 || len(applied.Models) != 2 {
		t.Fatalf("apply contract %+v, err %v", applied, err)
	}
	var old, fresh *pricing.ModelPricingConfig
	for index := range applied.Models {
		if applied.Models[index].Model == "claude-sonnet" {
			old = &applied.Models[index]
		}
		if applied.Models[index].Model == "openai/new" {
			fresh = &applied.Models[index]
		}
	}
	if old == nil || old.PricingStyle != "claude" || old.ModelMultiplier != 0.5 || old.BasePrices.Input != 3 || len(old.ConditionalMultipliers) != 1 || len(old.Branches) != 1 || fresh == nil || fresh.PricingStyle != "openai" || fresh.ModelMultiplier != 1 || len(fresh.ConditionalMultipliers) != 0 || len(fresh.Branches) != 0 {
		t.Fatalf("sync changed complete configuration: %+v", applied.Models)
	}
	var event entities.UsageEvent
	if err := db.Where("event_key = ?", "old-claude-cost").Take(&event).Error; err != nil || event.CostUSD == nil || *event.CostUSD != cost || event.CostAvailable == nil || !*event.CostAvailable {
		t.Fatalf("sync changed stored event fee: %+v err=%v", event, err)
	}
}

func TestPricingSyncApplyHTTPRejectsInvalidItemsWithoutRevision(t *testing.T) {
	base := `{"source":"models-dev","items":[{"model":"model-a","base_prices":{"input":1,"output":2,"cache_read":0,"cache_write":0},"pricing_style":"openai"}]}`
	for _, testCase := range []struct {
		name, body, code, path string
	}{
		{"source missing", strings.Replace(base, `"source":"models-dev",`, "", 1), "invalid_request", "source"},
		{"source unknown", strings.Replace(base, `"models-dev"`, `"other"`, 1), "invalid_request", "source"},
		{"items missing", `{"source":"models-dev"}`, "invalid_pricing", "items"},
		{"items empty", `{"source":"models-dev","items":[]}`, "invalid_pricing", "items"},
		{"duplicate model", strings.Replace(base, `}]}`, `},{"model":"model-a","base_prices":{"input":3,"output":4,"cache_read":0,"cache_write":0},"pricing_style":"openai"}]}`, 1), "invalid_pricing", "items[1].model"},
		{"missing cache write", strings.Replace(base, `,"cache_write":0`, "", 1), "invalid_pricing", "items[0].base_prices.cache_write"},
		{"negative input", strings.Replace(base, `"input":1`, `"input":-1`, 1), "invalid_pricing", "items[0].base_prices.input"},
		{"unknown item field", strings.Replace(base, `"pricing_style":"openai"`, `"pricing_style":"openai","extra":1`, 1), "invalid_request", ""},
		{"trailing JSON", base + ` true`, "invalid_request", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openAPITestDatabase(t)
			provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
			router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, newPricingRequest(http.MethodPost, "/api/v1/pricing/sync/apply", testCase.body))
			var failure servicedto.PricingErrorResponse
			if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != testCase.code {
				t.Fatalf("invalid apply status %d: %s", response.Code, response.Body.String())
			}
			if testCase.path != "" && (len(failure.Fields) == 0 || failure.Fields[0].Path != testCase.path) {
				t.Fatalf("expected path %s: %+v", testCase.path, failure.Fields)
			}
			listed, err := provider.ListPricingModels(context.Background())
			if err != nil || listed.ConfigRevision != 0 || len(listed.Models) != 0 {
				t.Fatalf("invalid apply wrote configuration: %+v err=%v", listed, err)
			}
		})
	}
}

func TestPricingSyncApplyHTTPValidatesAgainstCurrentMultiplierAndRollsBackBatch(t *testing.T) {
	db := openAPITestDatabase(t)
	provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
	for _, config := range []pricing.ModelPricingConfig{
		{Model: "free-model", PricingStyle: "openai", BasePrices: pricing.BasePrices{Input: 1}, ModelMultiplier: 0, ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{}},
		{Model: "paid-model", PricingStyle: "openai", BasePrices: pricing.BasePrices{Input: 1}, ModelMultiplier: 2, ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{}},
	} {
		if _, err := provider.SavePricingModel(context.Background(), config); err != nil {
			t.Fatal(err)
		}
	}
	router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
	// 倍率零的已存模型允许有限的大单价；按新模型倍率一预检会错误拒绝。
	free := httptest.NewRecorder()
	router.ServeHTTP(free, newPricingRequest(http.MethodPost, "/api/v1/pricing/sync/apply", `{"source":"litellm","items":[{"model":"free-model","base_prices":{"input":1e300,"output":0,"cache_read":0,"cache_write":0},"pricing_style":"claude"}]}`))
	if free.Code != http.StatusOK {
		t.Fatalf("zero multiplier large price status %d: %s", free.Code, free.Body.String())
	}
	before, err := provider.ListPricingModels(context.Background())
	if err != nil || before.ConfigRevision != 3 {
		t.Fatalf("unexpected pre-failure revision: %+v err=%v", before, err)
	}
	// 第二项与当前倍率连乘溢出时，第一项写入、快照发布和修订必须一起回滚。
	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, newPricingRequest(http.MethodPost, "/api/v1/pricing/sync/apply", `{"source":"litellm","items":[{"model":"free-model","base_prices":{"input":5,"output":0,"cache_read":0,"cache_write":0},"pricing_style":"openai"},{"model":"paid-model","base_prices":{"input":1e295,"output":0,"cache_read":0,"cache_write":0},"pricing_style":"openai"}]}`))
	var failure servicedto.PricingErrorResponse
	if failed.Code != http.StatusBadRequest || json.Unmarshal(failed.Body.Bytes(), &failure) != nil || failure.Code != "invalid_pricing" || len(failure.Fields) == 0 || failure.Fields[0].Path != "items[1].base_prices" {
		t.Fatalf("invalid combined price status %d: %s", failed.Code, failed.Body.String())
	}
	after, err := provider.ListPricingModels(context.Background())
	if err != nil || after.ConfigRevision != before.ConfigRevision || len(after.Models) != 2 {
		t.Fatalf("failed batch changed revision/models: %+v err=%v", after, err)
	}
	for _, model := range after.Models {
		if model.Model == "free-model" && (model.BasePrices.Input != 1e300 || model.ModelMultiplier != 0) {
			t.Fatalf("failed batch changed free model: %+v", model)
		}
		if model.Model == "paid-model" && model.BasePrices.Input != 1 {
			t.Fatalf("failed batch changed paid model: %+v", model)
		}
	}
	var stored []entities.ModelPriceSetting
	if err := db.Order("model asc").Find(&stored).Error; err != nil || len(stored) != 2 || stored[0].Model != "free-model" || stored[0].PromptPricePer1M != 1e300 || stored[1].Model != "paid-model" || stored[1].PromptPricePer1M != 1 {
		t.Fatalf("failed batch changed physical prices: %+v err=%v", stored, err)
	}
}

func TestPricingSyncFetchHTTPMapsTimeoutCancellationAndInternalFailure(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, "price_source_timeout"},
		{"canceled source", context.Canceled, http.StatusInternalServerError, "internal_error"},
		{"internal failure", errors.New("SELECT secret FROM pricing_source"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			oldTransport := http.DefaultTransport
			http.DefaultTransport = pricingSyncHTTPTransport(func(*http.Request) (*http.Response, error) { return nil, testCase.err })
			t.Cleanup(func() { http.DefaultTransport = oldTransport })
			db := openAPITestDatabase(t)
			if err := db.Create(&entities.UsageEvent{EventKey: "used-model", Model: "model-a"}).Error; err != nil {
				t.Fatal(err)
			}
			provider := newAPIPricingProvider(t, db, pricing.NewCatalog(pricing.EmptySnapshot()))
			router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, newPricingRequest(http.MethodGet, "/api/v1/pricing/sync/fetch?source=models-dev", ""))
			var failure servicedto.PricingErrorResponse
			if response.Code != testCase.status || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != testCase.code || strings.Contains(response.Body.String(), "SELECT") {
				t.Fatalf("source failure status %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPricingSyncApplyHTTPDoesNotCallDatabaseDeadlineSourceTimeout(t *testing.T) {
	router := keeperapi.NewRouter(nil, nil, nil, &pricingStub{err: context.DeadlineExceeded}, keeperapi.AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, newPricingRequest(http.MethodPost, "/api/v1/pricing/sync/apply", `{"source":"litellm","items":[{"model":"model-a","base_prices":{"input":1,"output":0,"cache_read":0,"cache_write":0},"pricing_style":"openai"}]}`))
	var failure servicedto.PricingErrorResponse
	if response.Code != http.StatusInternalServerError || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != "internal_error" {
		t.Fatalf("database deadline mislabeled as source timeout: %d %s", response.Code, response.Body.String())
	}
}

func TestPricingSyncNewRoutesKeepAdminBoundaryAndRequiredSource(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatal(err)
	}
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(nil, nil, nil, &pricingStub{}, config, keeperapi.NewAuthHandler(config, sessions), "")
	for _, route := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/v1/pricing/sync/fetch?source=models-dev", ""},
		{http.MethodPost, "/api/v1/pricing/sync/apply", `{"source":"models-dev","items":[{"model":"model-a","base_prices":{"input":1,"output":0,"cache_read":0,"cache_write":0},"pricing_style":"openai"}]}`},
	} {
		anonymous := httptest.NewRecorder()
		router.ServeHTTP(anonymous, newPricingRequest(route.method, route.target, route.body))
		if anonymous.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous sync %s: %d", route.target, anonymous.Code)
		}
		viewerRequest := newPricingRequest(route.method, route.target, route.body)
		viewerRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: viewerToken})
		viewer := httptest.NewRecorder()
		router.ServeHTTP(viewer, viewerRequest)
		if viewer.Code != http.StatusForbidden {
			t.Fatalf("viewer sync %s: %d", route.target, viewer.Code)
		}
	}
	openRouter := keeperapi.NewRouter(nil, nil, nil, &pricingStub{}, keeperapi.AuthConfig{}, nil, "")
	for _, target := range []string{"/api/v1/pricing/sync/fetch", "/api/v1/pricing/sync/fetch?source=unknown"} {
		response := httptest.NewRecorder()
		openRouter.ServeHTTP(response, newPricingRequest(http.MethodGet, target, ""))
		var failure servicedto.PricingErrorResponse
		if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != "invalid_request" || len(failure.Fields) != 1 || failure.Fields[0].Path != "source" {
			t.Fatalf("invalid fetch source %s: %d %s", target, response.Code, response.Body.String())
		}
	}
}
