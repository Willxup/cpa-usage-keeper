package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestUsageActivityPayloadExposesCanonicalTokenMetricsWithoutCachedTokens(t *testing.T) {
	// 准备独立数据库和一个位于当前 short 窗口内的 Activity 稀疏行。
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-activity-payload.db")})
	if err != nil {
		t.Fatalf("open Activity payload database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("resolve Activity payload database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	bucket, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainShort, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("resolve Activity bucket: %v", err)
	}
	row := entities.UsageActivityStat{
		Grain:               entities.UsageActivityGrainShort,
		BucketStart:         bucket.Start,
		BucketEnd:           bucket.End,
		APIGroupKey:         "provider-a",
		SuccessCount:        2,
		FailureCount:        1,
		InputTokens:         100,
		OutputTokens:        40,
		ReasoningTokens:     10,
		CacheReadTokens:     20,
		CacheCreationTokens: 5,
		TotalTokens:         777,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed Activity payload row: %v", err)
	}

	// 执行真实路由，确保 repository、service 和 API 映射使用同一份 canonical 数据。
	router := NewRouter(nil, nil, service.NewUsageService(db), nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/activity?range=24h", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("Activity payload status=%d body=%s", response.Code, response.Body.String())
	}

	// 断言顶层字段精确且新链路完全不暴露 cached_tokens。
	if strings.Contains(response.Body.String(), "cached_tokens") {
		t.Fatalf("Activity payload must not expose cached_tokens: %s", response.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode Activity payload: %v", err)
	}
	assertUsageActivityJSONKeys(t, payload, []string{
		"blocks", "bucket_seconds", "cache_creation_tokens", "cache_read_tokens", "columns", "grain",
		"input_tokens", "output_tokens", "reasoning_tokens", "rows", "success_rate", "timezone",
		"total_failure", "total_success", "total_tokens", "window", "window_end", "window_start",
	})
	assertUsageActivityJSONInt(t, payload, "input_tokens", 100)
	assertUsageActivityJSONInt(t, payload, "output_tokens", 40)
	assertUsageActivityJSONInt(t, payload, "reasoning_tokens", 10)
	assertUsageActivityJSONInt(t, payload, "cache_read_tokens", 20)
	assertUsageActivityJSONInt(t, payload, "cache_creation_tokens", 5)
	// total_tokens 必须直接使用数据库 canonical total，不能由其它字段重新相加。
	assertUsageActivityJSONInt(t, payload, "total_tokens", 777)

	// 断言 364 个块都固定返回完整 Token 字段，包含有数据块和补零空块。
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(payload["blocks"], &blocks); err != nil {
		t.Fatalf("decode Activity blocks: %v", err)
	}
	if len(blocks) != repository.UsageActivityHeatmapBlocks {
		t.Fatalf("Activity blocks=%d, want %d", len(blocks), repository.UsageActivityHeatmapBlocks)
	}
	wantBlockKeys := []string{
		"cache_creation_tokens", "cache_read_tokens", "end_time", "failure", "input_tokens", "output_tokens",
		"rate", "reasoning_tokens", "start_time", "success", "total_tokens",
	}
	var populatedBlock map[string]json.RawMessage
	for _, block := range blocks {
		assertUsageActivityJSONKeys(t, block, wantBlockKeys)
		var totalTokens int64
		if err := json.Unmarshal(block["total_tokens"], &totalTokens); err != nil {
			t.Fatalf("decode block total_tokens: %v", err)
		}
		if totalTokens == 777 {
			populatedBlock = block
		}
	}
	if populatedBlock == nil {
		t.Fatal("Activity payload did not include the populated Token block")
	}
	assertUsageActivityJSONInt(t, populatedBlock, "input_tokens", 100)
	assertUsageActivityJSONInt(t, populatedBlock, "cache_read_tokens", 20)
}

func assertUsageActivityJSONKeys(t *testing.T, object map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("JSON keys=%v, want %v", got, want)
	}
}

func assertUsageActivityJSONInt(t *testing.T, object map[string]json.RawMessage, key string, want int64) {
	t.Helper()
	var got int64
	if err := json.Unmarshal(object[key], &got); err != nil {
		t.Fatalf("decode %s: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s=%d, want %d", key, got, want)
	}
}
