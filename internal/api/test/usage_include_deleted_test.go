package test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
)

// 经真实 service 同时覆盖小时／日投影、历史 ID、脱敏标签和 Comparisons 的共享标识。
func TestUsageAnalysisIncludeDeleted(t *testing.T) {
	for _, period := range []string{"24h", "7d"} {
		t.Run(period, func(t *testing.T) {
			db := openAPITestDatabase(t)
			keys := []entities.CPAAPIKey{
				{ID: 41, APIKey: "test-active-123456", KeyAlias: "Active"},
				{ID: 42, APIKey: "test-deleted-654321", KeyAlias: "Deleted", IsDeleted: true},
			}
			if err := db.Create(&keys).Error; err != nil {
				t.Fatal(err)
			}
			legacy := "test-unregistered-123456"
			bucket := timeutil.NormalizeStorageTime(time.Now()).Truncate(time.Hour).Add(-2 * time.Hour)
			day := time.Date(bucket.Year(), bucket.Month(), bucket.Day(), 0, 0, 0, 0, time.Local)
			cost, unavailable := 1.0, int64(0)
			for _, key := range []string{keys[0].APIKey, keys[1].APIKey, legacy} {
				if err := db.Create(&entities.UsageOverviewHourlyStat{BucketStart: bucket, APIGroupKey: key, Model: "model-a", RequestCount: 1, TotalTokens: 10, CostUSD: &cost, UnavailableCostCount: &unavailable}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: day, APIGroupKey: key, Model: "model-a", RequestCount: 1, TotalTokens: 10, CostUSD: &cost, UnavailableCostCount: &unavailable}).Error; err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			router := keeperapi.NewRouter(nil, nil, service.NewUsageService(db, pricing.NewCatalog(snapshot)), nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})
			query := "?range=custom&unit=hour&start=" + url.QueryEscape(bucket.Add(-3*time.Hour).Format(time.RFC3339)) + "&end=" + url.QueryEscape(bucket.Add(time.Hour).Format(time.RFC3339))
			if period == "7d" {
				query = "?range=custom&unit=day&start=" + day.Format(time.DateOnly) + "&end=" + day.Format(time.DateOnly)
			}
			path := "/api/v1/usage/analysis" + query
			baseline := serveAPIGet(router, path)
			if baseline.Code != http.StatusOK {
				t.Fatalf("default status=%d", baseline.Code)
			}
			for _, value := range []string{"false", "invalid"} {
				response := serveAPIGet(router, path+"&include_deleted="+value)
				if response.Code != http.StatusOK || response.Body.String() != baseline.Body.String() {
					t.Fatal("non-opt-in response changed")
				}
			}
			type item struct{ Key, Label string }
			type payload struct {
				IncludeDeleted *bool  `json:"include_deleted"`
				APIKeys        []item `json:"api_key_composition"`
				Heatmap        struct {
					APIKeys []string          `json:"api_keys"`
					Labels  map[string]string `json:"api_key_labels"`
					Cells   []struct {
						APIKey string `json:"api_key"`
					} `json:"cells"`
				} `json:"heatmap"`
			}
			var original payload
			if err := json.Unmarshal(baseline.Body.Bytes(), &original); err != nil {
				t.Fatal(err)
			}
			if original.IncludeDeleted != nil || !reflect.DeepEqual(original.APIKeys, []item{{"41", "Active"}}) {
				t.Fatal("default analysis must contain only active keys and omit the echo")
			}
			response := serveAPIGet(router, path+"&include_deleted=true")
			if response.Code != http.StatusOK {
				t.Fatalf("opt-in status=%d", response.Code)
			}
			var got payload
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			legacyID := fmt.Sprintf("legacy:%x", sha256.Sum256([]byte(legacy)))
			want := map[string]string{"41": "Active", "42": "Deleted", legacyID: helper.RedactSensitiveValue(legacy)}
			if got.IncludeDeleted == nil || !*got.IncludeDeleted || len(got.APIKeys) != 3 || len(got.Heatmap.APIKeys) != 3 || len(got.Heatmap.Cells) != 3 || !reflect.DeepEqual(got.Heatmap.Labels, want) {
				t.Fatal("historical identities, labels or echo missing")
			}
			for _, row := range got.APIKeys {
				if want[row.Key] != row.Label || !slices.Contains(got.Heatmap.APIKeys, row.Key) {
					t.Fatal("composition and heatmap identities differ")
				}
			}
			for _, cell := range got.Heatmap.Cells {
				if _, ok := want[cell.APIKey]; !ok {
					t.Fatal("unknown heatmap identity")
				}
			}
			for _, raw := range []string{keys[0].APIKey, keys[1].APIKey, legacy} {
				if strings.Contains(response.Body.String(), raw) {
					t.Fatal("analysis disclosed an unmasked key")
				}
			}
			comparison := serveAPIGet(router, "/api/v1/usage/overview/comparisons?range="+period+"&include_deleted=true")
			var comparisons struct {
				APIKeys        []item `json:"api_keys"`
				IncludeDeleted *bool  `json:"include_deleted"`
			}
			if comparison.Code != http.StatusOK || json.Unmarshal(comparison.Body.Bytes(), &comparisons) != nil {
				t.Fatal("comparison request failed")
			}
			if comparisons.IncludeDeleted != nil {
				t.Fatal("comparisons must ignore the opt-in parameter")
			}
			if !slices.ContainsFunc(comparisons.APIKeys, func(row item) bool { return row.Key == legacyID && row.Label == want[legacyID] }) {
				t.Fatal("analysis and comparisons legacy identifiers differ")
			}
		})
	}
}

