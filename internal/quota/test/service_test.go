package test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

type recordingProviderHandler struct {
	inputs []quota.ProviderInput
	output quota.ProviderOutput
	err    error
}

func (h *recordingProviderHandler) Check(ctx context.Context, input quota.ProviderInput) (quota.ProviderOutput, error) {
	h.inputs = append(h.inputs, input)
	if h.err != nil {
		return quota.ProviderOutput{}, h.err
	}
	return h.output, nil
}

func TestServiceRejectsEmptyAuthIndex(t *testing.T) {
	service := newQuotaServiceWithRegistry(t, openQuotaTestDB(t), quota.NewProviderRegistry(nil))

	_, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "   "})
	if !errors.Is(err, quota.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestServiceIgnoresProviderOnlyIdentity(t *testing.T) {
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "shared-auth", Type: "codex", Name: "provider"})
	handler := &recordingProviderHandler{}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"codex": handler}))

	_, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "shared-auth"})
	if !errors.Is(err, quota.ErrNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	if len(handler.inputs) != 0 {
		t.Fatalf("expected provider not to be called, got %d calls", len(handler.inputs))
	}
}

func TestServiceDispatchesAuthFileIdentityByProviderBeforeType(t *testing.T) {
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "codex-auth", Provider: "codex", Type: "unknown", Name: "auth file"})
	handler := &recordingProviderHandler{output: quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{RateLimit: &quota.CodexRateLimitInfo{PrimaryWindow: &quota.CodexUsageWindow{UsedPercent: 25, LimitWindowSeconds: 18000}}}}}}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"codex": handler}))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "codex-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.ID != "codex-auth" || len(response.Quota) != 1 || response.Quota[0].Key != "rate_limit.primary_window" || response.Quota[0].UsedPercent == nil || *response.Quota[0].UsedPercent != 25 {
		t.Fatalf("unexpected check response: %+v", response)
	}
	if len(handler.inputs) != 1 || handler.inputs[0].Identity.Identity != "codex-auth" || handler.inputs[0].Identity.AuthType != entities.UsageIdentityAuthTypeAuthFile {
		t.Fatalf("unexpected provider inputs: %+v", handler.inputs)
	}
}

func TestServicePrefersRealtimeCodexSubscriptionOverIdentityMetadata(t *testing.T) {
	db := openQuotaTestDB(t)
	identityPlan := "plus"
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "codex-auth", Provider: "codex", Type: "codex", Name: "auth file", PlanType: &identityPlan})
	handler := &recordingProviderHandler{output: quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{PlanType: "pro", RateLimit: &quota.CodexRateLimitInfo{}}}}}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"codex": handler}))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "codex-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Provider != "codex" || response.Subscription.Plan != "pro-20x" {
		t.Fatalf("expected realtime subscription to win, got %+v", response.Subscription)
	}
}

func TestServiceFallsBackToCodexIdentitySubscription(t *testing.T) {
	db := openQuotaTestDB(t)
	identityPlan := "team"
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "codex-auth", Provider: "codex", Type: "codex", Name: "auth file", PlanType: &identityPlan})
	handler := &recordingProviderHandler{output: quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{Usage: &quota.CodexUsagePayload{RateLimit: &quota.CodexRateLimitInfo{}}}}}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"codex": handler}))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "codex-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Provider != "codex" || response.Subscription.Plan != "team" {
		t.Fatalf("expected identity subscription fallback, got %+v", response.Subscription)
	}
}

func TestServicePublishesRealtimeClaudeSubscription(t *testing.T) {
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "claude-auth", Provider: "claude", Type: "claude", Name: "auth file"})
	handler := &recordingProviderHandler{output: quota.ProviderOutput{Provider: "claude", Result: quota.ClaudeResult{
		Usage:   &quota.ClaudeUsagePayload{FiveHour: &quota.ClaudeUsageWindow{Utilization: 25}},
		Profile: &quota.ClaudeProfileResponse{Account: &quota.ClaudeProfileAccount{HasClaudeMax: boolPtr(true)}},
	}}}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": handler}))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "claude-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Provider != "claude" || response.Subscription.Plan != "max" {
		t.Fatalf("expected realtime Claude subscription, got %+v", response.Subscription)
	}
}

