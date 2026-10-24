package test

import (
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageEventAPIKeyCandidatesMatchEventsAndIgnoreSelectedKeyAndPagination(t *testing.T) {
	db := openTestDatabase(t)
	keys := []entities.CPAAPIKey{
		{APIKey: "matching-a", KeyAlias: "First"}, {APIKey: "matching-b"},
		{APIKey: "configured-only"}, {APIKey: "deleted", IsDeleted: true},
		{APIKey: "before-window"}, {APIKey: "end-boundary"},
		{APIKey: "other-model"}, {APIKey: "other-source"},
		{APIKey: "other-result"}, {APIKey: "other-auth-type"},
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	makeEvent := func(key string) entities.UsageEvent {
		return entities.UsageEvent{EventKey: key, APIGroupKey: key, Timestamp: start, Model: "gpt-5", AuthIndex: "auth-1", AuthType: "oauth", Failed: true}
	}
	events := []entities.UsageEvent{makeEvent("matching-a"), makeEvent("matching-b"), makeEvent("deleted"), makeEvent("unregistered")}
	duplicate := makeEvent("matching-a")
	duplicate.EventKey = "matching-a-duplicate"
	events = append(events, duplicate)
	before, boundary := makeEvent("before-window"), makeEvent("end-boundary")
	before.Timestamp, boundary.Timestamp = start.Add(-time.Second), end
	model, source := makeEvent("other-model"), makeEvent("other-source")
	model.Model, source.AuthIndex, source.Source = "gpt-other", "auth-other", "auth-1"
	result, authType := makeEvent("other-result"), makeEvent("other-auth-type")
	result.Failed, authType.AuthType = false, "apikey"
	events = append(events, before, boundary, model, source, result, authType)
	// Candidates only require a recorded request, including failed zero-token
	// events whose costs have not been populated yet.
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	filter := repodto.UsageQueryFilter{
		StartTime: &start, EndTime: &end, EndExclusive: true,
		Model: "gpt-5", AuthIndex: "auth-1", AuthType: "oauth", Result: "failed",
		APIGroupKey: "matching-a", Page: 100, PageSize: 1, Offset: 100, Limit: 1,
		CursorMode: true, CursorTimestamp: &start, CursorID: 1,
	}
	rows, err := repository.ListUsageEventAPIKeyFilterOptions(db, filter)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, len(rows))
	for index, row := range rows {
		ids[index] = row.ID
	}
	if !reflect.DeepEqual(ids, []int64{keys[0].ID, keys[1].ID}) || rows[0].KeyAlias != "First" {
		t.Fatalf("expected matching active keys once, in stable ID order, got %+v", rows)
	}
	filter.Model = "missing-model"
	rows, err = repository.ListUsageEventAPIKeyFilterOptions(db, filter)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("expected an empty candidate list, got %+v, %v", rows, err)
	}
}
