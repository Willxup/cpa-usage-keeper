package test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func TestAntigravityRefreshKeepsUndisplayableArgonEvidence(t *testing.T) {
	// Ultra 是合成契约：无效 Argon 不显示，但不能因组被过滤而混入 Gemini。
	argonByName := `{"displayName":"Gemini 4 Argon","buckets":[{"bucketId":"other","window":"weekly"}]}`
	argonByID := `{"displayName":"Unrecognized","buckets":[{"bucketId":"gemini-argon-wtu-weekly","remainingFraction":"NaN"}]}`
	for _, tc := range []struct {
		name       string
		argon      string
		fallback   bool
		wantTokens int64
		wantCost   float64
	}{
		{name: "missing fraction recognized by name", argon: argonByName, wantTokens: 100, wantCost: .5},
		{name: "non-finite fraction recognized by ID", argon: argonByID, wantTokens: 100, wantCost: .5},
		{name: "evidence survives empty endpoint fallback", argon: argonByID, fallback: true, wantTokens: 100, wantCost: .5},
		{name: "no Argon group Ultra shared pool", wantTokens: 400, wantCost: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openQuotaTestDB(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "argon-auth", Provider: "antigravity", Type: "antigravity", AuthType: entities.UsageIdentityAuthTypeCredential, ProjectID: new("project-123")})
			now := time.Now().Truncate(time.Second)
			if err := db.Create(&[]entities.UsageEvent{
				{EventKey: "gemini", AuthIndex: "argon-auth", Model: "gemini-3-flash", Timestamp: now.Add(-time.Hour), TotalTokens: 100, CostUSD: floatPtr(.5), CostAvailable: boolPtr(true)},
				{EventKey: "argon", AuthIndex: "argon-auth", Model: "gemini-4-argon-high", Timestamp: now.Add(-time.Hour), TotalTokens: 300, CostUSD: floatPtr(1.5), CostAvailable: boolPtr(true)},
			}).Error; err != nil {
				t.Fatal(err)
			}
			gemini := fmt.Sprintf(`{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.5,"resetTime":%q}]}`, now.Add(time.Hour).Format(time.RFC3339))
			groups := gemini
			if tc.argon != "" {
				groups += "," + tc.argon
			}
			responses := []*apicall.Response{quotaAPIResponse(200, `{"groups":[`+groups+`]}`)}
			if tc.fallback {
				responses = []*apicall.Response{quotaAPIResponse(200, `{"groups":[`+tc.argon+`]}`), quotaAPIResponse(200, `{"groups":[`+gemini+`]}`)}
			}
			caller := &antigravityRecordingCaller{quota: recordingManagementCaller{responses: responses}, subscription: recordingManagementCaller{responses: []*apicall.Response{quotaAPIResponse(200, `{"paidTier":{"id":"g1-ultra-tier","name":"Ultra"}}`)}}}
			configs := DefaultProviderConfigs()
			service := newQuotaRefreshService(t, db, NewProviderRegistry(map[string]ProviderHandler{"antigravity": NewAntigravityProvider(caller, configs.Antigravity, configs.AntigravitySubscriptions[:1])}))
			if _, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"argon-auth"}, Source: RefreshSourceManual}); err != nil {
				t.Fatal(err)
			}
			task := waitForRefreshTask(t, service, "argon-auth", RefreshTaskStatusCompleted)
			if task.Quota == nil || len(task.Quota.Quota) != 1 {
				t.Fatalf("invalid Argon must not create a bar: %+v", task.Quota)
			}
			row := findQuotaRow(t, task.Quota.Quota, "bucket.antigravity-gemini-models.gemini-5h")
			assertWindowUsage(t, row, tc.wantTokens, tc.wantCost)
			encoded, err := json.Marshal(task.Quota)
			if err != nil {
				t.Fatal(err)
			}
			serialized := strings.ToLower(string(encoded))
			if strings.Contains(serialized, "separateargon") || strings.Contains(serialized, "gemini-4-argon") {
				t.Fatalf("internal Argon evidence must not be serialized: %s", encoded)
			}
		})
	}
}

