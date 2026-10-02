package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
)

func TestOverviewComparisonAPIUsesAliasesAndViewerScope(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "comparisons.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	key := entities.CPAAPIKey{ID: 42, APIKey: "sk-viewer123456", KeyAlias: "Viewer Key"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	sharedLabel := "Shared Display"
	identities := []entities.UsageIdentity{
		{Name: "Auth Source", Alias: &sharedLabel, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "auth-one", Type: "codex"},
		{Name: "Provider Source", Alias: &sharedLabel, AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-one", Type: "codex"},
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatal(err)
	}
	feeAuth, feeProvider, feeOther, feeLegacyOne, feeLegacyTwo := 2.5, 3.75, 4.25, 1.5, 1.75
	available, unavailable := int64(0), int64(1)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: today, APIGroupKey: key.APIKey, Model: "my-model", ModelAlias: "display-model", AuthIndex: "auth-one", RequestCount: 1, SuccessCount: 1, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &feeAuth, UnavailableCostCount: &available},
		{BucketStart: today, APIGroupKey: key.APIKey, Model: "my-model", AuthIndex: "provider-one", RequestCount: 1, SuccessCount: 1, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &feeProvider, UnavailableCostCount: &available},
		{BucketStart: today, APIGroupKey: "sk-other654321", Model: "other-model", AuthIndex: "missing-identity", RequestCount: 5, SuccessCount: 5, TotalTokens: 100, CostUSD: &feeOther, UnavailableCostCount: &unavailable},
		{BucketStart: today, APIGroupKey: "sk-legacy-one-123456", Model: "legacy-model", RequestCount: 1, SuccessCount: 1, CostUSD: &feeLegacyOne, UnavailableCostCount: &available},
		{BucketStart: today, APIGroupKey: "sk-legacy-two-123456", Model: "legacy-model", RequestCount: 1, SuccessCount: 1, CostUSD: &feeLegacyTwo, UnavailableCostCount: &available},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "my-model", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := pricing.NewCatalog(snapshot)
	provider := service.NewUsageService(db, catalog)
	keys := &keyViewerAnalysisKeyStub{row: key}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keys})
	query := "?range=custom&unit=day&start=" + today.Format(time.DateOnly) + "&end=" + today.Format(time.DateOnly)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/comparisons"+query, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("admin status %d: %s", response.Code, response.Body.String())
	}
	type comparisonItem struct {
		Key, Label  string
		Requests    int64
		Cost        *float64
		TokenSeries []int64 `json:"token_series"`
	}
	type comparisonPayload struct {
		Buckets     []string
		Granularity string
		Timezone    string
		Models      []comparisonItem
		APIKeys     []comparisonItem `json:"api_keys"`
		AuthFiles   []comparisonItem `json:"auth_files"`
		AIProviders []comparisonItem `json:"ai_providers"`
	}
	var payload comparisonPayload
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.APIKeys) != 4 {
		t.Fatalf("key count: %d", len(payload.APIKeys))
	}
	if len(payload.Buckets) != 1 || payload.Buckets[0] != today.Format(time.DateOnly) || payload.Granularity != "daily" || payload.Timezone != time.Local.String() {
		t.Fatalf("incorrect comparison time axis: %+v", payload)
	}
	for _, items := range [][]comparisonItem{payload.Models, payload.APIKeys, payload.AuthFiles, payload.AIProviders} {
		for _, item := range items {
			if len(item.TokenSeries) != 1 {
				t.Fatalf("missing token timeline: %+v", item)
			}
		}
	}
	if len(payload.AuthFiles) != 1 || payload.AuthFiles[0].TokenSeries[0] != 1_000_000 || len(payload.AIProviders) != 1 || payload.AIProviders[0].TokenSeries[0] != 1_000_000 {
		t.Fatal("credential timeline did not preserve identity grouping")
	}
	seen := map[string]bool{}
	foundAlias := false
	foundLegacyOne, foundLegacyTwo, foundUnavailable := false, false, false
	for _, item := range payload.APIKeys {
		if seen[item.Key] {
			t.Fatal("history key identifiers collided")
		}
		seen[item.Key] = true
		if item.Key == "42" && item.Label == "Viewer Key" {
			foundAlias = true
		}
		if item.Key == "99" {
			if item.Cost != nil {
				t.Fatalf("unavailable active Key fee should be null: %+v", item)
			}
			foundUnavailable = true
		} else if item.Key != "42" {
			if !strings.HasPrefix(item.Key, "legacy:") {
				t.Fatalf("deleted Key lacks stable legacy identifier: %+v", item)
			}
			if item.Cost == nil {
				t.Fatalf("available deleted Key fee should be present: %+v", item)
			}
			switch {
			case overviewAPICostClose(*item.Cost, feeLegacyOne):
				foundLegacyOne = true
			case overviewAPICostClose(*item.Cost, feeLegacyTwo):
				foundLegacyTwo = true
			default:
				t.Fatalf("unexpected historical Key fee: %+v", item)
			}
		}
	}
	if !foundAlias || !foundLegacyOne || !foundLegacyTwo || !foundUnavailable {
		t.Fatalf("Key alias, deleted history or unavailable fee missing: %+v", payload.APIKeys)
	}
	byKey := func(items []comparisonItem) map[string]comparisonItem {
		result := make(map[string]comparisonItem, len(items))
		for _, item := range items {
			result[item.Key] = item
		}
		return result
	}
	models, apiKeys := byKey(payload.Models), byKey(payload.APIKeys)
	if len(models) != 3 || models["display-model"].Key != "" || models["my-model"].Requests != 2 || models["my-model"].Cost == nil || !overviewAPICostClose(*models["my-model"].Cost, 6.25) ||
		models["other-model"].Cost != nil || models["legacy-model"].Cost == nil || !overviewAPICostClose(*models["legacy-model"].Cost, 3.25) {
		t.Fatalf("model comparison lost stored fee/availability: %+v", payload.Models)
	}
	if apiKeys["42"].Cost == nil || !overviewAPICostClose(*apiKeys["42"].Cost, 6.25) || apiKeys["42"].Label != "Viewer Key" {
		t.Fatalf("API Key comparison lost stored fee/alias: %+v", payload.APIKeys)
	}
	if len(payload.AuthFiles) != 1 || len(payload.AIProviders) != 1 || payload.AuthFiles[0].Key != "auth-one" || payload.AIProviders[0].Key != "provider-one" ||
		payload.AuthFiles[0].Label != sharedLabel || payload.AIProviders[0].Label != sharedLabel || payload.AuthFiles[0].Cost == nil || payload.AIProviders[0].Cost == nil ||
		!overviewAPICostClose(*payload.AuthFiles[0].Cost, feeAuth) || !overviewAPICostClose(*payload.AIProviders[0].Cost, feeProvider) {
		t.Fatalf("typed identities with same display name merged: auth=%+v provider=%+v", payload.AuthFiles, payload.AIProviders)
	}
	for _, raw := range []string{key.APIKey, "sk-other654321", "sk-legacy-one-123456", "sk-legacy-two-123456"} {
		if strings.Contains(response.Body.String(), raw) {
			t.Fatal("raw API key leaked")
		}
	}
	keys.listCalls = 0
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatal(err)
	}
	authConfig := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	viewerRouter := NewRouter(nil, nil, provider, nil, authConfig, NewAuthHandler(authConfig, sessions), "", OptionalProviders{CPAAPIKeys: keys})
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview/comparisons"+query+"&api_key_id=99", nil)
	request.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	viewerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("viewer status %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "api_keys") || strings.Contains(response.Body.String(), "other-model") || strings.Contains(response.Body.String(), key.APIKey) {
		t.Fatal("viewer received data outside API Key scope")
	}
	if !strings.Contains(response.Body.String(), `"key":"42"`) || keys.listCalls != 0 {
		t.Fatal("viewer should receive its own API Key data without listing keys")
	}
	payload = comparisonPayload{}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	viewerModels := byKey(payload.Models)
	if len(viewerModels) != 1 || viewerModels["my-model"].Cost == nil || !overviewAPICostClose(*viewerModels["my-model"].Cost, 6.25) || len(payload.AuthFiles) != 0 || len(payload.AIProviders) != 0 {
		t.Fatalf("viewer scope or stored fee changed: %+v", payload)
	}
	for _, restricted := range []string{"auth_files", "ai_providers", "Auth Source", "Provider Source", "auth-one", "provider-one"} {
		if strings.Contains(response.Body.String(), restricted) {
			t.Fatalf("viewer received restricted dimension %s", restricted)
		}
	}
	if !strings.Contains(response.Body.String(), `"token_series":[2000000]`) {
		t.Fatal("viewer timeline is missing its own usage")
	}
	// 报价修改和删除都不得改变已经持久化的比较金额。
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "my-model", PromptPricePer1M: 90}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Replace(snapshot)
	for _, mutate := range []bool{false, true} {
		if mutate {
			if err := repository.DeleteModelPriceSettingRequired(db, "my-model"); err != nil {
				t.Fatal(err)
			}
			snapshot, err = repository.LoadPricingSnapshot(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			catalog.Replace(snapshot)
		}
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/comparisons"+query, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("comparison after pricing mutation status %d: %s", response.Code, response.Body.String())
		}
		payload = comparisonPayload{}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		item := byKey(payload.Models)["my-model"]
		if item.Cost == nil || !overviewAPICostClose(*item.Cost, 6.25) {
			t.Fatalf("pricing mutation repriced comparison: %+v", item)
		}
	}
}
