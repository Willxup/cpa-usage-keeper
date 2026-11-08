package test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type liveTestFrame struct {
	kind string
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
		case strings.HasPrefix(line, "id:"):
			t.Fatalf("unexpected SSE transport id: %s", line)
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
			t.Fatal("unexpected snapshot after connection initialized")
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

func TestUsageEventsLiveSnapshotReconnectAndCostGate(t *testing.T) {
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
	if busy.data["code"] != "costs_busy" {
		t.Fatalf("unexpected busy response: %+v", busy)
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
	reconnected, reconnectedReader := liveTestConnect(t, client, target, nil)
	defer reconnected.Body.Close()
	fresh := liveTestReadKind(t, reconnectedReader, "snapshot")
	found := false
	for _, row := range fresh.data["events"].([]any) {
		found = found || row.(map[string]any)["id"] == fmt.Sprint(missed[0].ID)
	}
	if !found {
		t.Fatalf("reconnect snapshot omitted event committed while disconnected: %+v", fresh.data)
	}
	next := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-4 * time.Hour), Model: "match"}})
	notifier.NotifyUsageEventsCommitted(next)
	frame := liveTestReadKind(t, reconnectedReader, "append")
	if got := frame.data["events"].([]any)[0].(map[string]any)["id"]; got != fmt.Sprint(next[0].ID) {
		t.Fatalf("reconnected append id=%v", got)
	}
}

func TestUsageEventsLiveResolvesCurrentAPIKeyAlias(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now()
	const keyValue = "sk-live-alias-probe"
	if err := repository.SyncCPAAPIKeys(db, []string{keyValue}, now); err != nil {
		t.Fatal(err)
	}
	key, err := repository.FindActiveCPAAPIKeyByValue(db, keyValue)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, key.ID, "Before rename"); err != nil {
		t.Fatal(err)
	}
	liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-2 * time.Minute), APIGroupKey: keyValue, Model: "initial"}})
	notifier := service.NewUsageEventsNotifier()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	keys := service.NewCPAAPIKeyService(db)
	router := api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, CPAAPIKeys: keys})
	server := httptest.NewServer(router)
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h", nil)
	defer resp.Body.Close()
	initial := liveTestReadKind(t, reader, "snapshot").data["events"].([]any)[0].(map[string]any)
	if initial["api_key"] != "Before rename" {
		t.Fatalf("initial snapshot alias: %v", initial["api_key"])
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, key.ID, "After rename"); err != nil {
		t.Fatal(err)
	}
	added := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now.Add(-time.Minute), APIGroupKey: keyValue, Model: "after-rename"}})
	notifier.NotifyUsageEventsCommitted(added)
	liveRow := liveTestReadKind(t, reader, "append").data["events"].([]any)[0].(map[string]any)
	listed := serveAPIGet(router, "/api/v1/usage/events?range=24h&cursor_mode=true")
	var page struct {
		Events []map[string]any `json:"events"`
	}
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &page) != nil || len(page.Events) != 2 {
		t.Fatalf("HTTP list status=%d body=%s", listed.Code, listed.Body.String())
	}
	for _, row := range page.Events {
		if row["id"] == liveRow["id"] {
			if liveRow["api_key"] != "After rename" || liveRow["api_key"] != row["api_key"] {
				t.Fatalf("new live row alias=%q, HTTP alias=%q", liveRow["api_key"], row["api_key"])
			}
			return
		}
	}
	t.Fatalf("HTTP page did not contain live event id=%v", liveRow["id"])
}

type countingLiveCPAAPIKeyProvider struct {
	service.CPAAPIKeyProvider
	calls   atomic.Int32
	failure error
}

func (p *countingLiveCPAAPIKeyProvider) ListCPAAPIKeys(ctx context.Context) ([]entities.CPAAPIKey, error) {
	p.calls.Add(1)
	if p.failure != nil {
		return nil, p.failure
	}
	return p.CPAAPIKeyProvider.ListCPAAPIKeys(ctx)
}