func TestServicePublishesRealtimeAntigravitySubscription(t *testing.T) {
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "ag-auth", Provider: "antigravity", Type: "antigravity", Name: "auth file", ProjectID: stringPtr("project-123")})
	remaining := 0.5
	handler := &recordingProviderHandler{output: quota.ProviderOutput{Provider: "antigravity", Result: quota.AntigravityResult{
		Quota: &quota.AntigravityQuotaPayload{Groups: []quota.AntigravityQuotaGroup{{
			DisplayName: "Gemini Models",
			Buckets:     []quota.AntigravityQuotaBucket{{BucketID: "gemini-5h", RemainingFraction: &remaining}},
		}}},
		Subscription: &quota.AntigravitySubscriptionPayload{PaidTier: &quota.GeminiCliUserTier{ID: "g1-ultra-lite-tier", Name: "Ultra Lite"}},
	}}}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"antigravity": handler}))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "ag-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Provider != "antigravity" || response.Subscription.Plan != "ultra-lite" || response.Subscription.TierID != "g1-ultra-lite-tier" || response.Subscription.TierName != "Ultra Lite" {
		t.Fatalf("expected realtime Antigravity subscription, got %+v", response.Subscription)
	}
}

func TestServiceFallsBackToTypeWhenProviderMissing(t *testing.T) {
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "gemini-auth", Provider: "Gemini", Type: "gemini-cli", Name: "auth file"})
	handler := &recordingProviderHandler{output: quota.ProviderOutput{Provider: "gemini-cli", Result: quota.GeminiCLIResult{Quota: &quota.GeminiCliQuotaPayload{Buckets: []quota.GeminiCliQuotaBucket{{ModelID: "gemini-2.5-pro_vertex", TokenType: "PROMPT", RemainingAmount: 42}}}}}}
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"gemini-cli": handler}))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "gemini-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.ID != "gemini-auth" || len(response.Quota) != 1 || response.Quota[0].Key != "bucket.gemini-2.5-pro_vertex.PROMPT" {
		t.Fatalf("unexpected check response: %+v", response)
	}
	if len(handler.inputs) != 1 {
		t.Fatalf("unexpected provider inputs: %+v", handler.inputs)
	}
}

func TestServiceReturnsUnsupportedType(t *testing.T) {
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "unknown-auth", Type: "unknown", Name: "auth file"})
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(nil))

	_, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "unknown-auth"})
	if !errors.Is(err, quota.ErrUnsupportedType) {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestServiceAllowsCodexQuotaWithoutAccountID(t *testing.T) {
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "codex-auth", Type: "codex", Name: "auth file"})
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false}}`,
		Body:       json.RawMessage(`{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false}}`),
	}}}
	service := newQuotaServiceWithRegistry(t, db, quota.NewDefaultProviderRegistry(caller, quota.DefaultProviderConfigs()))

	response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "codex-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.ID != "codex-auth" || len(caller.requests) != 1 {
		t.Fatalf("expected codex quota request without account_id, got response=%+v requests=%d", response, len(caller.requests))
	}
	if response.Subscription == nil || response.Subscription.Provider != "codex" || response.Subscription.Plan != "plus" {
		t.Fatalf("expected codex subscription in check response, got %+v", response.Subscription)
	}
}

func newQuotaServiceWithRegistry(t *testing.T, db *gorm.DB, registry quota.ProviderRegistry) *quota.Service {
	t.Helper()
	service := quota.NewServiceWithRegistry(db, registry, emptyPricingCatalogForTest())
	t.Cleanup(service.StopRefreshTasks)
	return service
}

func newQuotaServiceWithRegistryAndOptions(t *testing.T, db *gorm.DB, registry quota.ProviderRegistry, options quota.ServiceOptions) *quota.Service {
	t.Helper()
	if options.PricingCatalog == nil {
		options.PricingCatalog = emptyPricingCatalogForTest()
	}
	service := quota.NewServiceWithRegistryAndOptions(db, registry, options)
	t.Cleanup(service.StopRefreshTasks)
	return service
}

func openQuotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "quota.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}

func seedUsageIdentity(t *testing.T, db *gorm.DB, identity entities.UsageIdentity) {
	t.Helper()
	if identity.Name == "" {
		identity.Name = identity.Identity
	}
	identity.CreatedAt = time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	identity.UpdatedAt = identity.CreatedAt
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
}
