package test

import (
	"context"
	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScopedAPIKeyOptionsServiceIntegration(t *testing.T) {
	db := openAPITestDatabase(t)
	at := time.Now().AddDate(0, 0, -1)
	date := at.Format("2006-01-02")
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-summary-123456", "sk-event-654321"}, at); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{APIGroupKey: "sk-summary-123456", BucketStart: time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.Local), RequestCount: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.UsageEvent{EventKey: "event", APIGroupKey: "sk-event-654321", Timestamp: at}).Error; err != nil {
		t.Fatal(err)
	}
	router := api.NewRouter(nil, nil, service.NewUsageService(db, emptyPricingCatalogForTest()), nil, api.AuthConfig{}, nil, "", api.OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})
	for _, tc := range []struct{ scope, id string }{{"overview", "1"}, {"analysis", "1"}, {"request-events", "2"}} {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/usage/api-keys/options?scope="+tc.scope+"&range=custom&unit=day&start="+date+"&end="+date, nil))
		if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"id":"`+tc.id+`"`) || strings.Count(resp.Body.String(), `"id"`) != 1 {
			t.Fatalf("%s status=%d body=%s", tc.scope, resp.Code, resp.Body.String())
		}
	}
}

type scopedKeyOptionsProvider struct {
	service.UsageProvider
	scope  string
	filter servicedto.UsageFilter
}

func (s *scopedKeyOptionsProvider) ListUsageAPIKeyOptions(_ context.Context, scope string, filter servicedto.UsageFilter) ([]entities.CPAAPIKey, error) {
	s.scope, s.filter = scope, filter
	return []entities.CPAAPIKey{{ID: 1, APIKey: "sk-should-not-leak-123456", KeyAlias: "Used key"}}, nil
}
func TestScopedAPIKeyOptionsParsePageRanges(t *testing.T) {
	p := &scopedKeyOptionsProvider{}
	now := time.Now()
	longRange := "&range=custom&unit=day&start=" + now.AddDate(0, 0, -180).Format("2006-01-02") + "&end=" + now.AddDate(0, 0, -1).Format("2006-01-02")
	router := api.NewRouter(nil, nil, p, nil, api.AuthConfig{}, nil, "")
	for _, tc := range []struct {
		query string
		code  int
		scope string
	}{
		{"scope=overview" + longRange, 200, "overview"},
		{"scope=analysis" + longRange, 200, "analysis"},
		{"scope=request-events" + longRange, 400, ""},
		{"scope=request-events&range=8h&api_key_id=bad&model=missing&source=bad&result=bad", 200, "request-events"},
		{"scope=realtime&window=30m&range=invalid&api_key_id=bad", 200, "realtime"},
		{"scope=realtime&window=1m", 400, ""},
		{"scope=invalid&range=8h", 400, ""},
		{"scope=overview&range=invalid", 400, ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			p.scope = ""
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/usage/api-keys/options?"+tc.query, nil))
			if resp.Code != tc.code {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			if resp.Code != 200 {
				return
			}
			if p.scope != tc.scope {
				t.Fatal(p.scope)
			}
			if p.filter.APIKeyID != "" || p.filter.Model != "" || p.filter.Source != "" || p.filter.Result != "" {
				t.Fatalf("unexpected dimensions %+v", p.filter)
			}
			if tc.scope == "realtime" && p.filter.RealtimeWindow != "30m" {
				t.Fatal(p.filter)
			}
			if tc.scope != "realtime" && (p.filter.StartTime == nil || p.filter.EndTime == nil) {
				t.Fatal("missing range")
			}
			if strings.Contains(resp.Body.String(), "should-not-leak") || !strings.Contains(resp.Body.String(), `"label":"Used key"`) {
				t.Fatal(resp.Body.String())
			}
			if !strings.Contains(resp.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("missing no-store")
			}
		})
	}
}
