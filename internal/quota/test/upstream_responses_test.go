package test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestRefreshCachesUpstreamResponsesWhenEnabled(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	usageBody := `{"plan_type":"plus","rate_limit":{"allowed":true}}`
	detailsBody := `{"available_count":1}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{StatusCode: 200, Header: map[string][]string{"X-Request-Id": {"usage-request"}}, Body: json.RawMessage(strconv.Quote(usageBody))},
		{StatusCode: 200, Header: map[string][]string{"X-Request-Id": {"credits-request"}}, Body: json.RawMessage(strconv.Quote(detailsBody))},
	}}
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{
		PricingCatalog:                emptyPricingCatalogForTest(),
		QuotaUpstreamResponsesEnabled: true,
	})
	t.Cleanup(service.StopRefreshTasks)

	response, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, quota.RefreshTaskStatusCompleted)
	assertUpstreamResponses(t, task.UpstreamResponses)
	encodedTask, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal refresh task: %v", err)
	}
	if !contains(string(encodedTask), `"upstream_responses":[{"method":"GET","url":"https://chatgpt.com/backend-api/wham/usage","status_code":200`) {
		t.Fatalf("expected refresh task JSON to expose upstream responses, got %s", encodedTask)
	}

	cache, err := service.GetCachedQuota(context.Background(), quota.CacheRequest{AuthIndexes: []string{"codex-auth"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 {
		t.Fatalf("expected one cached quota item, got %+v", cache.Items)
	}
	assertUpstreamResponses(t, cache.Items[0].UpstreamResponses)

	// 新任务会替换同一 auth_index 的整条缓存，只保留本次刷新实际收到的响应。
	service.WaitRefreshTasks()
	latestBody := `{"plan_type":"team","rate_limit":{"allowed":true},"rate_limit_reset_credits":{"available_count":0}}`
	caller.responses = []*apicall.Response{{StatusCode: 200, Body: json.RawMessage(strconv.Quote(latestBody))}}
	response, err = service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual})
	if err != nil {
		t.Fatalf("second Refresh returned error: %v", err)
	}
	latestTask := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, quota.RefreshTaskStatusCompleted)
	if len(latestTask.UpstreamResponses) != 1 || latestTask.UpstreamResponses[0].Body != latestBody {
		t.Fatalf("expected latest refresh to replace upstream responses, got %+v", latestTask.UpstreamResponses)
	}
}

func TestRefreshOmitsUpstreamResponsesWhenDisabled(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	usageBody := `{"plan_type":"plus","rate_limit":{"allowed":true},"rate_limit_reset_credits":{"available_count":0}}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{StatusCode: 200, Body: json.RawMessage(strconv.Quote(usageBody))}}}
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
	t.Cleanup(service.StopRefreshTasks)

	response, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, quota.RefreshTaskStatusCompleted)
	if len(task.UpstreamResponses) != 0 {
		t.Fatalf("expected disabled upstream responses to stay empty, got %+v", task.UpstreamResponses)
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal refresh task: %v", err)
	}
	if contains(string(encoded), "upstream_responses") {
		t.Fatalf("expected disabled upstream responses to be omitted, got %s", encoded)
	}
	cache, err := service.GetCachedQuota(context.Background(), quota.CacheRequest{AuthIndexes: []string{"codex-auth"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 || len(cache.Items[0].UpstreamResponses) != 0 {
		t.Fatalf("expected disabled cache response to omit upstream responses, got %+v", cache.Items)
	}
}

func TestRefreshReturnsUpstreamResponseWhenProviderRejectsPayload(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	errorBody := `{"error":{"message":"rate limited"}}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 429,
		Header:     map[string][]string{"X-Request-Id": {"failed-request"}},
		Body:       json.RawMessage(strconv.Quote(errorBody)),
	}}}
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{
		PricingCatalog:                emptyPricingCatalogForTest(),
		QuotaUpstreamResponsesEnabled: true,
	})
	t.Cleanup(service.StopRefreshTasks)

	response, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, quota.RefreshTaskStatusFailed)
	if len(task.UpstreamResponses) != 1 || task.UpstreamResponses[0].StatusCode != 429 || task.UpstreamResponses[0].Body != errorBody {
		t.Fatalf("expected failed provider response to remain inspectable, got %+v", task.UpstreamResponses)
	}
}

func assertUpstreamResponses(t *testing.T, responses []quota.UpstreamResponse) {
	t.Helper()
	if len(responses) != 2 {
		t.Fatalf("expected usage and reset-credit responses, got %+v", responses)
	}
	if responses[0].Method != "GET" || responses[0].URL != "https://chatgpt.com/backend-api/wham/usage" || responses[0].StatusCode != 200 || responses[0].Header["X-Request-Id"][0] != "usage-request" || responses[0].Body != `{"plan_type":"plus","rate_limit":{"allowed":true}}` {
		t.Fatalf("unexpected usage upstream response: %+v", responses[0])
	}
	if responses[1].URL != quota.CodexRateLimitResetCreditsURL || responses[1].Header["X-Request-Id"][0] != "credits-request" || responses[1].Body != `{"available_count":1}` {
		t.Fatalf("unexpected reset-credit upstream response: %+v", responses[1])
	}
}
