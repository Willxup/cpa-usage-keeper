package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestXAIActiveRefreshRecordsWeeklyBillingHistoryAndSupportsDeletion(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		Identity: "xai-auth",
		Provider: "xai",
		Type:     "xai",
		AuthType: entities.UsageIdentityAuthTypeCredential,
	})

	now := time.Now().Truncate(time.Second)
	weeklyEnd := now.Add(5 * 24 * time.Hour).Format(time.RFC3339)
	handler := &recordingProviderHandler{output: quota.ProviderOutput{
		Provider: "xai",
		Result: quota.XAIResult{
			Weekly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
				CurrentPeriod:      &quota.XAIBillingPeriod{Type: "weekly", End: weeklyEnd},
				CreditUsagePercent: floatPtr(0.2), // 小数消耗 <0.5% 应正常建 100% 起点周期
			}},
		},
	}}
	service := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"xai": handler}))
	if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "xai-auth", Source: quota.RefreshSourceManual}); err != nil {
		service.StopRefreshTasks()
		t.Fatalf("xai first refresh failed: %v", err)
	}

	// 同周期后续消耗下降到 37.4% 已用（四舍五入后剩余 63%）。
	handler.output = quota.ProviderOutput{
		Provider: "xai",
		Result: quota.XAIResult{
			Weekly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
				BillingPeriodEnd:   weeklyEnd,
				CreditUsagePercent: floatPtr(37.4),
			}},
		},
	}
	if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "xai-auth", Source: quota.RefreshSourceScheduled}); err != nil {
		service.StopRefreshTasks()
		t.Fatalf("xai second refresh failed: %v", err)
	}
	service.StopRefreshTasks()

	readService := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(nil))
	defer readService.StopRefreshTasks()
	history, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex: "xai-auth",
		Now:       now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("get xAI quota history: %v", err)
	}
	if len(history.Windows) != 1 || history.SelectedWindow == nil || history.SelectedWindow.WindowRole != "weekly" ||
		history.SelectedWindow.WindowSeconds != 604800 || history.SelectedWindow.GroupKey != "" || history.SelectedWindow.GroupLabel != "" {
		t.Fatalf("unexpected xAI windows: %+v selected=%+v", history.Windows, history.SelectedWindow)
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "group_key") || strings.Contains(string(encoded), "group_label") {
		t.Fatalf("single-pool xAI window must omit group_key/group_label: %s", encoded)
	}
	if len(history.Cycles) != 1 || history.Cycles[0].FirstRemainingPercent == nil || *history.Cycles[0].FirstRemainingPercent != 100 ||
		history.Cycles[0].LastRemainingPercent == nil || *history.Cycles[0].LastRemainingPercent != 63 {
		t.Fatalf("unexpected xAI cycle percents: %+v", history.Cycles)
	}

	// 显式选择 weekly 成功，非法角色 primary 被拒绝。
	weeklyRole := "weekly"
	if _, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex: "xai-auth", WindowRole: &weeklyRole, Now: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("explicit weekly role should succeed: %v", err)
	}
	invalidRole := "primary"
	if _, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex: "xai-auth", WindowRole: &invalidRole, Now: now.Add(time.Minute),
	}); !errors.Is(err, quota.ErrValidation) {
		t.Fatalf("expected primary role rejected for xAI with ErrValidation, got %v", err)
	}

	// 删除周期成功。
	if err := readService.DeleteCodexQuotaHistoryCycle(context.Background(), "xai-auth", history.Cycles[0].ID); err != nil {
		t.Fatalf("delete xAI cycle: %v", err)
	}
	afterDelete, err := readService.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{
		AuthIndex: "xai-auth", Now: now.Add(time.Minute),
	})
	if err != nil || len(afterDelete.Windows) != 0 || len(afterDelete.Cycles) != 0 {
		t.Fatalf("expected empty xAI history after delete, got resp=%+v err=%v", afterDelete, err)
	}
}

