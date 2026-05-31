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
	)
	assertFileContains(t, "usage.go",
		"Select(usageEventProjectionColumns).Order(\"timestamp DESC, id DESC\")",
		"Select(usageEventProjectionColumns).Order(\"timestamp asc\")",
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
	return string(content)
}
