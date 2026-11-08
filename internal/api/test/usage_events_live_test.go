package test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	api "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type liveTestFrame struct {
	kind string
	id   string
	data map[string]any
}

func readLiveTestFrame(t *testing.T, reader *bufio.Reader) liveTestFrame {
	t.Helper()
	frame := liveTestFrame{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read live frame: %v", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if frame.kind != "" {
				return frame
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			frame.kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			frame.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame.data); err != nil {
				t.Fatalf("decode live data: %v", err)
			}
		}
	}
}

func liveTestReadKind(t *testing.T, reader *bufio.Reader, kind string) liveTestFrame {
	t.Helper()
	for i := 0; i < 20; i++ {
		frame := readLiveTestFrame(t, reader)
		if frame.kind == "snapshot" && kind != "snapshot" {
			t.Fatal("resume unexpectedly sent snapshot")
		}
		if frame.kind == kind {
			return frame
		}
		if frame.kind == "reset" {
			t.Fatalf("unexpected reset: %+v", frame.data)
		}
	}
	t.Fatalf("did not receive %s", kind)
	return liveTestFrame{}
}

func liveTestInsert(t *testing.T, db *gorm.DB, events []entities.UsageEvent) []entities.UsageEvent {
	t.Helper()
	for i := range events {
		events[i].CostUSD = new(1.25)
		events[i].CostAvailable = new(true)
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	return events
}

func liveTestConnect(t *testing.T, client *http.Client, target string, headers http.Header) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = headers
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect live: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("live status: %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("unexpected stream headers: %v", resp.Header)
	}
	return resp, bufio.NewReader(resp.Body)
}

func TestUsageEventsLiveSnapshotResumeAndCostGate(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now()
	seed := liveTestInsert(t, db, []entities.UsageEvent{
		{ID: 9007199254740993, Timestamp: now.Add(-time.Minute), Model: "match", RequestID: "same"},
		{Timestamp: now.Add(-time.Minute), Model: "match", RequestID: "same"},
		{Timestamp: now.Add(-time.Minute), Model: "other"},
	})
	notifier := service.NewUsageEventsNotifier()
	gate := service.NewCostReadGate()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, CostReadGate: gate}))
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	target := server.URL + "/api/v1/usage/events/live?range=24h&page_size=20&model=match"
	resp, reader := liveTestConnect(t, client, target, nil)
	defer resp.Body.Close()
	snap := liveTestReadKind(t, reader, "snapshot")
	rows := snap.data["events"].([]any)
	if len(rows) != 2 || snap.data["total_count"] != float64(2) || rows[0].(map[string]any)["id"] != fmt.Sprint(seed[1].ID) {
		t.Fatalf("unexpected snapshot: %+v", snap.data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resume, err := gate.BlockAndDrain(ctx)
	if err != nil {
		t.Fatalf("idle SSE retained cost permit: %v", err)
	}
	late := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-2 * time.Hour), Model: "match", RequestID: "same"}, {Timestamp: now, Model: "other"}})
	notifier.NotifyUsageEventsCommitted(late)
	busy := liveTestReadKind(t, reader, "busy")
	if busy.id != "" {
		t.Fatalf("busy advanced cursor: %+v", busy)
	}
	resume()
	appendFrame := liveTestReadKind(t, reader, "append")
	appended := appendFrame.data["events"].([]any)
	if len(appended) != 1 || appended[0].(map[string]any)["id"] != fmt.Sprint(late[0].ID) || appended[0].(map[string]any)["cost_usd"] != 1.25 {
		t.Fatalf("unexpected late append: %+v", appendFrame.data)
	}
	resp.Body.Close()
	missed := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-3 * time.Hour), Model: "match", RequestID: "same"}})
	notifier.NotifyUsageEventsCommitted(missed)
	resumed, resumedReader := liveTestConnect(t, client, target+"&after="+url.QueryEscape(appendFrame.id), nil)
	defer resumed.Body.Close()
	frame := liveTestReadKind(t, resumedReader, "append")
	if got := frame.data["events"].([]any)[0].(map[string]any)["id"]; got != fmt.Sprint(missed[0].ID) {
		t.Fatalf("replay id=%v", got)
	}
	bad, badReader := liveTestConnect(t, client, strings.Replace(target, "model=match", "model=other", 1)+"&after="+url.QueryEscape(frame.id), nil)
	defer bad.Body.Close()
	if reset := liveTestReadKind(t, badReader, "reset"); reset.data["code"] != "invalid_cursor" {
		t.Fatalf("scope mismatch: %+v", reset)
	}
}

