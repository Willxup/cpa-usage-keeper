package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
)

func TestListUsageEventsWithFilterAppliesTimeBoundsAndPagination(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", APIGroupKey: "provider-b", Model: "claude-opus", ReasoningEffort: "high", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)
	page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 1}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 2 || page.TotalPages != 2 || page.Page != 1 || page.PageSize != 1 {
		t.Fatalf("unexpected pagination metadata: %+v", page)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected one row after page size, got %d", len(page.Events))
	}
	if page.Events[0].Source != "source-c" {
		t.Fatalf("expected newest in-range row first, got %+v", page.Events[0])
	}
	if page.Events[0].ReasoningEffort != "high" {
		t.Fatalf("expected reasoning effort to round trip, got %+v", page.Events[0])
	}
}

func TestListUsageEventsWithFilterFindsProjectTimezoneStorageTimestamp(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-project-tz.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	eventTime := time.Date(2026, 5, 12, 21, 59, 18, 353569620, location)
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "event-project-tz", Model: "claude-sonnet", Timestamp: eventTime, TotalTokens: 10}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 20}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 1 || len(page.Events) != 1 || page.Events[0].Model != "claude-sonnet" {
		t.Fatalf("expected project timezone timestamp to match UTC query window, got %+v", page)
	}
}

func TestListUsageEventsWithFilterPagesByTimestampAndID(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-pages.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	timestamp := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: timestamp, Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: timestamp, Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: timestamp.Add(-time.Hour), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	firstPage, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 1, PageSize: 1}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	secondPage, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 2, PageSize: 1}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if firstPage.TotalCount != 3 || firstPage.TotalPages != 3 || secondPage.TotalCount != 3 || secondPage.TotalPages != 3 {
		t.Fatalf("unexpected page metadata: first=%+v second=%+v", firstPage, secondPage)
	}
	if len(firstPage.Events) != 1 || len(secondPage.Events) != 1 {
		t.Fatalf("expected one event on each page: first=%+v second=%+v", firstPage, secondPage)
	}
	if firstPage.Events[0].ID <= secondPage.Events[0].ID {
		t.Fatalf("expected id desc tie-breaker, first=%+v second=%+v", firstPage.Events[0], secondPage.Events[0])
	}
}

func TestListUsageEventsWithFilterCursorPaginatesWithoutDuplicates(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-cursor.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	timestamp := time.Date(2026, 4, 16, 12, 0, 0, 123456789, time.UTC)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "model-1", Timestamp: timestamp, Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "model-2", Timestamp: timestamp, Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", APIGroupKey: "provider-a", Model: "model-3", Timestamp: timestamp.Add(-time.Second), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
		{EventKey: "event-4", APIGroupKey: "provider-b", Model: "model-4", Timestamp: timestamp.Add(-2 * time.Second), Source: "source-d", AuthIndex: "4", TotalTokens: 40},
		{EventKey: "event-5", APIGroupKey: "provider-b", Model: "model-5", Timestamp: timestamp.Add(-2 * time.Second), Source: "source-e", AuthIndex: "5", TotalTokens: 50},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	allRows, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 1, PageSize: 20}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("load expected ordering: %v", err)
	}
	var loadedIDs []int64
	filter := dto.UsageQueryFilter{CursorMode: true, PageSize: 2}
	for batch := 0; ; batch++ {
		page, err := ListUsageEventsWithFilter(db, filter, emptyPricingResolverForTest())
		if err != nil {
			t.Fatalf("load cursor batch %d: %v", batch, err)
		}
		if batch == 0 && page.TotalCount != int64(len(events)) {
			t.Fatalf("expected first cursor batch total count, got %+v", page)
		}
		if batch > 0 && page.TotalCount != -1 {
			t.Fatalf("expected continuation batch to skip total count, got %+v", page)
		}
		for _, event := range page.Events {
			loadedIDs = append(loadedIDs, event.ID)
		}
		if !page.HasMore {
			break
		}
		lastEvent := page.Events[len(page.Events)-1]
		cursorTimestamp := lastEvent.Timestamp
		filter.CursorTimestamp = &cursorTimestamp
		filter.CursorID = lastEvent.ID
	}

	if len(loadedIDs) != len(allRows.Events) {
		t.Fatalf("expected %d cursor rows, got %d: %+v", len(allRows.Events), len(loadedIDs), loadedIDs)
	}
	seen := make(map[int64]struct{}, len(loadedIDs))
	for index, id := range loadedIDs {
		if _, exists := seen[id]; exists {
			t.Fatalf("cursor pagination returned duplicate id %d: %+v", id, loadedIDs)
		}
		seen[id] = struct{}{}
		if id != allRows.Events[index].ID {
			t.Fatalf("cursor ordering mismatch at %d: got %d want %d", index, id, allRows.Events[index].ID)
		}
	}
}

