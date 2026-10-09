package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestKimiProviderResolvesCredentialDomainAndKeepsProvider(t *testing.T) {
	for _, identityType := range []string{"kimi", "kimi-ai", "kimi.ai", "kimi.com"} {
		t.Run(identityType, func(t *testing.T) {
			for _, tc := range []struct {
				name, credential, host string
			}{
				{"international type", `{"type":"kimi-ai"}`, "api.kimi.ai"},
				{"provider fallback", `{}`, "api.kimi.ai"},
				{"explicit domestic domain", `{"type":"kimi-ai","domain":"kimi.com","base_url":"https://api.kimi.ai/coding"}`, "api.kimi.com"},
				{"explicit international domain", `{"type":"kimi","domain":" ai ","base_url":"https://api.kimi.com/coding"}`, "api.kimi.ai"},
				{"unknown explicit domain normalizes domestic", `{"domain":"unrecognized","type":"kimi-ai"}`, "api.kimi.com"},
				{"domestic base overrides type", `{"type":"kimi-ai","base_url":"https://api.kimi.com/coding"}`, "api.kimi.com"},
				{"base alias", `{"type":"kimi-ai","base-url":"https://api.kimi.com/coding"}`, "api.kimi.com"},
				{"canonical null base overrides alias", `{"type":"kimi-ai","base_url":null,"base-url":"https://api.kimi.com/coding"}`, "api.kimi.ai"},
				{"canonical empty base overrides alias", `{"type":"kimi-ai","base_url":"","base-url":"https://api.kimi.com/coding"}`, "api.kimi.ai"},
				{"credential type overrides provider", `{"type":"kimi"}`, "api.kimi.com"},
				{"arbitrary URL never receives token", `{"base_url":"https://evil.example/coding","type":"kimi-ai","access_token":"secret"}`, "api.kimi.ai"},
				{"lookalike URL never receives token", `{"base_url":"https://api.kimi.ai.evil.example/coding","type":"kimi-ai"}`, "api.kimi.ai"},
				{"unrecognized bare hostname", `{"base_url":"https://kimi/coding","type":"kimi-ai"}`, "api.kimi.ai"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var requests []apicall.Request
					var downloaded []string
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Header.Get("Authorization") != "Bearer management-key" {
							t.Errorf("missing CPA management authentication")
						}
						switch r.URL.Path {
						case "/v0/management/auth-files/download":
							downloaded = append(downloaded, r.URL.Query().Get("name"))
							_, _ = w.Write([]byte(tc.credential))
						case "/v0/management/api-call":
							var request apicall.Request
							if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
								t.Fatal(err)
							}
							requests = append(requests, request)
							_ = json.NewEncoder(w).Encode(quotaAPIResponse(200, `{"usage":{"limit":"100","remaining":"72","resetTime":"2026-10-12T00:00:00Z"},"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"100","used":"92","remaining":"8"}}]}`))
						default:
							t.Errorf("unexpected path %s", r.URL.Path)
						}
					}))
					defer server.Close()
					client := cpa.NewClient(server.URL, "management-key", time.Second, false)
					registry := quota.NewDefaultProviderRegistry(client, quota.DefaultProviderConfigs())
					provider, ok := registry.Provider(identityType)
					if !ok {
						t.Fatalf("Kimi type %q is unsupported", identityType)
					}
					output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
						Identity: "international-auth", Type: identityType, Provider: identityType,
						FileName: stringPtr(identityType + " + account.json"), FilePath: stringPtr("/data/auths/" + identityType + " + account.json"),
					}})
					if err != nil {
						t.Fatal(err)
					}
					if output.Provider != identityType || len(downloaded) != 1 || downloaded[0] != identityType+" + account.json" || len(requests) != 1 {
						t.Fatalf("unexpected provider/request routing: provider=%q downloads=%v requests=%v", output.Provider, downloaded, requests)
					}
					request := requests[0]
					host := tc.host
					if tc.name == "provider fallback" && (identityType == "kimi" || identityType == "kimi.com") {
						host = "api.kimi.com"
					}
					if request.AuthIndex != "international-auth" || request.Method != "GET" || request.URL != "https://"+host+"/coding/v1/usages" || request.Header["Authorization"] != "Bearer $TOKEN$" {
						t.Fatalf("unexpected usage request: %+v", request)
					}
					rows := quota.NormalizeQuotaRows(output)
					if len(rows) != 2 || rows[0].Label != "5h" || rows[1].Label != "Weekly" {
						t.Fatalf("unexpected Kimi quota rows: %+v", rows)
					}
					assertApproxFloatField(t, rows[0].UsedPercent, 92, "short window")
					assertApproxFloatField(t, rows[1].UsedPercent, 28, "weekly window")
				})
			}
		})
	}
}