func TestUsageEventsLiveRollingWindowFindsPreviouslyFutureEvent(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	future := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now().Add(600 * time.Millisecond), Model: "future"}})
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier}))
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h", nil)
	defer resp.Body.Close()
	first := liveTestReadKind(t, reader, "snapshot")
	if len(first.data["events"].([]any)) != 0 {
		t.Fatalf("future event appeared in snapshot: %+v", first.data)
	}
	time.Sleep(750 * time.Millisecond)
	notifier.NotifyUsageEventsCommitted(future)
	frame := liveTestReadKind(t, reader, "append")
	if got := frame.data["events"].([]any)[0].(map[string]any)["id"]; got != fmt.Sprint(future[0].ID) {
		t.Fatalf("future event missing: %v", got)
	}
}

func TestUsageEventsLiveAdminEmbedLimitAndRevocation(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	sessions := auth.NewSessionManager(time.Hour)
	config := api.AuthConfig{Enabled: true, SessionTTL: time.Hour, BasePath: "/keeper"}
	handler := api.NewAuthHandler(config, sessions)
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, config, handler, "/keeper", api.OptionalProviders{UsageEventsLive: notifier}))
	defer server.Close()
	target := server.URL + "/keeper/api/v1/usage/events/live?range=today"
	client := &http.Client{Timeout: 5 * time.Second}
	unauth, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	unauth.Body.Close()
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", unauth.StatusCode)
	}
	viewer, _, _ := sessions.CreateAPIKeyViewer(1)
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: viewer})
	viewResp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	viewResp.Body.Close()
	if viewResp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer status=%d", viewResp.StatusCode)
	}
	token, _, err := sessions.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"X-Cpa-Usage-Keeper-Embed": {"cpamc"}, "X-Cpa-Usage-Keeper-Embed-Session": {token}}
	responses := make([]*http.Response, 0, 3)
	var readers []*bufio.Reader
	for i := 0; i < 3; i++ {
		resp, reader := liveTestConnect(t, client, target, headers)
		responses = append(responses, resp)
		readers = append(readers, reader)
		liveTestReadKind(t, reader, "snapshot")
	}
	defer func() {
		for _, resp := range responses {
			resp.Body.Close()
		}
	}()
	req, _ = http.NewRequest(http.MethodGet, target, nil)
	req.Header = headers
	limited, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	limited.Body.Close()
	if limited.StatusCode != http.StatusTooManyRequests || limited.Header.Get("Retry-After") == "" {
		t.Fatalf("limit status=%d headers=%v", limited.StatusCode, limited.Header)
	}
	sessions.Delete(token)
	events := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now(), Model: "revoked"}})
	notifier.NotifyUsageEventsCommitted(events)
	if _, err := readers[0].ReadString('\n'); err == nil {
		t.Fatal("revoked session received live data")
	}
}

func TestUsageEventsLiveHeartbeatCountsCurrentFilteredRange(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	gate := service.NewCostReadGate()
	now := time.Now()
	liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-time.Minute), Model: "match"}, {Timestamp: now.Add(-time.Minute), Model: "other"}})
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, CostReadGate: gate}))
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 20 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h&model=match", nil)
	defer resp.Body.Close()
	if first := liveTestReadKind(t, reader, "snapshot"); first.data["total_count"] != float64(1) {
		t.Fatalf("snapshot count: %+v", first.data)
	}
	added := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now, Model: "match"}, {Timestamp: now, Model: "other"}})
	notifier.NotifyUsageEventsCommitted(added)
	liveTestReadKind(t, reader, "append")
	if heartbeat := liveTestReadKind(t, reader, "heartbeat"); heartbeat.data["total_count"] != float64(2) {
		t.Fatalf("heartbeat count must use the same filter: %+v", heartbeat.data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := gate.BlockAndDrain(ctx)
	if err != nil {
		t.Fatalf("heartbeat retained the cost permit: %v", err)
	}
	release()
}
