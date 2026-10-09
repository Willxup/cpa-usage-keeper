package test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

func TestUsageQueryFilterDoesNotExposeRawSourceFilter(t *testing.T) {
	if _, exists := reflect.TypeFor[dto.UsageQueryFilter]().FieldByName("Source"); exists {
		t.Fatal("usage queries must select identities through auth_index")
	}
}

func TestRequestEventListAndExportStreamUseLimitedRowProjection(t *testing.T) {
	db := openTestDatabase(t)
	modelAlias := "model-alias"
	parentSessionID := "parent-1"
	ttft := int64(25)
	if err := db.Create(&entities.UsageEvent{
		EventKey: "projection-event", RequestID: "request-1", SessionID: "session-1", ParentSessionID: &parentSessionID,
		Model: "model-a", ModelAlias: &modelAlias, Timestamp: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
		TTFTMS: &ttft, InputTokens: 10, OutputTokens: 5, CacheCreationTokens: 2, TotalTokens: 15,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	queries := captureUsageEventRowQueries(t, db)

	page, err := repository.ListUsageEventsWithFilter(db, dto.UsageQueryFilter{PageSize: 10, SkipTotalCount: true}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].RequestID != "request-1" || page.Events[0].ModelAlias != modelAlias ||
		page.Events[0].TTFTMS == nil || *page.Events[0].TTFTMS != ttft || page.Events[0].CacheCreationTokens != 2 {
		t.Fatalf("unexpected projected list row: %+v", page.Events)
	}

	streamed := 0
	if err := repository.StreamUsageEventsWithFilter(db, dto.UsageQueryFilter{}, func(record dto.UsageEventRecord) error {
		streamed++
		return nil
	}, emptyPricingResolverForTest()); err != nil {
		t.Fatalf("StreamUsageEventsWithFilter: %v", err)
	}
	if streamed != 1 {
		t.Fatalf("streamed rows = %d, want 1", streamed)
	}

	// 列表和导出都必须走逐行游标，并且不能读取只用于持久化的会话字段。
	if len(*queries) != 2 {
		t.Fatalf("usage event row queries = %#v, want list and export stream queries", *queries)
	}
	for _, query := range *queries {
		columns := analysisProjectionSelectColumns(t, query)
		for _, required := range []string{"id", "request_id", "model_alias", "ttft_ms", "cache_creation_tokens"} {
			if !slices.Contains(columns, required) {
				t.Fatalf("usage event projection misses %q: %s", required, query)
			}
		}
		for _, unused := range []string{"*", "event_key", "session_id", "parent_session_id", "created_at"} {
			if slices.Contains(columns, unused) {
				t.Fatalf("usage event projection includes storage-only column %q: %s", unused, query)
			}
		}
	}
}

func captureUsageEventRowQueries(t *testing.T, db *gorm.DB) *[]string {
	t.Helper()
	queries := make([]string, 0, 2)
	const callbackName = "test:capture-usage-event-row-projection"
	if err := db.Callback().Row().After("gorm:row").Register(callbackName, func(tx *gorm.DB) {
		query := strings.ToLower(strings.Join(strings.Fields(tx.Statement.SQL.String()), " "))
		if strings.Contains(query, " from `usage_events`") {
			queries = append(queries, query)
		}
	}); err != nil {
		t.Fatalf("register row callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Row().Remove(callbackName) })
	return &queries
}