type kimiAIMetadataCaller struct {
	recordingManagementCaller
	credentialType string
}

func (c *kimiAIMetadataCaller) FetchKimiCredentialMetadata(context.Context, string) (*authfiles.KimiCredentialMetadata, error) {
	return &authfiles.KimiCredentialMetadata{Type: c.credentialType}, nil
}

func TestKimiProviderNormalizesMonthlyRatioForAllAliases(t *testing.T) {
	for _, identityType := range []string{"kimi", "kimi-ai", "kimi.ai", "kimi.com"} {
		t.Run(identityType, func(t *testing.T) {
			for _, tc := range []struct {
				name, monthly string
				want          *float64
			}{
				{"numeric ratio", `{"used_ratio":0.0795,"reset_time":"2026-11-05T00:00:00Z"}`, floatPtr(7.95)},
				{"string ratio", `{"used_ratio":"0.125","reset_time":"2026-11-05T00:00:00Z"}`, floatPtr(12.5)},
				{"explicit zero", `{"used_ratio":0,"reset_time":"2026-11-05T00:00:00Z"}`, floatPtr(0)},
				{"exhausted", `{"used_ratio":1,"reset_time":"2026-11-05T00:00:00Z"}`, floatPtr(100)},
				{"missing ratio", `{"reset_time":"2026-11-05T00:00:00Z"}`, nil},
				{"null ratio", `{"used_ratio":null}`, nil},
				{"invalid ratio", `{"used_ratio":"invalid"}`, nil},
				{"nonfinite ratio", `{"used_ratio":"NaN"}`, nil},
				{"negative ratio", `{"used_ratio":-0.1}`, nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					body := `{"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"100","used":"25","remaining":"75"}}],"usages":{"limit_month_total":` + tc.monthly + `}}`
					caller := &kimiAIMetadataCaller{credentialType: identityType, recordingManagementCaller: recordingManagementCaller{responses: []*apicall.Response{quotaAPIResponse(200, body)}}}
					configs := quota.DefaultProviderConfigs()
					provider, ok := quota.NewDefaultProviderRegistry(caller, configs).Provider(identityType)
					if !ok {
						t.Fatalf("Kimi type %q is unsupported", identityType)
					}
					output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "international-auth", Provider: identityType, Type: identityType, FileName: stringPtr("account.json"), FilePath: stringPtr("/data/auths/account.json")}})
					if err != nil {
						t.Fatal(err)
					}
					rows := quota.NormalizeQuotaRows(output)
					if tc.want == nil {
						if len(rows) != 1 {
							t.Fatalf("invalid monthly data must not create a zero quota: %+v", rows)
						}
					} else {
						if len(rows) != 2 || rows[1].Key != "usages.limit_month_total" || rows[1].Label != "Monthly" || rows[1].ResetAt != "2026-11-05T00:00:00Z" {
							t.Fatalf("unexpected monthly row: %+v", rows)
						}
						assertApproxFloatField(t, rows[1].UsedPercent, *tc.want, "monthly percent")
						assertApproxFloatField(t, rows[1].Remaining, 100-*tc.want, "monthly remaining")
						if rows[1].Window != nil {
							t.Fatal("monthly window length must not be invented")
						}
					}
				})
			}
		})
	}
}

