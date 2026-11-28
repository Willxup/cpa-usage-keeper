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

func TestAntigravityRefreshIgnoresDeprecatedArgonGroupAndMergesUsageIntoGemini(t *testing.T) {
	// 无论响应是否残留已下线的 Argon 组或回退路径如何，所有 gemini-* 模型都统一归入 Gemini Models。
	argonByName := `{"displayName":"Gemini 4 Argon","buckets":[{"bucketId":"other","window":"weekly","remainingFraction":0.6}]}`
	argonByID := `{"displayName":"Unrecognized","buckets":[{"bucketId":"gemini-argon-wtu-weekly","remainingFraction":"NaN"}]}`
	for _, tier := range []struct{ id, name, plan string }{
		{id: "g1-pro-tier", name: "Pro", plan: "pro"},
		{id: "g1-ultra-tier", name: "Ultra", plan: "ultra"},
		{id: "g1-ultra-lite-tier", name: "Google AI Ultra", plan: "ultra-lite"},
	} {
		t.Run(tier.plan, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				argon    string
				fallback bool
			}{
				{name: "legacy Argon recognized by name ignored", argon: argonByName},
				{name: "legacy Argon recognized by ID ignored", argon: argonByID},
				{name: "legacy Argon ignored across fallback", argon: argonByID, fallback: true},
				{name: "no Argon group unified Gemini pool"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					db := openQuotaTestDB(t)
					seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "argon-auth", Provider: "antigravity", Type: "antigravity", AuthType: entities.UsageIdentityAuthTypeCredential, ProjectID: new("project-123")})
					now := time.Now().Truncate(time.Second)
					if err := db.Create(&[]entities.UsageEvent{
						{EventKey: "gemini", AuthIndex: "argon-auth", Model: "gemini-3-flash", Timestamp: now.Add(-5 * time.Minute), TotalTokens: 100, CostUSD: floatPtr(.5), CostAvailable: boolPtr(true)},
						{EventKey: "argon", AuthIndex: "argon-auth", Model: "gemini-4-argon-high", Timestamp: now.Add(-5 * time.Minute), TotalTokens: 300, CostUSD: floatPtr(1.5), CostAvailable: boolPtr(true)},
						{EventKey: "carbon", AuthIndex: "argon-auth", Model: "gemini-4-carbon-high", Timestamp: now.Add(-5 * time.Minute), TotalTokens: 50, CostUSD: floatPtr(.2), CostAvailable: boolPtr(true)},
						{EventKey: "older-argon", AuthIndex: "argon-auth", Model: "gemini-4-argon-high", Timestamp: now.Add(-24 * time.Hour), TotalTokens: 60, CostUSD: floatPtr(.25), CostAvailable: boolPtr(true)},
					}).Error; err != nil {
						t.Fatal(err)
					}
					// 长窗口从小时聚合读取旧事件；直接插入事件的测试须同时准备对应聚合。
					if err := db.Create(&entities.UsageOverviewHourlyStat{
						BucketStart: now.Add(-24 * time.Hour).Truncate(time.Hour), AuthIndex: "argon-auth", Model: "gemini-4-argon-high",
						TotalTokens: 60, CostUSD: floatPtr(.25), UnavailableCostCount: intPtr(0), CreatedAt: now, UpdatedAt: now,
					}).Error; err != nil {
						t.Fatal(err)
					}
					fiveHourReset := now.Add(time.Hour).Format(time.RFC3339)
					weeklyReset := now.Add(24 * time.Hour).Format(time.RFC3339)
					gemini := fmt.Sprintf(`{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.5,"resetTime":%q},{"bucketId":"gemini-weekly","window":"weekly","remainingFraction":0.75,"resetTime":%q}]}`, fiveHourReset, weeklyReset)
					groups := gemini
					if tc.argon != "" {
						groups += "," + tc.argon
					}
					responses := []*apicall.Response{quotaAPIResponse(200, `{"groups":[`+groups+`]}`)}
					if tc.fallback {
						responses = []*apicall.Response{quotaAPIResponse(200, `{"groups":[`+tc.argon+`]}`), quotaAPIResponse(200, `{"groups":[`+gemini+`]}`)}
					}
					subscription := fmt.Sprintf(`{"paidTier":{"id":%q,"name":%q}}`, tier.id, tier.name)
					caller := &antigravityRecordingCaller{quota: recordingManagementCaller{responses: responses}, subscription: recordingManagementCaller{responses: []*apicall.Response{quotaAPIResponse(200, subscription)}}}
					configs := DefaultProviderConfigs()
					service := newQuotaRefreshService(t, db, NewProviderRegistry(map[string]ProviderHandler{"antigravity": NewAntigravityProvider(caller, configs.Antigravity, configs.AntigravitySubscriptions[:1])}))
					if _, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"argon-auth"}, Source: RefreshSourceManual}); err != nil {
						t.Fatal(err)
					}
					task := waitForRefreshTask(t, service, "argon-auth", RefreshTaskStatusCompleted)
					if task.Quota == nil || len(task.Quota.Quota) != 2 {
						t.Fatalf("deprecated Argon must not create a bar: %+v", task.Quota)
					}
					gotSubscription := task.Quota.Subscription
					if gotSubscription == nil || gotSubscription.Provider != "antigravity" || gotSubscription.Plan != tier.plan || gotSubscription.TierID != tier.id || gotSubscription.TierName != tier.name {
						t.Fatalf("unexpected normalized subscription: %+v", gotSubscription)
					}
					fiveHour := findQuotaRow(t, task.Quota.Quota, "bucket.antigravity-gemini-models.gemini-5h")
					weekly := findQuotaRow(t, task.Quota.Quota, "bucket.antigravity-gemini-models.gemini-weekly")
					if fiveHour.RemainingFraction == nil || *fiveHour.RemainingFraction != .5 || fiveHour.ResetAt != fiveHourReset || weekly.RemainingFraction == nil || *weekly.RemainingFraction != .75 || weekly.ResetAt != weeklyReset {
						t.Fatalf("official quota and reset must be preserved: 5h=%+v weekly=%+v", fiveHour, weekly)
					}
					assertWindowUsage(t, fiveHour, 450, 2.2)
					assertWindowUsage(t, weekly, 510, 2.45)
					encoded, err := json.Marshal(task.Quota)
					if err != nil {
						t.Fatal(err)
					}
					serialized := strings.ToLower(string(encoded))
					if strings.Contains(serialized, "separateargon") || strings.Contains(serialized, "gemini-4-argon") {
						t.Fatalf("deprecated Argon group must not be serialized: %s", encoded)
					}
				})
			}
		})
	}
}

