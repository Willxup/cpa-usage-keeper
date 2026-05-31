package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestPricingServiceAllowsModelWithoutUsage(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db)

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                   "claude-sonnet",
		PricingStyle:            "claude",
		PromptPricePer1M:        3,
		CompletionPricePer1M:    15,
		CachePricePer1M:         0.3,
		CacheCreationPricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "claude-sonnet" || setting.PricingStyle != "claude" || setting.CacheCreationPricePer1M != 3.75 {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

func TestPricingServiceStoresPricingForUsedModel(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-1",
		Model:       "claude-sonnet",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	service := NewPricingService(db)
	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                   "claude-sonnet",
		PricingStyle:            "claude",
		PromptPricePer1M:        3,
		CompletionPricePer1M:    15,
		CachePricePer1M:         0.3,
		CacheCreationPricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "claude-sonnet" || setting.PricingStyle != "claude" || setting.CompletionPricePer1M != 15 || setting.CacheCreationPricePer1M != 3.75 {
		t.Fatalf("unexpected setting: %#v", setting)
	}

	usedModels, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list used models: %v", err)
	}
	if len(usedModels) != 1 || usedModels[0] != "claude-sonnet" {
		t.Fatalf("unexpected used models: %#v", usedModels)
	}
}

func TestPricingServiceRejectsUnknownPricingStyle(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-style",
		Model:       "claude-sonnet",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	service := NewPricingService(db)

	_, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:        "claude-sonnet",
		PricingStyle: "legacy",
	})
	if err == nil || !strings.Contains(err.Error(), "pricing_style") {
		t.Fatalf("expected pricing style validation error, got %v", err)
	}
}

func TestPricingServiceListsModelsFromCPAWhenAvailable(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-local",
		Model:       "local-model",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	logs := captureDebugLogs(t)

	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
		{ID: " zeta-model "},
		{ID: "alpha-model"},
		{ID: "zeta-model"},
		{ID: ""},
	}}}})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}

	expected := []string{"alpha-model", "zeta-model"}
	if strings.Join(modelsList, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected CPA models %#v, got %#v", expected, modelsList)
	}
	if !strings.Contains(logs.String(), "using CPA models endpoint") {
		t.Fatalf("expected CPA source debug log, got %q", logs.String())
	}
}

func TestPricingServiceFallsBackToLocalModelsWhenCPAFetchFails(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-local",
		Model:       "local-model",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	logs := captureDebugLogs(t)

	service := NewPricingService(db, stubModelsFetcher{err: errors.New("cpa unavailable")})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}

	if len(modelsList) != 1 || modelsList[0] != "local-model" {
		t.Fatalf("expected local fallback model, got %#v", modelsList)
	}
	if !strings.Contains(logs.String(), "level=error") {
		t.Fatalf("expected fallback error log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "falling back to local usage aggregation") {
		t.Fatalf("expected fallback error log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "error=\"cpa unavailable\"") && !strings.Contains(logs.String(), "error=cpa unavailable") {
		t.Fatalf("expected fallback log to include original error, got %q", logs.String())
	}
}

func TestPricingServiceReturnsEmptyCPAListWithoutFallback(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-local",
		Model:       "local-model",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: " "}}}}})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(modelsList) != 0 {
		t.Fatalf("expected empty CPA model list, got %#v", modelsList)
	}
}

func TestPricingServiceAllowsPricingForCPAModelWithoutUsage(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "claude-opus"}}}}})

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                "claude-opus",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "claude-opus" {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

func TestPricingServiceAllowsModelOutsideCPAModelList(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "cpa-model"}}}}})

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                   "local-model",
		PricingStyle:            "claude",
		PromptPricePer1M:        3,
		CompletionPricePer1M:    15,
		CachePricePer1M:         0.3,
		CacheCreationPricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "local-model" || setting.PricingStyle != "claude" {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

func TestPricingServiceSavesPricingWhenCPAFetchFails(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db, stubModelsFetcher{err: errors.New("cpa unavailable")})

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                "any-model",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "any-model" {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

type stubModelsFetcher struct {
	result *response.ModelsResult
	err    error
}

func (s stubModelsFetcher) FetchModels(context.Context) (*response.ModelsResult, error) {
	return s.result, s.err
}

func captureDebugLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previousOutput := logrus.StandardLogger().Out
	previousLevel := logrus.GetLevel()
	var logs bytes.Buffer
	logrus.SetOutput(&logs)
	logrus.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func openPricingServiceTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "pricing-service.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}
