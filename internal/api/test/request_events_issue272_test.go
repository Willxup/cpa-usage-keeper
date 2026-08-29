package test

// 本文件验证 Issue #272 从 Redis 入站到 Request Events speed_tps 的完整回归链路。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func TestIssue272RedisIngressKeepsRequestEventSpeedTPS(t *testing.T) {
	db := openIssue272TokenProcessorDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
			"latency_ms":2045,
			"ttft_ms":45,
			"provider":"OpenAI Compatible",
			"auth_type":"api_key",
			"auth_index":"issue-272-auth",
			"model":"gemini-2.5-pro",
			"request_id":"issue-272-speed-regression",
			"executor_type":"OpenAICompatExecutor",
			"tokens":{"input_tokens":1000,"output_tokens":20,"reasoning_tokens":50,"total_tokens":1070}
		}`,
		PoppedAt: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed issue #272 inbox row: %v", err)
	}

	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one normalized issue #272 event, got %+v", result)
	}

	// Request Events 使用归一化后的完整 Output=70 计算速度，不再扣除 Reasoning=50。
	router := NewRouter(nil, nil, service.NewUsageService(db, emptyPricingCatalogForTest()), nil, AuthConfig{}, nil, "")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected Request Events status 200, got %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"request_id":"issue-272-speed-regression"`) || !strings.Contains(body, `"output_tokens":70`) || !strings.Contains(body, `"reasoning_tokens":50`) || !strings.Contains(body, `"speed_tps":35`) {
		t.Fatalf("expected issue #272 event to use full output tokens for speed, got %s", body)
	}
}

func openIssue272TokenProcessorDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "issue-272-tokenprocessor.db")})
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
