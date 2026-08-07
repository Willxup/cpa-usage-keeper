package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type pluginAnalysisResponse struct {
	APIKeyComposition []struct {
		Label        string `json:"label"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
		TotalTokens  int64  `json:"total_tokens"`
	} `json:"api_key_composition"`
}

func TestAdministrativeAnalysisShowsHistoricalPluginUsageAfterMetadataSync(t *testing.T) {
	db := openPluginAnalysisDatabase(t)
	currentHour := time.Now().In(time.Local).Truncate(time.Hour)
	eventTime := currentHour.Add(-4*time.Hour + 15*time.Minute)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "marketplace-history", APIGroupKey: "marketplace1", Model: "gpt-5.6", Timestamp: eventTime, InputTokens: 9925, OutputTokens: 10, TotalTokens: 9935},
		{EventKey: "dispatcher-history", APIGroupKey: "task-dispatcher", Model: "gpt-5.6", Timestamp: eventTime.Add(time.Minute), InputTokens: 100, OutputTokens: 20, TotalTokens: 120},
		{EventKey: "native-history", APIGroupKey: "sk-native123456", Model: "gpt-5.6", Timestamp: eventTime.Add(2 * time.Minute), InputTokens: 7, OutputTokens: 3, TotalTokens: 10},
	}); err != nil {
		t.Fatalf("seed historical usage: %v", err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, eventTime.Add(time.Hour)); err != nil {
		t.Fatalf("aggregate historical usage: %v", err)
	}

	router := NewRouter(nil, nil, service.NewUsageService(db, emptyPricingCatalogForTest()), nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})
	path := "/api/v1/usage/analysis?range=custom&unit=hour&start=" + url.QueryEscape(eventTime.Truncate(time.Hour).Format(time.RFC3339)) + "&end=" + url.QueryEscape(currentHour.Format(time.RFC3339))
	if before := requestPluginAnalysis(t, router, path); len(before.APIKeyComposition) != 0 {
		t.Fatalf("usage must remain hidden before metadata exists: %+v", before.APIKeyComposition)
	}

	if err := repository.SyncCPAAPIKeySnapshots(db, []repository.CPAAPIKeySourceSnapshot{
		{Source: entities.CPAAPIKeySourceNative, Keys: []repository.CPAAPIKeyMetadata{{APIKey: "sk-native123456"}}},
		{Source: entities.CPAAPIKeySourceCPAKeyPolicy, Keys: []repository.CPAAPIKeyMetadata{{APIKey: "marketplace1", DisplayKey: "cpa_mar...ce1", KeyAlias: "белослудцев"}, {APIKey: "task-dispatcher", DisplayKey: "cpa_tas...her", KeyAlias: "Task Dispatcher"}}},
	}, eventTime.Add(90*time.Minute)); err != nil {
		t.Fatalf("sync API key metadata: %v", err)
	}

	after := requestPluginAnalysis(t, router, path)
	byLabel := make(map[string]struct{ input, output, total int64 }, len(after.APIKeyComposition))
	for _, item := range after.APIKeyComposition {
		byLabel[item.Label] = struct{ input, output, total int64 }{item.InputTokens, item.OutputTokens, item.TotalTokens}
	}
	if got := byLabel["белослудцев"]; got.input != 9925 || got.output != 10 || got.total != 9935 {
		t.Fatalf("marketplace1 historical usage = %+v", got)
	}
	if got := byLabel["Task Dispatcher"]; got.input != 100 || got.output != 20 || got.total != 120 {
		t.Fatalf("task-dispatcher historical usage = %+v", got)
	}
	if got := byLabel["sk-*********123456"]; got.total != 10 {
		t.Fatalf("native usage changed: %+v", got)
	}
}

func requestPluginAnalysis(t *testing.T, router http.Handler, path string) pluginAnalysisResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("analysis status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload pluginAnalysisResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode analysis: %v", err)
	}
	return payload
}

func openPluginAnalysisDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "plugin-analysis.db")})
	if err != nil {
		t.Fatalf("open plugin analysis database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load plugin analysis database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
