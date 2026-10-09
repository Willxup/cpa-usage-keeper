package test

import (
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestListUsageEventsWithFilterDoesNotLoadModelFilterOptions(t *testing.T) {
	db := openTestDatabase(t)
	seedUsageEventModels(t, db)
	recorder, queryDB := usageEventsQueryRecorder(db)

	page, err := repository.ListUsageEventsWithFilter(queryDB, repodto.UsageQueryFilter{Page: 1, PageSize: 1}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 || page.TotalCount != 2 || page.Page != 1 || page.PageSize != 1 || page.TotalPages != 2 {
		t.Fatalf("unexpected usage events page: %+v", page)
	}
	if strings.Contains(strings.ToLower(recorder.String()), "select distinct model") {
		t.Fatalf("expected list query not to load model filter options, SQL logs:\n%s", recorder.String())
	}
}

func TestListUsageEventsWithFilterSkipsTotalCountWhenRequested(t *testing.T) {
	db := openTestDatabase(t)
	seedUsageEventModels(t, db)
	recorder, queryDB := usageEventsQueryRecorder(db)

	page, err := repository.ListUsageEventsWithFilter(queryDB, repodto.UsageQueryFilter{
		Page:           1,
		PageSize:       1,
		CursorMode:     true,
		SkipTotalCount: true,
	}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 || page.TotalCount != -1 || !page.HasMore {
		t.Fatalf("unexpected count-free usage events page: %+v", page)
	}
	if strings.Contains(strings.ToLower(recorder.String()), "count(*)") {
		t.Fatalf("expected count-free query not to execute COUNT(*), SQL logs:\n%s", recorder.String())
	}
}

func TestListUsageEventFilterOptionsWithFilterStillLoadsModels(t *testing.T) {
	db := openTestDatabase(t)
	seedUsageEventModels(t, db)
	recorder, queryDB := usageEventsQueryRecorder(db)

	options, err := repository.ListUsageEventFilterOptionsWithFilter(queryDB, repodto.UsageQueryFilter{})
	if err != nil {
		t.Fatalf("ListUsageEventFilterOptionsWithFilter returned error: %v", err)
	}
	if want := []string{"model-alpha", "model-beta"}; !slices.Equal(options.Models, want) {
		t.Fatalf("expected actual usage event models %v, got %v", want, options.Models)
	}
	if !strings.Contains(strings.ToLower(recorder.String()), "select distinct model") {
		t.Fatalf("expected model filter options query to use SELECT DISTINCT model, SQL logs:\n%s", recorder.String())
	}
}

func seedUsageEventModels(t *testing.T, db *gorm.DB) {
	t.Helper()

	events := []entities.UsageEvent{
		{EventKey: "usage-events-query-alpha", Model: "model-alpha", Timestamp: time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)},
		{EventKey: "usage-events-query-beta", Model: "model-beta", Timestamp: time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
}

func usageEventsQueryRecorder(db *gorm.DB) (*strings.Builder, *gorm.DB) {
	recorder := &strings.Builder{}
	queryLogger := gormlogger.New(log.New(recorder, "", 0), gormlogger.Config{LogLevel: gormlogger.Info})
	return recorder, db.Session(&gorm.Session{Logger: queryLogger})
}
