package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

func TestAntigravityActiveRefreshRecordsFourPoolWindowsAndIgnoresLegacyArgon(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		Identity:  "ag-auth",
		Provider:  "antigravity",
		Type:      "antigravity",
		AuthType:  entities.UsageIdentityAuthTypeCredential,
		ProjectID: new("project-ag"),
	})

	now := time.Now().Truncate(time.Second)
	fiveReset := now.Add(4 * time.Hour).Format(time.RFC3339)
	weeklyReset := now.Add(5 * 24 * time.Hour).Format(time.RFC3339)
	handler := &recordingProviderHandler{output: quota.ProviderOutput{
		Provider: "antigravity",
		Result: quota.AntigravityResult{Quota: &quota.AntigravityQuotaPayload{Groups: []quota.AntigravityQuotaGroup{
			{
				// 使用显示名变体确认与实时配额条同口径归一化。
				DisplayName: "Gemini models",
				Buckets: []quota.AntigravityQuotaBucket{
					// <0.5% 小额真实消耗（remainingFraction=0.9985 -> ZeroUsage=false）应建立 100% 首段周期。
					{BucketID: "gemini-5h", Window: "5h", RemainingFraction: new(0.9985), ResetTime: fiveReset},
					{BucketID: "gemini-weekly", Window: "weekly", RemainingFraction: new(0.82), ResetTime: weeklyReset},
				},
			},
			{
				DisplayName: "Claude & GPT Models",
				Buckets: []quota.AntigravityQuotaBucket{
					{BucketID: "3p-5h", Window: "five_hour", RemainingFraction: new(0.65), ResetTime: fiveReset},
					{BucketID: "3p-weekly", Window: "week", RemainingFraction: new(0.50), ResetTime: weeklyReset},
				},
			},
			{
				// 已下线旧 Argon 组必须静默忽略，不得建周期或与 Gemini 池冲突。
				DisplayName: "Gemini 4 Argon",
				Buckets: []quota.AntigravityQuotaBucket{
					{BucketID: "gemini-argon-wtu-weekly", Window: "weekly", RemainingFraction: new(0.44), ResetTime: weeklyReset},
				},
			},
		}}},
	}}
	service := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"antigravity": handler}))

	if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "ag-auth", Source: quota.RefreshSourceManual}); err != nil {
		service.StopRefreshTasks()
		t.Fatalf("antigravity active refresh failed: %v", err)
	}
	service.StopRefreshTasks()

	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ? AND auth_index = ?", "antigravity", "ag-auth").Order("quota_key ASC").Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 4 {
		t.Fatalf("expected 4 Antigravity pool cycles, got %+v", cycles)
	}

	// 查询详情历史：验证 4 个窗口按注册表顺序返回、携带 GroupKey/GroupLabel、支持切换与非法角色拦截。
	readService := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(nil))
	defer readService.StopRefreshTasks()
	history, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex: "ag-auth",
		Now:       now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("get Antigravity quota history: %v", err)
	}
	if len(history.Windows) != 4 {
		t.Fatalf("expected 4 windows, got %+v", history.Windows)
	}
	wantRoles := []struct {
		role, groupKey, groupLabel string
	}{
		{"gemini_5h", repositorydto.AntigravityGeminiGroupKey, "Gemini"},
		{"gemini_weekly", repositorydto.AntigravityGeminiGroupKey, "Gemini"},
		{"claude_gpt_5h", repositorydto.AntigravityClaudeGPTGroupKey, "Claude & GPT"},
		{"claude_gpt_weekly", repositorydto.AntigravityClaudeGPTGroupKey, "Claude & GPT"},
	}
	for i, want := range wantRoles {
		got := history.Windows[i]
		if got.WindowRole != want.role || got.GroupKey != want.groupKey || got.GroupLabel != want.groupLabel {
			t.Fatalf("window[%d] = %+v, want %+v", i, got, want)
		}
	}
	if history.SelectedWindow == nil || history.SelectedWindow.WindowRole != "gemini_5h" {
		t.Fatalf("expected default selected window gemini_5h, got %+v", history.SelectedWindow)
	}
	if len(history.Cycles) != 1 || history.Cycles[0].LastRemainingPercent == nil || *history.Cycles[0].LastRemainingPercent != 100 {
		t.Fatalf("expected fractional <0.5%% usage to persist 100%% start segment, got %+v", history.Cycles)
	}

	// 显式切换到 claude_gpt_weekly，并校验 Codex 的 primary 角色会被拒绝。
	claudeWeeklyRole := "claude_gpt_weekly"
	selectedHistory, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex:  "ag-auth",
		WindowRole: &claudeWeeklyRole,
		Now:        now.Add(time.Minute),
	})
	if err != nil || selectedHistory.SelectedWindow == nil || selectedHistory.SelectedWindow.WindowRole != "claude_gpt_weekly" ||
		len(selectedHistory.Cycles) != 1 || selectedHistory.Cycles[0].LastRemainingPercent == nil || *selectedHistory.Cycles[0].LastRemainingPercent != 50 {
		t.Fatalf("unexpected claude_gpt_weekly history: resp=%+v err=%v", selectedHistory, err)
	}
	invalidRole := "primary"
	if _, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex:  "ag-auth",
		WindowRole: &invalidRole,
		Now:        now.Add(time.Minute),
	}); !errors.Is(err, quota.ErrValidation) {
		t.Fatalf("expected primary role rejected for Antigravity with ErrValidation, got %v", err)
	}

	// 删除单个池窗口周期，其余 3 个池窗口不受影响。
	if err := readService.DeleteCodexQuotaHistoryCycle(context.Background(), "ag-auth", selectedHistory.Cycles[0].ID); err != nil {
		t.Fatalf("delete Antigravity cycle: %v", err)
	}
	afterDelete, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex: "ag-auth",
		Now:       now.Add(time.Minute),
	})
	if err != nil || len(afterDelete.Windows) != 3 {
		t.Fatalf("expected 3 remaining Antigravity windows after delete, got resp=%+v err=%v", afterDelete, err)
	}
}

