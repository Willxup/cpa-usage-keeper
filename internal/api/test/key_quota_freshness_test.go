package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestKeyQuotaMissingCaptureAndElapsedRelativeReset(t *testing.T) {
	for _, withCapture := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing capture", true: "elapsed relative reset"}[withCapture], func(t *testing.T) {
			var capture *time.Time
			if withCapture {
				at := time.Now().Add(-time.Minute)
				capture = &at
			}
			delay := int64(10)
			used := 25.0
			provider := &quotaProviderStub{cacheResponse: quota.CacheResponse{Items: []quota.CachedQuotaItem{{
				AuthIndex: "fixture-index", Status: quota.RefreshTaskStatusCompleted, RefreshedAt: capture,
				Quota: &quota.CheckResponse{Quota: []quota.QuotaRow{{Label: "Weekly", UsedPercent: &used, ResetAfterSeconds: &delay, Source: "private arbitrary source"}}},
			}}}}
			identities := identityMetadataStub{items: []entities.UsageIdentity{{ID: 1, Identity: "fixture-index", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile}}}
			sessions := auth.NewSessionManager(time.Hour)
			token, _, _ := sessions.CreateAPIKeyViewer(42)
			config := AuthConfig{Enabled: true, APIKeyViewerQuotaEnabled: true}
			router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{Quota: provider, UsageIdentity: identities, CPAAPIKeys: &keyViewerRankingKeyStub{row: entities.CPAAPIKey{ID: 42}}})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, viewerRankingRequest(http.MethodGet, "/api/v1/key-quota", token))
			if response.Code != http.StatusOK {
				t.Fatal(response.Code, response.Body.String())
			}
			var data struct {
				Accounts []struct {
					Status string
					Rows   []struct {
						CapturedAt *time.Time `json:"captured_at"`
						ResetAt    string     `json:"reset_at"`
						Source     string     `json:"source"`
						Stale      bool       `json:"stale"`
					}
				}
			}
			if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if len(data.Accounts) != 1 || len(data.Accounts[0].Rows) != 1 {
				t.Fatal(response.Body.String())
			}
			row := data.Accounts[0].Rows[0]
			if !row.Stale || data.Accounts[0].Status != "stale" || row.Source != "" || strings.Contains(response.Body.String(), "private arbitrary source") {
				t.Fatal(response.Body.String())
			}
			if !withCapture {
				if row.CapturedAt != nil || row.ResetAt != "" || strings.Contains(response.Body.String(), "0001-") {
					t.Fatal(response.Body.String())
				}
			} else {
				if row.CapturedAt == nil || !row.CapturedAt.Equal(*capture) || row.ResetAt != capture.Add(10*time.Second).UTC().Format(time.RFC3339) {
					t.Fatal(response.Body.String())
				}
			}
		})
	}
}