func TestUsageEventsLiveResolvesNewPluginAPIKeyLabels(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	keys := &countingLiveCPAAPIKeyProvider{CPAAPIKeyProvider: service.NewCPAAPIKeyService(db)}
	router := api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, CPAAPIKeys: keys})
	server := httptest.NewServer(router)
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h", nil)
	defer resp.Body.Close()
	liveTestReadKind(t, reader, "snapshot")
	if keys.calls.Load() != 0 {
		t.Fatalf("empty snapshot queried API keys: %d", keys.calls.Load())
	}

	keyA := helper.PluginAPIKeyGroupKey("plugin:probe", "principal-a")
	keyB := helper.PluginAPIKeyGroupKey("plugin:probe", "principal-b")
	registered, err := repository.RegisterPluginAPIKeys(context.Background(), db, []entities.PluginAPIKeyIdentity{
		{APIGroupKey: keyA, AccessProvider: "plugin:probe"},
		{APIGroupKey: keyB, AccessProvider: "plugin:probe"},
	})
	if err != nil || registered.Inserted != 2 {
		t.Fatalf("register plugin identities: %+v %v", registered, err)
	}
	added := liveTestInsert(t, db, []entities.UsageEvent{
		{Timestamp: time.Now().Add(-time.Minute), APIGroupKey: keyA, Model: "a"},
		{Timestamp: time.Now().Add(-time.Minute), APIGroupKey: keyB, Model: "b"},
	})
	notifier.NotifyUsageEventsCommitted(added)
	frame := liveTestReadKind(t, reader, "append")
	if keys.calls.Load() != 1 {
		t.Fatalf("expected one API key lookup for a two-event batch, got %d", keys.calls.Load())
	}
	live := make(map[string]string)
	for _, value := range frame.data["events"].([]any) {
		row := value.(map[string]any)
		live[row["id"].(string)] = row["api_key"].(string)
	}
	listed := serveAPIGet(router, "/api/v1/usage/events?range=24h&cursor_mode=true")
	var page struct {
		Events []map[string]any `json:"events"`
	}
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &page) != nil || len(page.Events) != 2 {
		t.Fatalf("HTTP list status=%d body=%s", listed.Code, listed.Body.String())
	}
	labels := make(map[string]bool)
	for _, row := range page.Events {
		id, label := row["id"].(string), row["api_key"].(string)
		if live[id] != label {
			t.Errorf("new live event %s label=%q, HTTP label=%q", id, live[id], label)
		}
		labels[label] = true
	}
	if !labels["Third-party Identity #1"] || !labels["Third-party Identity #2"] {
		t.Fatalf("HTTP labels did not distinguish plugin identities: %+v", labels)
	}
}

func TestUsageEventsLiveAPIKeyLookupFailureDoesNotEmitMislabelledEvents(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	keys := &countingLiveCPAAPIKeyProvider{CPAAPIKeyProvider: service.NewCPAAPIKeyService(db), failure: errors.New("key lookup failed")}
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, CPAAPIKeys: keys}))
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	target := server.URL + "/api/v1/usage/events/live?range=24h"
	resp, reader := liveTestConnect(t, client, target, nil)
	defer resp.Body.Close()
	liveTestReadKind(t, reader, "snapshot")
	if keys.calls.Load() != 0 {
		t.Fatalf("empty snapshot queried API keys: %d", keys.calls.Load())
	}
	added := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now().Add(-time.Minute), Model: "new", APIGroupKey: "sk-key"}})
	notifier.NotifyUsageEventsCommitted(added)
	remaining, err := io.ReadAll(reader)
	if err != nil || strings.Contains(string(remaining), "event: append") || strings.Contains(string(remaining), "internal server error") {
		t.Fatalf("failed key lookup must close SSE without an event or JSON error: %q, %v", remaining, err)
	}
	if keys.calls.Load() != 1 {
		t.Fatalf("expected one lookup for the nonempty batch, got %d", keys.calls.Load())
	}
	failed, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer failed.Body.Close()
	body, err := io.ReadAll(failed.Body)
	if err != nil || failed.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "internal server error") {
		t.Fatalf("snapshot key lookup failure: status=%d body=%s error=%v", failed.StatusCode, body, err)
	}
}

func TestUsageEventsLiveResolvesCurrentIdentityForNewEvents(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	identities := service.NewUsageIdentityService(db)
	router := api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, UsageIdentity: identities})
	server := httptest.NewServer(router)
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h", nil)
	defer resp.Body.Close()
	liveTestReadKind(t, reader, "snapshot")

	// 模拟连接建立后其他会话提交了新的本地活跃身份。
	alias := "Active credential alias"
	identity := entities.UsageIdentity{Identity: "new-active-identity", Name: "credential.json", Alias: &alias, Type: "codex", AuthType: entities.UsageIdentityAuthTypeCredential, TotalRequests: 1}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	inserted := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now().Add(-time.Minute), Model: "metadata-probe", AuthIndex: identity.Identity, AuthType: "oauth", Source: "credential.json"}})
	notifier.NotifyUsageEventsCommitted(inserted)
	appendFrame := liveTestReadKind(t, reader, "append")
	liveRow := appendFrame.data["events"].([]any)[0].(map[string]any)

	listed := serveAPIGet(router, "/api/v1/usage/events?range=24h&cursor_mode=true")
	if listed.Code != http.StatusOK {
		t.Fatalf("HTTP list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var list struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil || len(list.Events) != 1 {
		t.Fatalf("decode HTTP events: %+v, %v", list.Events, err)
	}
	httpRow := list.Events[0]
	if httpRow["source"] != alias || httpRow["source_type"] != "codex" || httpRow["isDelete"] == true {
		t.Fatalf("expected active HTTP identity: %+v", httpRow)
	}
	if liveRow["id"] != httpRow["id"] || liveRow["source"] != httpRow["source"] || liveRow["source_type"] != httpRow["source_type"] || liveRow["isDelete"] != httpRow["isDelete"] {
		t.Fatalf("live row differs from HTTP: live=%+v HTTP=%+v", liveRow, httpRow)
	}
}