func TestAntigravityActiveRefreshSkipsUnstartedFullBucketsWithSlidingReset(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		Identity:  "ag-idle",
		Provider:  "antigravity",
		Type:      "antigravity",
		AuthType:  entities.UsageIdentityAuthTypeCredential,
		ProjectID: new("project-idle"),
	})
	handler := &recordingProviderHandler{}
	service := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"antigravity": handler}))

	baseReset := time.Now().Add(5 * time.Hour).Truncate(time.Second)
	for step, source := range []quota.RefreshSource{quota.RefreshSourceManual, quota.RefreshSourceScheduled, quota.RefreshSourceInspection} {
		slidingFive := baseReset.Add(time.Duration(step*5) * time.Minute).Format(time.RFC3339)
		slidingWeekly := baseReset.Add(7*24*time.Hour + time.Duration(step*5)*time.Minute).Format(time.RFC3339)
		handler.output = quota.ProviderOutput{
			Provider: "antigravity",
			Result: quota.AntigravityResult{Quota: &quota.AntigravityQuotaPayload{Groups: []quota.AntigravityQuotaGroup{
				{
					DisplayName: "Gemini Models",
					Buckets: []quota.AntigravityQuotaBucket{
						{BucketID: "gemini-5h", Window: "5h", RemainingFraction: new(1.0), ResetTime: slidingFive},
						{BucketID: "gemini-weekly", Window: "weekly", RemainingFraction: new(1.0), ResetTime: slidingWeekly},
					},
				},
				{
					DisplayName: "Claude and GPT models",
					Buckets: []quota.AntigravityQuotaBucket{
						{BucketID: "3p-5h", Window: "5h", RemainingFraction: new(1.0), ResetTime: slidingFive},
						{BucketID: "3p-weekly", Window: "weekly", RemainingFraction: new(1.0), ResetTime: slidingWeekly},
					},
				},
			}}},
		}
		if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "ag-idle", Source: source}); err != nil {
			service.StopRefreshTasks()
			t.Fatalf("antigravity idle refresh step %d failed: %v", step, err)
		}
	}
	service.StopRefreshTasks()

	var count int64
	if err := db.Model(&entities.QuotaCycle{}).Where("provider = ?", "antigravity").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 cycles for unstarted remainingFraction>=1 Antigravity refreshes, got %d", count)
	}
}
