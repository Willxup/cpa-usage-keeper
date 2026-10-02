package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
)

// 两个 Viewer 入口经真实 service 读取同一 Key 的已存费用，客户端 Key 参数与现价不能扩大或重算结果。
func TestKeyViewerOverviewAndAnalysisReadOnlySessionKeyStoredCosts(t *testing.T) {
	db := openAPITestDatabase(t)
	keys := []entities.CPAAPIKey{
		{ID: 42, APIKey: "sk-viewer", DisplayKey: "Viewer Key"},
		{ID: 99, APIKey: "sk-other", DisplayKey: "Other Key"},
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatalf("seed Keys: %v", err)
	}
	bucket := timeutil.NormalizeStorageTime(time.Now()).Truncate(time.Hour).Add(-2 * time.Hour)
	viewerCost, otherCost, unavailable := 2.5, 99.0, int64(0)
	rows := []entities.UsageOverviewHourlyStat{
		{BucketStart: bucket, APIGroupKey: "sk-viewer", Model: "model-a", RequestCount: 2, SuccessCount: 2, InputTokens: 100, TotalTokens: 100, CostUSD: &viewerCost, UnavailableCostCount: &unavailable},
		{BucketStart: bucket, APIGroupKey: "sk-other", Model: "model-a", RequestCount: 3, SuccessCount: 3, InputTokens: 999, TotalTokens: 999, CostUSD: &otherCost, UnavailableCostCount: &unavailable},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed stored overview rows: %v", err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 9}); err != nil {
		t.Fatalf("seed different current price: %v", err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("load current price: %v", err)
	}
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create Viewer session: %v", err)
	}
	config := api.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := api.NewRouter(nil, nil, service.NewUsageService(db, pricing.NewCatalog(snapshot)), nil, config, api.NewAuthHandler(config, sessions), "", api.OptionalProviders{CPAAPIKeys: &keyViewerAnalysisKeyStub{row: keys[0]}})
	cookie := &http.Cookie{Name: standardSessionCookieName, Value: token}

	overview := serveAPIGet(router, "/api/v1/key-overview?range=24h&api_key_id=99", cookie)
	if overview.Code != http.StatusOK {
		t.Fatalf("Viewer overview status=%d body=%s", overview.Code, overview.Body.String())
	}
	var overviewPayload struct {
		Usage struct {
			TotalRequests int64 `json:"total_requests"`
			TotalTokens   int64 `json:"total_tokens"`
		} `json:"usage"`
		Summary struct {
			TotalCost     float64 `json:"total_cost"`
			CostAvailable bool    `json:"cost_available"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(overview.Body.Bytes(), &overviewPayload); err != nil {
		t.Fatalf("decode Viewer overview: %v", err)
	}
	if overviewPayload.Usage.TotalRequests != 2 || overviewPayload.Usage.TotalTokens != 100 || !overviewAPICostClose(overviewPayload.Summary.TotalCost, viewerCost) || !overviewPayload.Summary.CostAvailable {
		t.Fatalf("Viewer overview leaked another Key or repriced stored fee: %+v", overviewPayload)
	}

	analysis := serveAPIGet(router, "/api/v1/key-analysis?range=24h&api_key_id=99", cookie)
	if analysis.Code != http.StatusOK {
		t.Fatalf("Viewer Analysis status=%d body=%s", analysis.Code, analysis.Body.String())
	}
	var analysisPayload struct {
		CostSummary struct {
			TotalCostUSD  float64 `json:"total_cost_usd"`
			CostAvailable bool    `json:"cost_available"`
		} `json:"cost_summary"`
		TokenUsage []struct {
			TotalTokens int64   `json:"total_tokens"`
			CostUSD     float64 `json:"cost_usd"`
		} `json:"token_usage"`
		APIKeyComposition []struct {
			Key     string  `json:"key"`
			CostUSD float64 `json:"cost_usd"`
		} `json:"api_key_composition"`
		AuthFilesComposition  []json.RawMessage `json:"auth_files_composition"`
		AIProviderComposition []json.RawMessage `json:"ai_provider_composition"`
	}
	if err := json.Unmarshal(analysis.Body.Bytes(), &analysisPayload); err != nil {
		t.Fatalf("decode Viewer Analysis: %v", err)
	}
	if !analysisPayload.CostSummary.CostAvailable || !overviewAPICostClose(analysisPayload.CostSummary.TotalCostUSD, viewerCost) ||
		len(analysisPayload.TokenUsage) != 1 || analysisPayload.TokenUsage[0].TotalTokens != 100 || !overviewAPICostClose(analysisPayload.TokenUsage[0].CostUSD, viewerCost) ||
		len(analysisPayload.APIKeyComposition) != 1 || analysisPayload.APIKeyComposition[0].Key != "42" || !overviewAPICostClose(analysisPayload.APIKeyComposition[0].CostUSD, viewerCost) ||
		len(analysisPayload.AuthFilesComposition) != 0 || len(analysisPayload.AIProviderComposition) != 0 {
		t.Fatalf("Viewer Analysis leaked another Key or source identity: %+v", analysisPayload)
	}
}