type failingLiveIdentityProvider struct {
	service.UsageIdentityProvider
	calls atomic.Int32
}

func (p *failingLiveIdentityProvider) ListActiveUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	p.calls.Add(1)
	return nil, errors.New("identity lookup failed")
}

func TestUsageEventsLiveIdentityLookupFailureDoesNotEmitMislabelledEvents(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	identities := &failingLiveIdentityProvider{UsageIdentityProvider: service.NewUsageIdentityService(db)}
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, UsageIdentity: identities}))
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	target := server.URL + "/api/v1/usage/events/live?range=24h"
	resp, reader := liveTestConnect(t, client, target, nil)
	defer resp.Body.Close()
	liveTestReadKind(t, reader, "snapshot")
	if identities.calls.Load() != 0 {
		t.Fatalf("empty snapshot queried identities: %d", identities.calls.Load())
	}
	inserted := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now().Add(-time.Minute), Model: "new", AuthIndex: "credential"}})
	notifier.NotifyUsageEventsCommitted(inserted)
	remaining, err := io.ReadAll(reader)
	if err != nil || strings.Contains(string(remaining), "event: append") || strings.Contains(string(remaining), "internal server error") {
		t.Fatalf("failed lookup must close SSE without an event or JSON error: %q, %v", remaining, err)
	}
	if identities.calls.Load() != 1 {
		t.Fatalf("expected one lookup for the nonempty batch, got %d", identities.calls.Load())
	}
	failed, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer failed.Body.Close()
	body, err := io.ReadAll(failed.Body)
	if err != nil || failed.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "internal server error") {
		t.Fatalf("snapshot lookup failure: status=%d body=%s error=%v", failed.StatusCode, body, err)
	}
}

type blockingLiveIdentityProvider struct {
	service.UsageIdentityProvider
	started  chan struct{}
	canceled chan error
}

func (p *blockingLiveIdentityProvider) ListActiveUsageIdentities(ctx context.Context) ([]entities.UsageIdentity, error) {
	close(p.started)
	<-ctx.Done()
	p.canceled <- ctx.Err()
	return nil, ctx.Err()
}

func TestUsageEventsLiveApplicationCancellationStopsInFlightLookup(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	gate := service.NewCostReadGate()
	identities := &blockingLiveIdentityProvider{
		UsageIdentityProvider: service.NewUsageIdentityService(db), started: make(chan struct{}), canceled: make(chan error, 1),
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{
		UsageEventsLive: notifier, UsageEventsLiveContext: lifecycle, UsageIdentity: identities, CostReadGate: gate,
	}))
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h", nil)
	defer resp.Body.Close()
	liveTestReadKind(t, reader, "snapshot")
	inserted := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now().Add(-time.Minute), Model: "new", AuthIndex: "credential"}})
	notifier.NotifyUsageEventsCommitted(inserted)
	select {
	case <-identities.started:
	case <-time.After(2 * time.Second):
		t.Fatal("identity lookup did not start")
	}
	cancel()
	select {
	case err := <-identities.canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lookup cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("application cancellation did not reach the lookup")
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("live stream did not finish: %v", err)
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	release, err := gate.BlockAndDrain(drainCtx)
	if err != nil {
		t.Fatalf("canceled batch retained cost-read permit: %v", err)
	}
	release()
}

type blockingLiveCPAAPIKeyProvider struct {
	service.CPAAPIKeyProvider
	started  chan struct{}
	canceled chan error
}

func (p *blockingLiveCPAAPIKeyProvider) ListCPAAPIKeys(ctx context.Context) ([]entities.CPAAPIKey, error) {
	close(p.started)
	<-ctx.Done()
	p.canceled <- ctx.Err()
	return nil, ctx.Err()
}

