package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func TestProcessRedisUsageInboxNormalizesOpenAICompatibilityTokensForOpenAIIdentity(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	// 固定 unknown executor 必须进入 identity fallback；查询计数用于防止测试被 executor-first 路径悄悄绕过。
	identityLookupCount := registerTokenIdentityTypeLookupCallback(t, db, nil)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "OpenAI Compatible",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "openai-compat-auth-index",
		Type:         "openai",
		Provider:     "OpenAI Compatible",
	}).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
			"timestamp":"2026-07-06T08:00:00Z",
			"provider":"OpenAI Compatible",
			"auth_type":"api_key",
			"auth_index":"openai-compat-auth-index",
			"model":"gemini-2.5-pro",
			"request_id":"openai-compatible-gemini-thinking",
			"executor_type":"unknown",
			"tokens":{
				"input_tokens":11,
				"output_tokens":7,
				"reasoning_tokens":3,
				"cached_tokens":5,
				"cache_read_tokens":4,
				"cache_creation_tokens":2,
				"total_tokens":21
			}
		}`,
		PoppedAt: time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one inserted event, got %+v", result)
	}
	if *identityLookupCount == 0 {
		t.Fatal("expected OpenAI compatibility normalization to query the identity fallback")
	}
	event := loadOpenAITokenNormalizationEvent(t, db, "openai-compatible-gemini-thinking")
	if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.CacheReadTokens != 4 || event.CacheCreationTokens != 2 || event.TotalTokens != 21 {
		t.Fatalf("expected openai identity to normalize separated reasoning into output, got %+v", event)
	}
}

func TestProcessRedisUsageInboxBackfillsCodexCacheReadFromCachedTokens(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Codex",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "codex-auth-index",
		Type:         "codex",
		Provider:     "OpenAI",
	}).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
			"timestamp":"2026-07-06T08:00:00Z",
			"provider":"OpenAI",
			"auth_type":"api_key",
			"auth_index":"codex-auth-index",
			"model":"gpt-5.6-terra",
			"request_id":"codex-cache-read-fallback",
			"executor_type":"CodexExecutor",
			"tokens":{
				"input_tokens":100,
				"output_tokens":20,
				"cached_tokens":30,
				"cache_read_tokens":0,
				"cache_creation_tokens":10,
				"total_tokens":120
			}
		}`,
		PoppedAt: time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one inserted event, got %+v", result)
	}
	event := loadOpenAITokenNormalizationEvent(t, db, "codex-cache-read-fallback")
	if event.CachedTokens != 30 || event.CacheReadTokens != 30 || event.CacheCreationTokens != 10 {
		t.Fatalf("expected Codex cached tokens to backfill cache read while preserving write, got %+v", event)
	}
}

func TestProcessRedisUsageInboxPreservesCanonicalZeroCacheRead(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
			"timestamp":"2026-07-15T08:00:00Z",
			"provider":"OpenAI Compatible",
			"auth_type":"api_key",
			"auth_index":"canonical-zero-auth-index",
			"model":"gpt-5.4",
			"request_id":"canonical-zero-cache-read",
			"executor_type":"OpenAICompatExecutor",
			"tokens":{
				"input_tokens":100,
				"output_tokens":20,
				"cached_tokens":30,
				"cache_read_tokens":0,
				"cache_read_tokens_present":true,
				"total_tokens":120
			}
		}`,
		PoppedAt: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one inserted event, got %+v", result)
	}
	event := loadOpenAITokenNormalizationEvent(t, db, "canonical-zero-cache-read")
	if event.CachedTokens != 30 || event.CacheReadTokens != 0 {
		t.Fatalf("expected canonical zero cache read to remain zero, got %+v", event)
	}
}

func TestProcessRedisUsageInboxBackfillsCustomCacheReadDespiteQueuePresence(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
			"timestamp":"2026-07-15T08:00:00Z",
			"provider":"External Provider",
			"auth_type":"api_key",
			"auth_index":"external-auth-index",
			"model":"external-model",
			"request_id":"custom-cache-read-fallback",
			"executor_type":"ExternalExecutor",
			"tokens":{
				"input_tokens":100,
				"output_tokens":20,
				"cached_tokens":30,
				"cache_read_tokens":0,
				"cache_read_tokens_present":true,
				"total_tokens":120
			}
		}`,
		PoppedAt: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one inserted event, got %+v", result)
	}
	event := loadOpenAITokenNormalizationEvent(t, db, "custom-cache-read-fallback")
	if event.CachedTokens != 30 || event.CacheReadTokens != 30 {
		t.Fatalf("expected custom executor cached tokens to backfill cache read despite queue presence, got %+v", event)
	}
}

func TestProcessRedisUsageInboxUsesDefaultTokensWhenUsageIdentityMissing(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
			"timestamp":"2026-07-06T08:00:00Z",
			"provider":"Unknown Provider",
			"auth_type":"api_key",
			"auth_index":"missing-auth-index",
			"model":"unknown-model",
			"request_id":"missing-identity-thinking",
			"tokens":{
				"input_tokens":11,
				"output_tokens":7,
				"reasoning_tokens":3,
				"cached_tokens":5,
				"total_tokens":21
			}
		}`,
		PoppedAt: time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one inserted event, got %+v", result)
	}
	event := loadOpenAITokenNormalizationEvent(t, db, "missing-identity-thinking")
	if event.InputTokens != 11 || event.OutputTokens != 7 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.CacheReadTokens != 5 || event.TotalTokens != 21 {
		t.Fatalf("expected missing identity to use default strict token normalization, got %+v", event)
	}
}

func openOpenAITokenNormalizationTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "openai-token-normalization.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}

func loadOpenAITokenNormalizationEvent(t *testing.T, db *gorm.DB, eventKey string) entities.UsageEvent {
	t.Helper()
	var event entities.UsageEvent
	if err := db.Where("event_key = ?", eventKey).First(&event).Error; err != nil {
		t.Fatalf("load usage event %q: %v", eventKey, err)
	}
	return event
}
