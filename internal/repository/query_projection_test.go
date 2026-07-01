package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryQueriesAvoidKnownFullEntityReads(t *testing.T) {
	assertFileDoesNotContain(t, "usage.go",
		"var events []entities.UsageEvent\n\tif err := query.Find(&events)",
		"var events []entities.UsageEvent\n\tif err := db.Find(&events)",
		"Select(usageOverviewRealtimeProjectionColumns)",
		"loadUsageOverviewRealtimeEventsWithFilter",
	)
	assertFileContains(t, "usage.go",
		"Select(usageEventProjectionColumns).Order(\"timestamp DESC, id DESC\")",
		"Select(usageOverviewRawEventProjectionColumns).\n\t\tOrder(\"timestamp asc\")",
		"usageOverviewRawEventProjectionColumns = \"api_group_key, provider, auth_type, model, model_alias, timestamp, source, auth_index, failed, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens\"",
	)
	assertFileContains(t, "usage_recent_event_cache.go",
		"Select(\"api_group_key, provider, auth_type, model, model_alias, timestamp, source, auth_index, failed, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens\")",
	)

	assertFileDoesNotContain(t, "usage_identities.go",
		"db.WithContext(ctx).Find(&identities)",
	)
	assertFileContains(t, "usage_identities.go",
		"Select(usageIdentityReadColumns)",
		"Select(usageIdentityAggregationColumns)",
		"Select(\"timestamp\").Where(\"id > ?\", identity.LastAggregatedUsageEventID).Order(\"timestamp asc, id asc\").First(&firstEvent)",
		"Select(\"timestamp\").Where(\"id > ?\", identity.LastAggregatedUsageEventID).Order(\"timestamp desc, id desc\").First(&lastEvent)",
	)

	assertFileContains(t, "redis_usage_inbox.go",
		"Select(redisUsageInboxProcessingColumns).Where(\"status = ? OR status = ?\"",
		"Select(redisUsageInboxProcessingColumns).Where(\"status = ?\"",
	)
	assertFileContains(t, "pricing.go",
		"Select(modelPriceSettingColumns)",
	)
}

func TestUsageQueryFilterDoesNotExposeRawSourceFilter(t *testing.T) {
	dtoContent := readRepositorySourceFile(t, "dto/usage_query_filter.go")
	if strings.Contains(dtoContent, "Source          string") || strings.Contains(dtoContent, "Source string") {
		t.Fatalf("repository UsageQueryFilter should query identities via auth_index, not expose raw Source")
	}

	serviceContent := readServiceSourceFile(t, "usage.go")
	if strings.Contains(serviceContent, "Source:      filter.Source") {
		t.Fatalf("service should not pass Request Events source through to repository queries")
	}
}

func assertFileContains(t *testing.T, name string, snippets ...string) {
	t.Helper()
	content := readRepositorySourceFile(t, name)
	for _, snippet := range snippets {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %s to contain %q", name, snippet)
		}
	}
}

func assertFileDoesNotContain(t *testing.T, name string, snippets ...string) {
	t.Helper()
	content := readRepositorySourceFile(t, name)
	for _, snippet := range snippets {
		if strings.Contains(content, snippet) {
			t.Fatalf("expected %s not to contain %q", name, snippet)
		}
	}
}

func readRepositorySourceFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return normalizeSourceForSnippetAssertions(content)
}

func readServiceSourceFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "service", name))
	if err != nil {
		t.Fatalf("read service %s: %v", name, err)
	}
	return normalizeSourceForSnippetAssertions(content)
}

func normalizeSourceForSnippetAssertions(content []byte) string {
	// Windows CI 可能以 CRLF 读取源码，投影断言统一按 LF 片段匹配。
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}