func TestListUsageEventsWithFilterAppliesModelAuthIndexAndResultFilters(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-filtered.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "auth-a", Failed: false, TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "auth-a", Failed: true, TotalTokens: 20},
		{EventKey: "event-3", APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "auth-a", Failed: false, TotalTokens: 30},
		{EventKey: "event-4", APIGroupKey: "provider-c", Model: "gpt-5", Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "auth-b", Failed: false, TotalTokens: 40},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 1, PageSize: 20, Model: "claude-sonnet", AuthIndex: "auth-a", Result: "success"}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 1 || len(page.Events) != 1 {
		t.Fatalf("expected one matching event, got %+v", page)
	}
	if page.Events[0].Model != "claude-sonnet" || page.Events[0].Source != "source-a" || page.Events[0].Failed {
		t.Fatalf("unexpected filtered event: %+v", page.Events[0])
	}
}

func TestExportUsageEventsWithFilterAppliesFiltersWithoutPagination(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-export.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", ExecutorType: "responses", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "auth-a", Failed: false, TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", ExecutorType: "responses", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "auth-a", Failed: false, TotalTokens: 20},
		{EventKey: "event-3", APIGroupKey: "provider-a", Model: "claude-sonnet", ExecutorType: "chat_completions", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "auth-b", Failed: false, TotalTokens: 30},
		{EventKey: "event-4", APIGroupKey: "provider-a", Model: "claude-sonnet", ExecutorType: "responses", Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "auth-a", Failed: true, TotalTokens: 40},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	rows, err := ExportUsageEventsWithFilter(db, dto.UsageQueryFilter{Model: "claude-sonnet", AuthIndex: "auth-a", Result: "success", Page: 1, PageSize: 1, Limit: 1}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ExportUsageEventsWithFilter returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected export to ignore pagination and return two filtered rows, got %+v", rows)
	}
	if rows[0].TotalTokens != 20 || rows[1].TotalTokens != 10 {
		t.Fatalf("expected newest filtered rows first, got %+v", rows)
	}
	for _, row := range rows {
		if row.AuthIndex != "auth-a" || row.ExecutorType != "responses" || row.Failed {
			t.Fatalf("unexpected exported row: %+v", row)
		}
	}
}

func TestListUsageEventsWithFilterAddsBackendCost(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-cost.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PricingStyle:         entities.ModelPricingStyleClaude,
		PromptPricePer1M:     10,
		CompletionPricePer1M: 20,
		CacheReadPricePer1M:  1,
		CacheWritePricePer1M: 12.5,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:            "event-cost",
		APIGroupKey:         "provider-a",
		Model:               "claude-sonnet",
		Timestamp:           time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC),
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CachedTokens:        200_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
		TotalTokens:         1_800_000,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 1, PageSize: 20}, pricingResolverFromDBForTest(t, db))
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected one event, got %+v", page)
	}
	event := page.Events[0]
	wantCost := 1.0*10 + 0.5*20 + 0.2*1 + 0.1*12.5
	if math.Abs(event.CostUSD-wantCost) > 0.000000001 || !event.CostAvailable || event.PricingStyle != entities.ModelPricingStyleClaude {
		t.Fatalf("unexpected backend cost fields: %+v", event)
	}
}

func TestListUsageEventsWithFilterMarksCostUnavailableWhenPriceMissing(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-missing-cost.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "event-missing-cost",
		APIGroupKey: "provider-a",
		Model:       "missing-model",
		Timestamp:   time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC),
		InputTokens: 1,
		TotalTokens: 1,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 1, PageSize: 20}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].CostAvailable || page.Events[0].CostUSD != 0 {
		t.Fatalf("expected missing pricing to mark cost unavailable, got %+v", page.Events)
	}
}

