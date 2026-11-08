package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	api "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestUsageEventsLiveFiltersMatchListAndCheckpointUnmatchedIDs(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now()
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-live-keep", "sk-live-other"}, now); err != nil {
		t.Fatal(err)
	}
	liveTestInsert(t, db, []entities.UsageEvent{
		{Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-live-keep", Model: "match", AuthIndex: "identity-a", AuthType: "oauth", Failed: true},
		{Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-live-keep", Model: "match", AuthIndex: "identity-b", AuthType: "oauth", Failed: true},
		{Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-live-other", Model: "match", AuthIndex: "identity-a", AuthType: "oauth", Failed: true},
		{Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-live-keep", Model: "match", AuthIndex: "identity-a", AuthType: "oauth", Failed: false},
		{Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-live-keep", Model: "match", AuthIndex: "identity-a", AuthType: "apikey", Failed: true},
		{Timestamp: now.Add(-25 * time.Hour), APIGroupKey: "sk-live-keep", Model: "match", AuthIndex: "identity-a", AuthType: "oauth", Failed: true},
	})
	notifier := service.NewUsageEventsNotifier()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	keys := service.NewCPAAPIKeyService(db)
	router := api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, CPAAPIKeys: keys})
	server := httptest.NewServer(router)
	defer server.Close()
	query := "?range=24h&page_size=20&cursor_mode=true&api_key_id=1&model=match&source=identity-a&auth_type=1&result=failed"
	listed := serveAPIGet(router, "/api/v1/usage/events"+query)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var list struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live"+query, nil)
	defer resp.Body.Close()
	snap := liveTestReadKind(t, reader, "snapshot")
	liveEvents, _ := json.Marshal(snap.data["events"])
	listEvents, _ := json.Marshal(list.Events)
	if len(list.Events) != 1 || string(liveEvents) != string(listEvents) || strings.Contains(string(liveEvents), "sk-live-keep") {
		t.Fatalf("live/list filter or redaction mismatch: live=%s list=%s", liveEvents, listEvents)
	}
	unmatched := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-live-keep", Model: "other", AuthIndex: "identity-a", AuthType: "oauth", Failed: true}})
	notifier.NotifyUsageEventsCommitted(unmatched)
	for {
		frame := readLiveTestFrame(t, reader)
		if frame.kind == "append" {
			t.Fatalf("unmatched event sent as append: %+v", frame)
		}
		if frame.kind == "checkpoint" {
			break
		}
	}
	matched := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-live-keep", Model: "match", AuthIndex: "identity-a", AuthType: "oauth", Failed: true}})
	notifier.NotifyUsageEventsCommitted(matched)
	frame := liveTestReadKind(t, reader, "append")
	rows := frame.data["events"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != fmt.Sprint(matched[0].ID) {
		t.Fatalf("filtered checkpoint did not advance to matched event: %+v", frame.data)
	}
}
