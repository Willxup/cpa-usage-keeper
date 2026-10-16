package test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"gorm.io/gorm"
)

const claudeGrantBlock = `{"eligible":true,"at_limit":true,"next_grant_id":"spring","grants":[{"id":"spring","resets_total":3,"resets_left":2,"usable_now":true,"clears":["five_hour"],"ends_at":"2099-10-01T00:00:00Z"}]}`

func TestClaudeUsageIncludesOptionalResetGrantsWithoutExtraRequests(t *testing.T) {
	for _, tc := range []struct {
		name, block string
		wantCount   *int
	}{
		{"multiple uses", claudeGrantBlock, new(2)},
		{"explicit zero", `{"eligible":false,"grants":[]}`, new(0)},
		{"missing", `null`, nil},
		{"malformed", `{"eligible":true,"grants":[{"id":"bad","resets_total":1,"resets_left":2}]}`, nil},
		{"duplicate", `{"eligible":true,"grants":[{"id":"a","resets_total":1,"resets_left":1},{"id":"a","resets_total":1,"resets_left":1}]}`, nil},
		{"invalid scope type", `{"eligible":true,"grants":[{"id":"a","resets_total":1,"resets_left":1,"clears":[123]}]}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &recordingManagementCaller{responses: []*apicall.Response{
				quotaAPIResponse(200, `{"five_hour":{"utilization":45},"cedar_ember":`+tc.block+`}`),
				quotaAPIResponse(200, `{"organization":{"uuid":"11111111-1111-1111-1111-111111111111"}}`),
			}}
			configs := quota.DefaultProviderConfigs()
			output, err := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile).Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude"}})
			if err != nil {
				t.Fatal(err)
			}
			result := output.Result.(quota.ClaudeResult)
			if len(caller.requests) != 2 || caller.requests[0].URL != "https://api.anthropic.com/api/oauth/usage?cedar_ember=1&skip_spend=1" || caller.requests[0].Header["User-Agent"] != "claude-cli/2.1.280 (external, cli)" {
				t.Fatalf("unexpected calls: %+v", caller.requests)
			}
			if result.Usage.FiveHour.Utilization != 45 {
				t.Fatal("optional grants broke quota")
			}
			if tc.wantCount == nil {
				if result.Usage.ResetGrants != nil {
					t.Fatal("unknown grants became known")
				}
				return
			}
			if result.Usage.ResetGrants == nil || result.Usage.ResetGrants.AvailableCount != *tc.wantCount {
				t.Fatalf("wrong grants: %+v", result.Usage.ResetGrants)
			}
		})
	}
}

func TestClaudeResetGrantsPreservesUpstreamScopeValues(t *testing.T) {
	block := strings.Replace(claudeGrantBlock, `"clears":["five_hour"]`, `"label":"Upstream 自由名称","clears":["future_window","five_hour","seven_day","seven_day_overage_included","future_window"]`, 1)
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		quotaAPIResponse(200, `{"cedar_ember":`+block+`}`),
		quotaAPIResponse(200, `{"organization":{"uuid":"11111111-1111-1111-1111-111111111111"}}`),
	}}
	configs := quota.DefaultProviderConfigs()
	output, err := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile).Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude"}})
	if err != nil {
		t.Fatal(err)
	}
	status := output.Result.(quota.ClaudeResult).Usage.ResetGrants
	if status == nil || len(status.Grants) != 1 {
		t.Fatalf("missing grants: %+v", status)
	}
	grant := status.Grants[0]
	want := []string{"future_window", "five_hour", "seven_day", "seven_day_overage_included", "future_window"}
	if !reflect.DeepEqual(grant.Clears, want) || grant.Label != "Upstream 自由名称" {
		t.Fatalf("upstream display data changed: %+v", grant)
	}
}

func TestClaudeResetHeaderPreservesGrants(t *testing.T) {
	caller := &claudeResetCaller{result: "reset"}
	service, _ := claudeResetFixture(t, caller)
	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "claude"})
	if err != nil || response.ClaudeResetGrants == nil {
		t.Fatal("service lost grants", err)
	}
	setRefreshTask(service, "claude", &quota.RefreshTaskRecord{AuthIndex: "claude", Type: "claude", Status: quota.RefreshTaskStatusCompleted, Quota: &response, RefreshedAt: time.Now().Add(-time.Minute)})
	snapshot := buildClaudePartialHeaderSnapshot(t, time.Now(), claudePartialHeader{window: "5h", utilization: "0.2"})
	snapshot.AuthIndex = "claude"
	if !applyUsageHeaderSnapshot(service, context.Background(), *snapshot) {
		t.Fatal("header not applied")
	}
	if task := refreshTaskRecord(service, "claude"); task.Quota.ClaudeResetGrants == nil || task.Quota.ClaudeResetGrants.AvailableCount != 2 {
		t.Fatal("header erased grant count")
	}
}

type claudeResetCaller struct {
	mu            sync.Mutex
	requests      []apicall.Request
	result        string
	org           string
	grantBlock    string
	profileErr    error
	recoveryErr   error
	recoveryCalls int
	postEntered   chan struct{}
	postRelease   chan struct{}
}

func (c *claudeResetCaller) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, request)
	result, org, block := c.result, c.org, c.grantBlock
	c.mu.Unlock()
	if request.Method == "POST" {
		if request.URL == quota.CodexRateLimitResetCreditsConsumeURL {
			return quotaAPIResponse(200, `{"code":"reset","windows_reset":2}`), nil
		}
		if c.postEntered != nil {
			c.postEntered <- struct{}{}
			<-c.postRelease
		}
		if result == "unknown" {
			return nil, errors.New("mock connection lost")
		}
		if result == "rate_limited" {
			return quotaAPIResponse(429, `{}`), nil
		}
		if result == "http_failure" {
			return quotaAPIResponse(500, `{"result":"reset"}`), nil
		}
		if result == "auth_error" {
			return quotaAPIResponse(401, `{}`), nil
		}
		return quotaAPIResponse(200, `{"result":"`+result+`"}`), nil
	}
	if strings.Contains(request.URL, "profile") {
		if c.profileErr != nil {
			return nil, c.profileErr
		}
		if org == "" {
			org = "11111111-1111-1111-1111-111111111111"
		}
		return quotaAPIResponse(200, `{"organization":{"uuid":"`+org+`"},"account":{"uuid":"22222222-2222-2222-2222-222222222222"}}`), nil
	}
	if block == "" {
		block = claudeGrantBlock
	}
	return quotaAPIResponse(200, `{"five_hour":{"utilization":100},"cedar_ember":`+block+`}`), nil
}

func (c *claudeResetCaller) ResetQuota(context.Context, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recoveryCalls++
	return c.recoveryErr
}
func (c *claudeResetCaller) posts() []apicall.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []apicall.Request
	for _, r := range c.requests {
		if r.Method == "POST" {
			out = append(out, r)
		}
	}
	return out
}

func claudeResetFixture(t *testing.T, caller *claudeResetCaller) (*quota.Service, *gorm.DB) {
	t.Helper()
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "claude", Type: "claude", Provider: "claude"})
	config := quota.DefaultProviderConfigs()
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": quota.NewClaudeProvider(caller, config.ClaudeUsage, config.ClaudeProfile)}))
	return service, db
}

const claudeTestOrg = "11111111-1111-1111-1111-111111111111"

func claudeManualReset() quota.ResetRequest {
	return quota.ResetRequest{AuthIndex: "claude", GrantID: "spring", OrganizationID: claudeTestOrg}
}

func TestClaudeResetPopupReadsUsageAndProfileButConfirmationOnlyClaims(t *testing.T) {
	caller := &claudeResetCaller{result: "reset"}
	service, _ := claudeResetFixture(t, caller)
	details, err := service.GetClaudeResetGrants(context.Background(), "claude")
	if err != nil || details.SelectedGrantID != "spring" || details.OrganizationID != claudeTestOrg || len(caller.requests) != 2 {
		t.Fatalf("popup: %+v %v requests=%d", details, err, len(caller.requests))
	}
	for range 2 {
		result, err := service.Reset(context.Background(), claudeManualReset())
		if err != nil || result.Code != "reset" || result.RecoveryFailed {
			t.Fatalf("reset: %+v %v", result, err)
		}
	}
	if len(caller.requests) != 4 || caller.recoveryCalls != 2 {
		t.Fatalf("confirmation repeated a read: %+v", caller.requests)
	}
	posts := caller.posts()
	ids := map[string]bool{}
	for _, request := range posts {
		data := request.Data.(map[string]string)
		id := data["request_id"]
		if len(id) != 36 || ids[id] || data["program"] != "cedar_ember" || data["grant_id"] != "spring" || request.URL != "https://api.anthropic.com/api/organizations/"+claudeTestOrg+"/reset_rate_limits" || request.Header["Authorization"] != "Bearer $TOKEN$" {
			t.Fatalf("wrong claim: %+v", request)
		}
		ids[id] = true
	}
}

func TestClaudeResetRejectsMalformedParametersWithoutUpstreamRequests(t *testing.T) {
	for _, request := range []quota.ResetRequest{
		{AuthIndex: "claude"}, {AuthIndex: "claude", GrantID: "../escape", OrganizationID: claudeTestOrg},
		{AuthIndex: "claude", GrantID: "spring", OrganizationID: "https://example.com"},
	} {
		caller := &claudeResetCaller{result: "reset"}
		service, _ := claudeResetFixture(t, caller)
		result, err := service.Reset(context.Background(), request)
		if err != nil || result.Code != "unavailable" || len(caller.requests) != 0 {
			t.Fatalf("invalid parameters dispatched: %+v %v", result, err)
		}
	}
}

func TestClaudeResetUpstreamResultsAndRecovery(t *testing.T) {
	for _, code := range []string{"reset", "already_used", "not_limited", "cooldown", "ineligible", "unavailable", "auth_error", "rate_limited", "unknown", "unexpected", "http_failure"} {
		t.Run(code, func(t *testing.T) {
			caller := &claudeResetCaller{result: code, recoveryErr: errors.New("recovery unavailable")}
			service, _ := claudeResetFixture(t, caller)
			result, err := service.Reset(context.Background(), claudeManualReset())
			expected := code
			if code == "unexpected" || code == "http_failure" {
				expected = "unknown"
			}
			success := code == "reset" || code == "already_used"
			if err != nil || result.Code != expected || result.RecoveryFailed != success || len(caller.requests) != 1 || (caller.recoveryCalls == 1) != success {
				t.Fatalf("outcome: %+v %v calls=%d", result, err, caller.recoveryCalls)
			}
		})
	}
}

func TestClaudeResetUsesExistingSameAuthInFlightLock(t *testing.T) {
	caller := &claudeResetCaller{result: "reset", postEntered: make(chan struct{}, 1), postRelease: make(chan struct{})}
	service, _ := claudeResetFixture(t, caller)
	done := make(chan error, 1)
	go func() { _, err := service.Reset(context.Background(), claudeManualReset()); done <- err }()
	<-caller.postEntered
	_, err := service.Reset(context.Background(), claudeManualReset())
	if !errors.Is(err, quota.ErrResetInProgress) || len(caller.posts()) != 1 {
		t.Fatal("concurrent claim was not blocked", err)
	}
	close(caller.postRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClaudeGrantCannotFallThroughToCodexConsumption(t *testing.T) {
	caller := &claudeResetCaller{result: "reset"}
	_, db := claudeResetFixture(t, caller)
	config := quota.DefaultProviderConfigs()
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": quota.NewClaudeProvider(caller, config.ClaudeUsage, config.ClaudeProfile), "codex": quota.NewCodexProvider(caller, config.Codex)}))
	if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", "claude").Updates(map[string]any{"type": "codex", "provider": "codex"}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := service.Reset(context.Background(), claudeManualReset())
	if err != nil || result.Code != "unavailable" || len(caller.posts()) != 0 {
		t.Fatalf("Claude popup spent Codex credit: %+v %v", result, err)
	}
	result, err = service.Reset(context.Background(), quota.ResetRequest{AuthIndex: "claude"})
	if err != nil || result.Code != "reset" || len(caller.posts()) != 1 || caller.posts()[0].URL != quota.CodexRateLimitResetCreditsConsumeURL {
		t.Fatalf("Codex changed: %+v %v", result, err)
	}
}

func TestClaudeResetPopupMissingOrganizationCannotConfirm(t *testing.T) {
	for _, caller := range []*claudeResetCaller{{org: "malformed"}, {profileErr: errors.New("profile unavailable")}} {
		service, _ := claudeResetFixture(t, caller)
		details, err := service.GetClaudeResetGrants(context.Background(), "claude")
		if err != nil || details.OrganizationID != "" || details.Status == nil || len(caller.requests) != 2 {
			t.Fatalf("profile failure lost usage or supplied org: %+v %v", details, err)
		}
	}
}

func TestClaudeResetPopupOnlyListsUsableGrantsAndCountsTheirRemainingUses(t *testing.T) {
	caller := &claudeResetCaller{grantBlock: `{"eligible":true,"at_limit":true,"grants":[
 {"id":"ready","resets_total":3,"resets_left":2,"usable_now":true,"clears":["five_hour"]},
 {"id":"paused","resets_total":3,"resets_left":3,"usable_now":true,"paused":true},
 {"id":"future","resets_total":2,"resets_left":2,"usable_now":true,"starts_at":"2099-01-01T00:00:00Z"},
 {"id":"expired","resets_total":1,"resets_left":1,"usable_now":true,"ends_at":"2000-01-01T00:00:00Z"},
 {"id":"notusable","resets_total":1,"resets_left":1,"usable_now":false},
 {"id":"empty","resets_total":1,"resets_left":0,"usable_now":true}]}`}
	service, _ := claudeResetFixture(t, caller)
	raw, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "claude"})
	if err != nil || raw.ClaudeResetGrants.AvailableCount != 9 || len(raw.ClaudeResetGrants.Grants) != 6 {
		t.Fatal("ordinary quota no longer retains original grants", err)
	}
	details, err := service.GetClaudeResetGrants(context.Background(), "claude")
	if err != nil || details.SelectedGrantID != "ready" || details.Status.AvailableCount != 2 || len(details.Status.Grants) != 1 {
		t.Fatalf("popup: %+v %v", details, err)
	}
	for _, block := range []string{
		strings.Replace(caller.grantBlock, `"eligible":true`, `"eligible":false`, 1),
		strings.Replace(caller.grantBlock, `"at_limit":true`, `"at_limit":false`, 1),
		strings.Replace(caller.grantBlock, `"at_limit":true`, `"at_limit":true,"cooldown_until":"2099-01-01T00:00:00Z"`, 1),
	} {
		caller.grantBlock = block
		details, err := service.GetClaudeResetGrants(context.Background(), "claude")
		if err != nil || details.SelectedGrantID != "" || details.Status.AvailableCount != 0 || len(details.Status.Grants) != 0 {
			t.Fatalf("unavailable grant listed: %+v %v", details, err)
		}
	}
}