func TestListUsageEventsWithFilterAppliesAuthIndexFilter(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-auth-filter.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	events := []entities.UsageEvent{
		{EventKey: "event-1", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "auth-1", AuthIndex: "auth-1", TotalTokens: 10},
		{EventKey: "event-2", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-alias", AuthIndex: "auth-1", TotalTokens: 20},
		{EventKey: "event-3", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "other", AuthIndex: "other", TotalTokens: 30},
		{EventKey: "event-4", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), Source: "auth-1", AuthIndex: "auth-1", Provider: "Provider A", TotalTokens: 40},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{AuthIndex: "auth-1", Page: 1, PageSize: 20}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 3 || len(page.Events) != 3 {
		t.Fatalf("expected three matching auth events, got %+v", page)
	}
	for _, event := range page.Events {
		if event.AuthIndex != "auth-1" {
			t.Fatalf("unexpected auth filtered event: %+v", event)
		}
	}
}

func TestListUsageEventsWithFilterAppliesAuthTypeWithSharedAuthIndex(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-auth-type-filter.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	events := []entities.UsageEvent{
		{EventKey: "oauth-shared", AuthType: "oauth", AuthIndex: "shared-auth", Model: "codex", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)},
		{EventKey: "apikey-shared", AuthType: "apikey", AuthIndex: "shared-auth", Model: "gpt-5", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{AuthIndex: "shared-auth", AuthType: "oauth", Page: 1, PageSize: 20}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 1 || len(page.Events) != 1 || page.Events[0].AuthType != "oauth" {
		t.Fatalf("expected only the auth-file event, got %+v", page)
	}
}

func TestListUsageEventFilterOptionsWithFilterReturnsStableModels(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-filter-options.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", Failed: false, TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-b", Failed: true, TotalTokens: 20},
		{EventKey: "event-3", APIGroupKey: "provider-b", Model: "gpt-5", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-a", Failed: false, TotalTokens: 30},
		{EventKey: "event-blank-model", APIGroupKey: "provider-c", Model: "   ", Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), Source: "source-c", Failed: false, TotalTokens: 40},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	options, err := ListUsageEventFilterOptionsWithFilter(db, dto.UsageQueryFilter{Result: "success"})
	if err != nil {
		t.Fatalf("ListUsageEventFilterOptionsWithFilter returned error: %v", err)
	}
	if len(options.Models) != 2 || options.Models[0] != "claude-sonnet" || options.Models[1] != "gpt-5" {
		t.Fatalf("expected stable model options, got %+v", options.Models)
	}
}

func TestListUsageEventFilterOptionsWithFilterSplitsAliasesIntoDistinctOptions(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-filter-options-alias.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	aliasA := "claude-sonnet-cfs"
	aliasB := "claude-sonnet-phoenix"
	events := []entities.UsageEvent{
		{EventKey: "a-1", APIGroupKey: "provider-a", Model: "claude-sonnet", ModelAlias: &aliasA, Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "a-2", APIGroupKey: "provider-a", Model: "claude-sonnet", ModelAlias: &aliasB, Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 20},
		{EventKey: "a-3", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), TotalTokens: 30},
		{EventKey: "b-1", APIGroupKey: "provider-b", Model: "gpt-5", Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), TotalTokens: 40},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	options, err := ListUsageEventFilterOptionsWithFilter(db, dto.UsageQueryFilter{})
	if err != nil {
		t.Fatalf("ListUsageEventFilterOptionsWithFilter returned error: %v", err)
	}
	want := []string{"claude-sonnet", "claude-sonnet-cfs", "claude-sonnet-phoenix", "gpt-5"}
	if len(options.Models) != len(want) {
		t.Fatalf("expected distinct alias options %+v, got %+v", want, options.Models)
	}
	for i := range want {
		if options.Models[i] != want[i] {
			t.Fatalf("expected model options %+v, got %+v", want, options.Models)
		}
	}
}

func TestListUsageEventsWithFilterFiltersByAliasDisplayValue(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-filter-by-alias.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	aliasA := "claude-sonnet-cfs"
	aliasB := "claude-sonnet-phoenix"
	events := []entities.UsageEvent{
		{EventKey: "a-1", APIGroupKey: "provider-a", Model: "claude-sonnet", ModelAlias: &aliasA, Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "a-2", APIGroupKey: "provider-a", Model: "claude-sonnet", ModelAlias: &aliasB, Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 20},
		{EventKey: "a-3", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), TotalTokens: 30},
		{EventKey: "b-1", APIGroupKey: "provider-b", Model: "gpt-5", Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), TotalTokens: 40},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	for _, tc := range []struct {
		filterModel string
		wantTokens  []int64
	}{
		{filterModel: "claude-sonnet-cfs", wantTokens: []int64{10}},
		{filterModel: "claude-sonnet-phoenix", wantTokens: []int64{20}},
		{filterModel: "claude-sonnet", wantTokens: []int64{30}},
		{filterModel: "gpt-5", wantTokens: []int64{40}},
	} {
		t.Run(tc.filterModel, func(t *testing.T) {
			page, err := ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Model: tc.filterModel, Page: 1, PageSize: 100, Limit: 100}, emptyPricingResolverForTest())
			if err != nil {
				t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
			}
			gotTokens := make([]int64, 0, len(page.Events))
			for _, event := range page.Events {
				gotTokens = append(gotTokens, event.TotalTokens)
			}
			if len(gotTokens) != len(tc.wantTokens) {
				t.Fatalf("expected events with tokens %v for filter %q, got %v", tc.wantTokens, tc.filterModel, gotTokens)
			}
			for i := range tc.wantTokens {
				if gotTokens[i] != tc.wantTokens[i] {
					t.Fatalf("expected events with tokens %v for filter %q, got %v", tc.wantTokens, tc.filterModel, gotTokens)
				}
			}
		})
	}
}