func TestAttachWindowUsageStatsAntigravityUnifiedGeminiPool(t *testing.T) {
	// 独立 Argon 桶取消后，所有套餐下 gemini-* 均无条件归入 antigravity-gemini-models，未知模型仍保留完整性保护。
	for _, tc := range []struct {
		name           string
		plan           string
		geminiName     string
		claudeGPTName  string
		legacyArgon    bool
		argonUsage     bool
		argonAvailable bool
		unknownUsage   bool
		wantGemini     int64
		wantGeminiCost float64
		wantMissing    bool
		missingGemini  bool
	}{
		{name: "Pro unifies Argon into Gemini", plan: "pro", argonUsage: true, argonAvailable: true, wantGemini: 450, wantGeminiCost: 2.2},
		{name: "legacy Argon group ignored while unifying usage", plan: "pro", legacyArgon: true, argonUsage: true, argonAvailable: true, wantGemini: 450, wantGeminiCost: 2.2},
		{name: "legacy display names retain grouped stats", plan: "pro", geminiName: "Gemini Future Models", claudeGPTName: "GPT and Claude models", argonUsage: true, argonAvailable: true, wantGemini: 450, wantGeminiCost: 2.2},
		{name: "Free unifies Argon into Gemini", plan: "free", argonUsage: true, argonAvailable: true, wantGemini: 450, wantGeminiCost: 2.2},
		{name: "Ultra unifies Argon into Gemini", plan: "ultra", argonUsage: true, argonAvailable: true, wantGemini: 450, wantGeminiCost: 2.2},
		{name: "Ultra Lite unifies Argon into Gemini", plan: "ultra-lite", argonUsage: true, argonAvailable: true, wantGemini: 450, wantGeminiCost: 2.2},
		{name: "missing plan still unifies Argon into Gemini", argonUsage: true, argonAvailable: true, wantGemini: 450, wantGeminiCost: 2.2},
		{name: "without Argon usage", plan: "pro", wantGemini: 150, wantGeminiCost: .7},
		{name: "unknown model keeps completeness guard", plan: "ultra-lite", argonUsage: true, argonAvailable: true, unknownUsage: true, wantMissing: true},
		{name: "unavailable Argon cost suppresses Gemini pair only", plan: "pro", argonUsage: true, missingGemini: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openQuotaTestDB(t)
			now := time.Date(2026, 10, 20, 12, 30, 0, 0, time.Local)
			resetAt := now.Add(7*24*time.Hour - 2*time.Hour)
			alias := "gemini-4-argon-high"
			events := []entities.UsageEvent{
				{EventKey: "gemini", AuthIndex: "argon-auth", Model: "gemini-3-flash", ModelAlias: &alias, Timestamp: now.Add(-time.Hour), TotalTokens: 100, CostUSD: floatPtr(.5), CostAvailable: boolPtr(true)},
				{EventKey: "neon", AuthIndex: "argon-auth", Model: "gemini-4-neon-medium", Timestamp: now.Add(-time.Hour), TotalTokens: 50, CostUSD: floatPtr(.2), CostAvailable: boolPtr(true)},
				{EventKey: "claude", AuthIndex: "argon-auth", Model: "claude-sonnet-4-6", Timestamp: now.Add(-time.Hour), TotalTokens: 40, CostUSD: floatPtr(.2), CostAvailable: boolPtr(true)},
			}
			if tc.argonUsage {
				events = append(events, entities.UsageEvent{EventKey: "argon", AuthIndex: "argon-auth", Model: "gemini-4-argon-high", Timestamp: now.Add(-time.Hour), TotalTokens: 300, CostUSD: floatPtr(1.5), CostAvailable: boolPtr(tc.argonAvailable)})
			}
			if tc.unknownUsage {
				events = append(events, entities.UsageEvent{EventKey: "unknown", AuthIndex: "argon-auth", Model: "future-model", Timestamp: now.Add(-time.Hour), TotalTokens: 10, CostUSD: floatPtr(.1), CostAvailable: boolPtr(true)})
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
			if tc.legacyArgon {
				groups = append(groups, AntigravityQuotaGroup{DisplayName: "Gemini 4 Argon", Buckets: []AntigravityQuotaBucket{{BucketID: "gemini-argon-wtu-weekly", Window: "weekly", RemainingFraction: new(.6), ResetTime: reset}}})
			}
			response := CheckResponse{ID: "argon-auth", Quota: NormalizeQuotaRows(ProviderOutput{Provider: "antigravity", Result: AntigravityResult{Quota: &AntigravityQuotaPayload{Groups: groups}}})}
			if tc.plan != "" {
				response.Subscription = &SubscriptionInfo{Provider: "antigravity", Plan: tc.plan}
			}
			response = attachWindowUsageStats(service, context.Background(), "argon-auth", response, now)
			if len(response.Quota) != 2 {
				t.Fatalf("expected only Gemini and Claude & GPT rows, got %+v", response.Quota)
			}
			gemini := findQuotaRow(t, response.Quota, "bucket.antigravity-gemini-models.gemini-weekly")
			if gemini.RemainingFraction == nil || *gemini.RemainingFraction != .4 || gemini.ResetAt != reset {
				t.Fatalf("official quota and reset must remain visible: %+v", gemini)
			}
			claude := findQuotaRow(t, response.Quota, "bucket.antigravity-claude-and-gpt-models.claude-weekly")
			if tc.wantMissing {
				for _, row := range []QuotaRow{gemini, claude} {
					if row.WindowUsageTokens != nil || row.WindowUsageCost != nil {
						t.Fatalf("incomplete attribution must not yield group estimates: %+v", row)
					}
				}
			} else {
				if tc.missingGemini {
					if gemini.WindowUsageTokens != nil || gemini.WindowUsageCost != nil {
						t.Fatalf("unavailable shared cost must not produce a partial pair: %+v", gemini)
					}
				} else {
					assertWindowUsage(t, gemini, tc.wantGemini, tc.wantGeminiCost)
				}
				assertWindowUsage(t, claude, 40, .2)
			}
			if counter.UsageWindowQueries() != 1 {
				t.Fatalf("expected one shared grouped query, got %d", counter.UsageWindowQueries())
			}
		})
	}
}