func TestUsageEventsLiveApplicationCancellationStopsInFlightAPIKeyLookup(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	gate := service.NewCostReadGate()
	keys := &blockingLiveCPAAPIKeyProvider{
		CPAAPIKeyProvider: service.NewCPAAPIKeyService(db), started: make(chan struct{}), canceled: make(chan error, 1),
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{
		UsageEventsLive: notifier, UsageEventsLiveContext: lifecycle, CPAAPIKeys: keys, CostReadGate: gate,
	}))
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h", nil)
	defer resp.Body.Close()
	liveTestReadKind(t, reader, "snapshot")
	added := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now().Add(-time.Minute), Model: "new", APIGroupKey: "sk-key"}})
	notifier.NotifyUsageEventsCommitted(added)
	select {
	case <-keys.started:
	case <-time.After(2 * time.Second):
		t.Fatal("API key lookup did not start")
	}
	cancel()
	select {
	case err := <-keys.canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("API key lookup cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("application cancellation did not reach the API key lookup")
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("live stream did not finish: %v", err)
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	release, err := gate.BlockAndDrain(drainCtx)
	if err != nil {
		t.Fatalf("canceled API key batch retained cost-read permit: %v", err)
	}
	release()
}

func TestUsageEventsLiveRollingWindowFindsPreviouslyFutureEvent(t *testing.T) {
	db := openAPITestDatabase(t)
	notifier := service.NewUsageEventsNotifier()
	future := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: time.Now().Add(600 * time.Millisecond), Model: "future", AuthIndex: "future-identity", AuthType: "oauth", Source: "future.json", APIGroupKey: "sk-future-key"}})
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	identities := service.NewUsageIdentityService(db)
	keys := service.NewCPAAPIKeyService(db)
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, UsageIdentity: identities, CPAAPIKeys: keys}))
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 5 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h", nil)
	defer resp.Body.Close()
	first := liveTestReadKind(t, reader, "snapshot")
	if len(first.data["events"].([]any)) != 0 {
		t.Fatalf("future event appeared in snapshot: %+v", first.data)
	}
	alias := "Future credential"
	identity := entities.UsageIdentity{Identity: "future-identity", Name: "future.json", Alias: &alias, Type: "codex", AuthType: entities.UsageIdentityAuthTypeCredential, TotalRequests: 1}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-future-key", KeyAlias: "Future key"}).Error; err != nil {
		t.Fatal(err)
	}
	time.Sleep(750 * time.Millisecond)
	notifier.NotifyUsageEventsCommitted(future)
	frame := liveTestReadKind(t, reader, "append")
	row := frame.data["events"].([]any)[0].(map[string]any)
	if row["id"] != fmt.Sprint(future[0].ID) || row["source"] != alias || row["source_type"] != "codex" || row["isDelete"] == true || row["api_key"] != "Future key" {
		t.Fatalf("future event or current metadata missing: %+v", row)
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
	keys := &countingLiveCPAAPIKeyProvider{CPAAPIKeyProvider: service.NewCPAAPIKeyService(db)}
	server := httptest.NewServer(api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{UsageEventsLive: notifier, CostReadGate: gate, CPAAPIKeys: keys}))
	defer server.Close()
	resp, reader := liveTestConnect(t, &http.Client{Timeout: 20 * time.Second}, server.URL+"/api/v1/usage/events/live?range=24h&model=match", nil)
	defer resp.Body.Close()
	if first := liveTestReadKind(t, reader, "snapshot"); first.data["total_count"] != float64(1) {
		t.Fatalf("snapshot count: %+v", first.data)
	}
	added := liveTestInsert(t, db, []entities.UsageEvent{{Timestamp: now, Model: "match"}, {Timestamp: now, Model: "other"}})
	notifier.NotifyUsageEventsCommitted(added)
	liveTestReadKind(t, reader, "append")
	if keys.calls.Load() != 2 {
		t.Fatalf("expected one API key lookup per nonempty batch, got %d", keys.calls.Load())
	}
	if heartbeat := liveTestReadKind(t, reader, "heartbeat"); heartbeat.data["total_count"] != float64(2) {
		t.Fatalf("heartbeat count must use the same filter: %+v", heartbeat.data)
	}
	if keys.calls.Load() != 2 {
		t.Fatalf("heartbeat queried API keys: %d", keys.calls.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := gate.BlockAndDrain(ctx)
	if err != nil {
		t.Fatalf("heartbeat retained the cost permit: %v", err)
	}
	release()
}