func TestAttachWindowUsageStatsAntigravityArgonPools(t *testing.T) {
	// Ultra 的共享池行为是基于公开描述的合成契约用例，不是 Ultra 实际捕获。
	for _, tc := range []struct {
		name           string
		plan           string
		geminiName     string
		claudeGPTName  string
		dedicated      bool
		argonUsage     bool
		argonAvailable bool
		wantGemini     int64
		wantGeminiCost float64
		wantArgon      int64
		wantArgonCost  float64
		wantMissing    bool
		missingArgon   bool
	}{
		{name: "dedicated Pro", plan: "pro", dedicated: true, argonUsage: true, argonAvailable: true, wantGemini: 100, wantGeminiCost: .5, wantArgon: 300, wantArgonCost: 1.5},
		{name: "legacy names retain grouped stats with Argon", plan: "pro", geminiName: "Gemini Future Models", claudeGPTName: "GPT and Claude models", dedicated: true, argonUsage: true, argonAvailable: true, wantGemini: 100, wantGeminiCost: .5, wantArgon: 300, wantArgonCost: 1.5},
		{name: "dedicated takes precedence over Ultra", plan: "ultra", dedicated: true, argonUsage: true, argonAvailable: true, wantGemini: 100, wantGeminiCost: .5, wantArgon: 300, wantArgonCost: 1.5},
		{name: "synthetic Ultra shared pool", plan: "ultra", argonUsage: true, argonAvailable: true, wantGemini: 400, wantGeminiCost: 2},
		{name: "Pro missing group is uncertain", plan: "pro", argonUsage: true, argonAvailable: true, wantMissing: true},
		{name: "missing plan is uncertain", argonUsage: true, argonAvailable: true, wantMissing: true},
		{name: "Ultra Lite is uncertain", plan: "ultra-lite", argonUsage: true, argonAvailable: true, wantMissing: true},
		{name: "uncertain without Argon usage", plan: "pro", wantGemini: 100, wantGeminiCost: .5},
		{name: "real zero in dedicated group", plan: "pro", dedicated: true, wantGemini: 100, wantGeminiCost: .5},
		{name: "unavailable Argon cost", plan: "pro", dedicated: true, argonUsage: true, wantGemini: 100, wantGeminiCost: .5, missingArgon: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openQuotaTestDB(t)
			now := time.Date(2026, 10, 20, 12, 30, 0, 0, time.Local)
			resetAt := now.Add(7*24*time.Hour - 2*time.Hour)
			alias := "gemini-4-argon-high"
			events := []entities.UsageEvent{
				{EventKey: "gemini", AuthIndex: "argon-auth", Model: "gemini-3-flash", ModelAlias: &alias, Timestamp: now.Add(-time.Hour), TotalTokens: 100, CostUSD: floatPtr(.5), CostAvailable: boolPtr(true)},
				{EventKey: "claude", AuthIndex: "argon-auth", Model: "claude-sonnet-4-6", Timestamp: now.Add(-time.Hour), TotalTokens: 40, CostUSD: floatPtr(.2), CostAvailable: boolPtr(true)},
			}
			if tc.argonUsage {
				events = append(events, entities.UsageEvent{EventKey: "argon", AuthIndex: "argon-auth", Model: "gemini-4-argon-high", Timestamp: now.Add(-time.Hour), TotalTokens: 300, CostUSD: floatPtr(1.5), CostAvailable: boolPtr(tc.argonAvailable)})
			}
			if err := db.Create(&events).Error; err != nil {
				t.Fatal(err)
			}
			counter := &quotaUsageQueryCounter{}
			service := NewServiceWithRegistry(db.Session(&gorm.Session{Logger: counter}), NewProviderRegistry(nil))
			defer service.StopRefreshTasks()
			reset := timeutil.FormatStorageTime(resetAt)
			geminiName, claudeGPTName := tc.geminiName, tc.claudeGPTName
			if geminiName == "" {
				geminiName = "Gemini Models"
			}
			if claudeGPTName == "" {
				claudeGPTName = "Claude and GPT models"
			}
			groups := []AntigravityQuotaGroup{
				{DisplayName: geminiName, Buckets: []AntigravityQuotaBucket{{BucketID: "gemini-weekly", Window: "weekly", RemainingFraction: new(.4), ResetTime: reset}}},
				{DisplayName: claudeGPTName, Buckets: []AntigravityQuotaBucket{{BucketID: "claude-weekly", Window: "weekly", RemainingFraction: new(.8), ResetTime: reset}}},
			}
			if tc.dedicated {
				groups = append(groups, AntigravityQuotaGroup{DisplayName: "Gemini 4 Argon", Buckets: []AntigravityQuotaBucket{{BucketID: "gemini-argon-wtu-weekly", Window: "weekly", RemainingFraction: new(.6), ResetTime: reset}}})
			}
			response := CheckResponse{ID: "argon-auth", Quota: NormalizeQuotaRows(ProviderOutput{Provider: "antigravity", Result: AntigravityResult{Quota: &AntigravityQuotaPayload{Groups: groups}}})}
			if tc.plan != "" {
				response.Subscription = &SubscriptionInfo{Provider: "antigravity", Plan: tc.plan}
			}
			response = attachWindowUsageStats(service, context.Background(), "argon-auth", response, now)
			if len(response.Quota) != len(groups) {
				t.Fatalf("unexpected quota rows: %+v", response.Quota)
			}
			gemini := findQuotaRow(t, response.Quota, "bucket.antigravity-gemini-models.gemini-weekly")
			if gemini.RemainingFraction == nil || *gemini.RemainingFraction != .4 {
				t.Fatalf("official quota must remain visible: %+v", gemini)
			}
			if tc.wantMissing {
				if gemini.WindowUsageTokens != nil || gemini.WindowUsageCost != nil {
					t.Fatalf("uncertain Argon attribution must not yield estimates: %+v", gemini)
				}
			} else {
				assertWindowUsage(t, gemini, tc.wantGemini, tc.wantGeminiCost)
				assertWindowUsage(t, findQuotaRow(t, response.Quota, "bucket.antigravity-claude-and-gpt-models.claude-weekly"), 40, .2)
			}
			if tc.dedicated {
				argon := findQuotaRow(t, response.Quota, "bucket.antigravity-gemini-4-argon.gemini-argon-wtu-weekly")
				if tc.missingArgon {
					if argon.WindowUsageTokens != nil || argon.WindowUsageCost != nil {
						t.Fatalf("unavailable Argon cost must not produce a partial pair: %+v", argon)
					}
				} else {
					assertWindowUsage(t, argon, tc.wantArgon, tc.wantArgonCost)
				}
			}
			if counter.UsageWindowQueries() != 1 {
				t.Fatalf("expected one shared grouped query, got %d", counter.UsageWindowQueries())
			}
		})
	}
}