func TestXAIActiveRefreshSkipsZeroUsageAndMissingTotalPercent(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		Identity: "xai-skip",
		Provider: "xai",
		Type:     "xai",
		AuthType: entities.UsageIdentityAuthTypeCredential,
	})

	baseReset := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	handler := &recordingProviderHandler{}
	service := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"xai": handler}))

	// 1. 仅返回 productUsage 而缺失总 CreditUsagePercent 时，不得伪造历史观测。
	handler.output = quota.ProviderOutput{
		Provider: "xai",
		Result: quota.XAIResult{Weekly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
			CurrentPeriod: &quota.XAIBillingPeriod{Type: "weekly", End: baseReset.Format(time.RFC3339)},
			ProductUsage:  []quota.XAIBillingProductUsage{{Product: "GrokBuild", UsagePercent: floatPtr(14)}},
		}}},
	}
	if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "xai-skip", Source: quota.RefreshSourceManual}); err != nil {
		service.StopRefreshTasks()
		t.Fatalf("xai partial breakdown refresh failed: %v", err)
	}

	// 2. 空闲未启动 0% 连续滑动刷新也不得创建空周期。
	for step, source := range []quota.RefreshSource{quota.RefreshSourceManual, quota.RefreshSourceScheduled, quota.RefreshSourceInspection} {
		handler.output = quota.ProviderOutput{
			Provider: "xai",
			Result: quota.XAIResult{Weekly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
				CurrentPeriod:      &quota.XAIBillingPeriod{Type: "weekly", End: baseReset.Add(time.Duration(step*5) * time.Minute).Format(time.RFC3339)},
				CreditUsagePercent: floatPtr(0),
			}}},
		}
		if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "xai-skip", Source: source}); err != nil {
			service.StopRefreshTasks()
			t.Fatalf("xai idle refresh step %d failed: %v", step, err)
		}
	}
	// 3. 非正纪元重置时间（如 1970-01-01T00:00:00Z）即使存在消耗也必须跳过。
	handler.output = quota.ProviderOutput{
		Provider: "xai",
		Result: quota.XAIResult{Weekly: &quota.XAIBillingPayload{Config: &quota.XAIBillingConfig{
			CurrentPeriod:      &quota.XAIBillingPeriod{Type: "weekly", End: "1970-01-01T00:00:00Z"},
			CreditUsagePercent: floatPtr(25),
		}}},
	}
	if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "xai-skip", Source: quota.RefreshSourceManual}); err != nil {
		service.StopRefreshTasks()
		t.Fatalf("xai epoch-zero refresh failed: %v", err)
	}
	service.StopRefreshTasks()

	var count int64
	if err := db.Model(&entities.QuotaCycle{}).Where("provider = ?", "xai").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 cycles for missing total percent and zero-usage xAI refreshes, got %d", count)
	}
}

type delayedMonthlyXAICaller struct {
	weeklyBody   string
	weeklyDoneCh chan struct{}
	monthlyEnter chan struct{}
	monthlyAllow chan struct{}
}

func (c *delayedMonthlyXAICaller) CallManagementAPI(_ context.Context, req apicall.Request) (*apicall.Response, error) {
	if strings.Contains(req.URL, "format=credits") {
		resp := quotaAPIResponse(200, c.weeklyBody)
		c.weeklyDoneCh <- struct{}{}
		return resp, nil
	}
	c.monthlyEnter <- struct{}{}
	<-c.monthlyAllow
	return quotaAPIResponse(200, `{"config":{"monthlyLimit":{"val":20000},"used":{"val":100},"billingPeriodEnd":"2026-12-01T00:00:00Z"}}`), nil
}

func TestXAIActiveRefreshFixesObservedAtBeforeSlowMonthlyFinishes(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		Identity: "xai-obs-time",
		Provider: "xai",
		Type:     "xai",
		AuthType: entities.UsageIdentityAuthTypeCredential,
	})

	weeklyEnd := time.Now().Add(5 * 24 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)
	caller := &delayedMonthlyXAICaller{
		weeklyBody:   `{"config":{"currentPeriod":{"type":"weekly","end":"` + weeklyEnd + `"},"creditUsagePercent":15}}`,
		weeklyDoneCh: make(chan struct{}, 1),
		monthlyEnter: make(chan struct{}, 1),
		monthlyAllow: make(chan struct{}),
	}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)
	service := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"xai": provider}))

	done := make(chan error, 1)
	go func() {
		_, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "xai-obs-time", Source: quota.RefreshSourceManual})
		done <- err
	}()
	<-caller.monthlyEnter
	<-caller.weeklyDoneCh
	time.Sleep(120 * time.Millisecond)
	releasedAt := time.Now()
	close(caller.monthlyAllow)
	if err := <-done; err != nil {
		service.StopRefreshTasks()
		t.Fatalf("xai check failed: %v", err)
	}
	service.StopRefreshTasks()

	var cycle entities.QuotaCycle
	if err := db.Where("provider = ? AND auth_index = ?", "xai", "xai-obs-time").Take(&cycle).Error; err != nil {
		t.Fatal(err)
	}
	if !cycle.FirstObservedAt.Before(releasedAt) {
		t.Fatalf("expected FirstObservedAt (%s) captured before slow monthly billing release (%s)", cycle.FirstObservedAt, releasedAt)
	}
}