func TestCPAAPIKeySettingsIncludeDeleted(t *testing.T) {
	db := openAPITestDatabase(t)
	keys := []entities.CPAAPIKey{
		{ID: 41, APIKey: "test-active-123456"},
		{ID: 42, APIKey: "test-deleted-654321", IsDeleted: true},
		{ID: 43, APIKey: "test-plugin-123456", SourceType: entities.CPAAPIKeySourcePlugin, IsDeleted: true},
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatal(err)
	}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})
	path := "/api/v1/usage/api-keys/settings"
	baseline := serveAPIGet(router, path)
	if baseline.Code != http.StatusOK {
		t.Fatalf("default status=%d", baseline.Code)
	}
	for _, value := range []string{"false", "invalid"} {
		response := serveAPIGet(router, path+"?include_deleted="+value)
		if response.Code != http.StatusOK || response.Body.String() != baseline.Body.String() {
			t.Fatal("default settings response changed")
		}
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(baseline.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	if _, ok := original["includeDeleted"]; ok {
		t.Fatal("default settings echoed opt-in")
	}
	var originalRows []map[string]json.RawMessage
	if err := json.Unmarshal(original["items"], &originalRows); err != nil {
		t.Fatal(err)
	}
	if len(originalRows) != 1 {
		t.Fatal("default settings included deleted rows")
	}
	if _, ok := originalRows[0]["isDeleted"]; ok {
		t.Fatal("default settings added deletion state")
	}
	response := serveAPIGet(router, path+"?include_deleted=true")
	if response.Code != http.StatusOK {
		t.Fatalf("opt-in status=%d", response.Code)
	}
	var got struct {
		IncludeDeleted *bool `json:"includeDeleted"`
		Items          []struct {
			ID        string  `json:"id"`
			APIKey    *string `json:"apiKey"`
			IsDeleted *bool   `json:"isDeleted"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.IncludeDeleted == nil || !*got.IncludeDeleted || len(got.Items) != 3 {
		t.Fatal("settings history or echo missing")
	}
	for i, row := range got.Items {
		if row.ID != fmt.Sprint(keys[i].ID) || row.IsDeleted == nil || *row.IsDeleted != keys[i].IsDeleted {
			t.Fatal("incorrect settings identity or deletion state")
		}
		if i == 2 {
			if row.APIKey != nil {
				t.Fatal("plugin key disclosed raw value")
			}
		} else if row.APIKey == nil || *row.APIKey != keys[i].APIKey {
			t.Fatal("native key missing raw value")
		}
	}
}

func TestIncludeDeletedRoutesRequireAdmin(t *testing.T) {
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "test-password", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, nil, "")
	for _, path := range []string{"/api/v1/usage/analysis?range=24h&include_deleted=true", "/api/v1/usage/api-keys/settings?include_deleted=true"} {
		if response := serveAPIGet(router, path); response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated status=%d", response.Code)
		}
	}
}

func TestIncludeDeletedEchoRequiresHonoredRequest(t *testing.T) {
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "")
	for _, path := range []string{"/api/v1/usage/analysis?range=24h&include_deleted=true", "/api/v1/usage/api-keys/settings?include_deleted=true"} {
		response := serveAPIGet(router, path)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "include_deleted") || strings.Contains(response.Body.String(), "includeDeleted") {
			t.Fatal("unconfigured route echoed an unhonored request")
		}
	}
}

func TestIncludeDeletedUnsupportedProviderDoesNotEcho(t *testing.T) {
	provider := &analysisSplitStub{}
	for _, keys := range []service.CPAAPIKeyProvider{nil, usageAnalysisAPIKeyStub{}} {
		router := keeperapi.NewRouter(nil, nil, provider, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{CPAAPIKeys: keys})
		response := serveAPIGet(router, "/api/v1/usage/analysis?range=24h&include_deleted=true")
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "include_deleted") {
			t.Fatal("unsupported history provider must fail without a success echo")
		}
		if keys != nil {
			response = serveAPIGet(router, "/api/v1/usage/api-keys/settings?include_deleted=true")
			if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "includeDeleted") {
				t.Fatal("unsupported settings history provider must fail without a success echo")
			}
		}
	}
}