func TestKimiRefreshPipelineSupportsManualScheduledAndInspection(t *testing.T) {
	for _, identityType := range []string{"kimi", "kimi-ai", "kimi.ai", "kimi.com"} {
		t.Run(identityType, func(t *testing.T) {
			for _, source := range []quota.RefreshSource{quota.RefreshSourceManual, quota.RefreshSourceScheduled, quota.RefreshSourceInspection} {
				t.Run(string(source), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/v0/management/auth-files/download":
							_, _ = fmt.Fprintf(w, `{"type":%q,"access_token":"credential-secret","refresh_token":"refresh-secret"}`, identityType)
						case "/v0/management/api-call":
							_ = json.NewEncoder(w).Encode(quotaAPIResponse(200, `{"usages":{"limit_month_total":{"used_ratio":1,"reset_time":"2026-11-05T00:00:00Z"}}}`))
						default:
							t.Errorf("unexpected CPA path: %s", r.URL.Path)
						}
					}))
					defer server.Close()
					db := openQuotaTestDatabase(t)
					seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "international-auth", Provider: identityType, Type: identityType, AuthType: entities.UsageIdentityAuthTypeAuthFile, FileName: stringPtr("account.json"), FilePath: stringPtr("/data/auths/account.json")})
					service := quota.NewServiceWithOptions(db, cpa.NewClient(server.URL, "management-key", time.Second, false), quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest(), QuotaUpstreamResponsesEnabled: true})
					t.Cleanup(service.StopRefreshTasks)
					setRefreshCooldown(service, func(time.Duration) {})
					switch source {
					case quota.RefreshSourceManual:
						response := queueManualQuotaRefresh(t, service, "international-auth")
						if response.Accepted != 1 {
							t.Fatalf("international auth not queued: %+v", response)
						}
					case quota.RefreshSourceScheduled:
						if err := service.RunAutoRefresh(context.Background()); err != nil {
							t.Fatal(err)
						}
					case quota.RefreshSourceInspection:
						if _, err := service.StartInspection(context.Background()); err != nil {
							t.Fatal(err)
						}
					}
					task := waitForRefreshTask(t, service, "international-auth", quota.RefreshTaskStatusCompleted)
					if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].Label != "Monthly" {
						t.Fatalf("international refresh did not produce monthly quota: %+v", task)
					}
					status, err := service.GetInspectionStatus(context.Background())
					if err != nil || status.Total != 1 || status.Cached != 1 || status.LimitReached != 1 || len(status.Results) != 1 || status.Results[0].Status != quota.InspectionResultStatusLimitReached {
						t.Fatalf("international quota not classified as limit reached: %+v err=%v", status, err)
					}
					host := "api.kimi.ai"
					if identityType == "kimi" || identityType == "kimi.com" {
						host = "api.kimi.com"
					}
					if len(task.UpstreamResponses) != 1 || task.UpstreamResponses[0].URL != "https://"+host+"/coding/v1/usages" {
						t.Fatalf("credential download must stay out of recorded upstream responses: %+v", task.UpstreamResponses)
					}
					encoded, _ := json.Marshal(task)
					if strings.Contains(string(encoded), "credential-secret") || strings.Contains(string(encoded), "refresh-secret") {
						t.Fatal("credential download was retained in refresh output")
					}
				})
			}
		})
	}
}

func TestKimiProviderRejectsUnavailableCredentialWithoutUsageCall(t *testing.T) {
	for _, identityType := range []string{"kimi", "kimi-ai", "kimi.ai", "kimi.com"} {
		t.Run(identityType, func(t *testing.T) {
			for _, tc := range []struct {
				name, credential string
				status           int
				fileName, path   *string
			}{
				{"missing filename", `{}`, 200, nil, stringPtr("/data/auths/account.json")},
				{"runtime only", `{}`, 200, stringPtr("runtime-account.json"), nil},
				{"HTTP error", `{"access_token":"payload-secret"}`, 403, stringPtr("account.json"), stringPtr("/data/auths/account.json")},
				{"invalid credential", `{"access_token":"payload-secret"`, 200, stringPtr("account.json"), stringPtr("/data/auths/account.json")},
				{"array credential", `["payload-secret"]`, 200, stringPtr("account.json"), stringPtr("/data/auths/account.json")},
				{"null credential", `null`, 200, stringPtr("account.json"), stringPtr("/data/auths/account.json")},
			} {
				t.Run(tc.name, func(t *testing.T) {
					calls := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						if r.URL.Path != "/v0/management/auth-files/download" {
							t.Errorf("must not call usage API after credential failure: %s", r.URL.Path)
						}
						w.WriteHeader(tc.status)
						_, _ = fmt.Fprint(w, tc.credential)
					}))
					defer server.Close()
					registry := quota.NewDefaultProviderRegistry(cpa.NewClient(server.URL, "management-key", time.Second, false), quota.DefaultProviderConfigs())
					provider, ok := registry.Provider(identityType)
					if !ok {
						t.Fatal("international Kimi provider is unsupported")
					}
					_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "auth", Provider: identityType, Type: identityType, FileName: tc.fileName, FilePath: tc.path}})
					if err == nil || strings.Contains(err.Error(), "payload-secret") {
						t.Fatalf("expected sanitized credential error, got %v", err)
					}
					if (tc.fileName == nil || tc.path == nil) && calls != 0 {
						t.Fatalf("non-downloadable identity made %d requests", calls)
					}
				})
			}
		})
	}
}
