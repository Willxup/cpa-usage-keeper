package test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type usageEventsSQLRecorder struct {
	logs strings.Builder
}

func (r *usageEventsSQLRecorder) Printf(message string, args ...interface{}) {
	fmt.Fprintf(&r.logs, message, args...)
	r.logs.WriteByte('\n')
}

func (r *usageEventsSQLRecorder) String() string {
	return r.logs.String()
}

func TestListUsageEventsWithFilterDoesNotLoadModelFilterOptions(t *testing.T) {
	db := openTestDatabase(t)
	seedUsageEventModels(t, db)
	recorder, queryDB := usageEventsQueryRecorder(db)

	page, err := repository.ListUsageEventsWithFilter(queryDB, repodto.UsageQueryFilter{Page: 1, PageSize: 1})
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

func TestListUsageEventFilterOptionsWithFilterStillLoadsModels(t *testing.T) {
	db := openTestDatabase(t)
	seedUsageEventModels(t, db)
	recorder, queryDB := usageEventsQueryRecorder(db)

	options, err := repository.ListUsageEventFilterOptionsWithFilter(queryDB, repodto.UsageQueryFilter{})
	if err != nil {
		t.Fatalf("ListUsageEventFilterOptionsWithFilter returned error: %v", err)
	}
	if got, want := strings.Join(options.Models, ","), "model-alpha,model-beta"; got != want {
		t.Fatalf("expected actual usage event models %q, got %q", want, got)
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

func usageEventsQueryRecorder(db *gorm.DB) (*usageEventsSQLRecorder, *gorm.DB) {
	recorder := &usageEventsSQLRecorder{}
	queryLogger := gormlogger.New(recorder, gormlogger.Config{LogLevel: gormlogger.Info})
	return recorder, db.Session(&gorm.Session{Logger: queryLogger})
}
