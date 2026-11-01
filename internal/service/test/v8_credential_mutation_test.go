package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

// 模拟 CPA 配置列表整体替换与 GET 注入只读标识，首个 GET 可控阻塞以暴露过期快照覆盖。
type v8ProviderServer struct {
	mu                                      sync.Mutex
	document                                providerconfig.Document
	reads, writes                           int
	failWrite                               bool
	firstRead, secondRead, releaseFirstRead chan struct{}
}

func newV8ProviderServer(t *testing.T, raw string, blockFirst bool) (*v8ProviderServer, *httptest.Server) {
	t.Helper()
	state := &v8ProviderServer{}
	if err := json.Unmarshal([]byte(raw), &state.document); err != nil {
		t.Fatal(err)
	}
	if blockFirst {
		state.firstRead, state.secondRead, state.releaseFirstRead = make(chan struct{}), make(chan struct{}), make(chan struct{})
	}
	server := httptest.NewServer(http.HandlerFunc(state.serve))
	t.Cleanup(server.Close)
	return state, server
}

func (s *v8ProviderServer) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		s.reads++
		read := s.reads
		data, err := json.Marshal(s.document)
		s.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.firstRead != nil {
			if read == 1 {
				close(s.firstRead)
				<-s.releaseFirstRead
			}
			if read == 2 {
				close(s.secondRead)
			}
		}
		w.Write(data)
	case http.MethodPut:
		var next providerconfig.Document
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.writes++
		if s.failWrite {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"error":"write_failed"}`)
			return
		}
		// CPA 的 auth_index 来自运行时而非保存内容；mock 仅按稳定位置重新附加原标识。
		for groupIndex := range next.Groups {
			for keyIndex := range next.Groups[groupIndex].Keys {
				next.Groups[groupIndex].Keys[keyIndex].Fields["auth_index"] = s.document.Groups[groupIndex].Keys[keyIndex].Fields["auth_index"]
			}
		}
		s.document = next
		fmt.Fprint(w, `{"status":"ok","config-version":8}`)
	default:
		http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
	}
}

func (s *v8ProviderServer) snapshot(t *testing.T) providerconfig.Document {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(s.document)
	if err != nil {
		t.Fatal(err)
	}
	var copy providerconfig.Document
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestV8ProviderMutationsSerializeWholeListAcrossServicesAndGroups(t *testing.T) {
	for _, scenario := range []struct {
		name, provider, first, second string
		sameGroup                     bool
	}{
		{"status then priority", "codex", "status", "priority", false},
		{"priority then status", "codex", "priority", "status", false},
		{"different keys status", "codex", "status", "status", false},
		{"different keys priority", "codex", "priority", "priority", false},
		{"different OpenAI groups", "openai", "priority", "priority", false},
		{"same OpenAI group", "openai", "priority", "priority", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			raw := `[{"priority":1,"excluded-models":["model-a"],"keys":[{"api-key":"a-key","auth_index":"a"}]},{"priority":2,"excluded-models":["model-b"],"keys":[{"api-key":"b-key","auth_index":"b"}]}]`
			if scenario.sameGroup {
				raw = `[{"priority":1,"keys":[{"api-key":"a-key","auth_index":"a"},{"api-key":"b-key","auth_index":"b"}]}]`
			}
			state, server := newV8ProviderServer(t, raw, true)
			release := sync.OnceFunc(func() { close(state.releaseFirstRead) })
			defer release()
			db := openMetadataTestDatabase(t, "v8-concurrent.db")
			seedProviderCredential(t, db, scenario.provider, "a", "a-key")
			seedProviderCredential(t, db, scenario.provider, "b", "b-key")
			client := cpa.NewClient(server.URL, "management-secret", 3*time.Second, false)
			locks := &service.CredentialMutationLocks{}
			status := service.NewCredentialStatusService(db, client, nil, locks, nil)
			priority := service.NewCredentialPriorityService(db, client, nil, locks, nil)
			action := func(kind, authIndex string, value int) error {
				if kind == "status" {
					_, err := status.SetAIProviderDisabled(context.Background(), authIndex, true)
					return err
				}
				_, err := priority.SetAIProviderPriority(context.Background(), authIndex, value)
				return err
			}
			results := make(chan error, 2)
			go func() { results <- action(scenario.first, "a", 7) }()
			select {
			case <-state.firstRead:
			case <-time.After(2 * time.Second):
				t.Fatal("first GET did not start")
			}
			secondStarted := make(chan struct{})
			go func() { close(secondStarted); results <- action(scenario.second, "b", 9) }()
			<-secondStarted
			select {
			case <-state.secondRead:
				t.Error("second GET started with the first list mutation still pending")
			case <-time.After(70 * time.Millisecond):
			}
			release()
			for range 2 {
				select {
				case err := <-results:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("mutation timed out")
				}
			}
			final := state.snapshot(t)
			if scenario.provider == "openai" {
				groups, err := final.OpenAIProviders()
				if err != nil {
					t.Fatal(err)
				}
				if scenario.sameGroup {
					if *groups[0].Priority != 9 {
						t.Fatalf("same group priority = %d", *groups[0].Priority)
					}
					for _, index := range []string{"a", "b"} {
						requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, index), 9)
					}
				} else if *groups[0].Priority != 7 || *groups[1].Priority != 9 {
					t.Fatalf("group priorities = %#v", groups)
				}
			} else {
				keys, err := final.ProviderKeys("")
				if err != nil {
					t.Fatal(err)
				}
				if scenario.first == "status" && !*keys[0].Disabled || scenario.first == "priority" && *keys[0].Priority != 7 {
					t.Fatalf("first update lost: %#v", keys[0])
				}
				if scenario.second == "status" && !*keys[1].Disabled || scenario.second == "priority" && *keys[1].Priority != 9 {
					t.Fatalf("second update lost: %#v", keys[1])
				}
			}
			if state.reads != 2 || state.writes != 2 {
				t.Fatalf("requests = %d GET, %d PUT", state.reads, state.writes)
			}
		})
	}
}

func TestV8StatusWritesInheritedExclusionsToFirstMemberOnly(t *testing.T) {
	raw := `[{"excluded-models":["model-x","*"],"keys":[{"api-key":"first","auth_index":"target","excluded-models":null,"opaque":{"n":9007199254740993}},{"api-key":"same-group","auth_index":"target","excluded-models":["second"]}]},{"excluded-models":["other"],"keys":[{"api-key":"second-group","auth_index":"target"}]}]`
	state, server := newV8ProviderServer(t, raw, false)
	db := openMetadataTestDatabase(t, "v8-first-status.db")
	seedProviderCredential(t, db, "codex", "target", "first")
	provider := service.NewCredentialStatusService(db, cpa.NewClient(server.URL, "management-secret", time.Second, false), nil, &service.CredentialMutationLocks{}, nil)
	if _, err := provider.SetAIProviderDisabled(context.Background(), "target", false); err != nil {
		t.Fatal(err)
	}
	final := state.snapshot(t)
	if string(final.Groups[0].Fields["excluded-models"]) != `["model-x","*"]` || string(final.Groups[0].Keys[0].Fields["excluded-models"]) != `["model-x"]` {
		t.Fatalf("exclusions = %#v", final.Groups[0])
	}
	if string(final.Groups[0].Keys[1].Fields["excluded-models"]) != `["second"]` || string(final.Groups[1].Fields["excluded-models"]) != `["other"]` {
		t.Fatal("later duplicate changed")
	}
	if string(final.Groups[0].Keys[0].Fields["opaque"]) != `{"n":9007199254740993}` {
		t.Fatal("opaque field changed")
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAIProvider, "target"); disabled == nil || *disabled {
		t.Fatalf("local disabled = %v", disabled)
	}
	if state.reads != 1 || state.writes != 1 {
		t.Fatalf("requests = %d GET, %d PUT", state.reads, state.writes)
	}
}

func TestV8PriorityUpdatesFirstDuplicateAndOpenAIGroupScope(t *testing.T) {
	for _, providerType := range []string{"codex", "openai"} {
		t.Run(providerType, func(t *testing.T) {
			raw := `[{"name":"first","priority":3,"keys":[{"api-key":"a","auth_index":"target","priority":null},{"api-key":"b","auth_index":"sibling","priority":4}]},{"name":"later","priority":5,"keys":[{"api-key":"c","auth_index":"target"},{"api-key":"d","auth_index":"outside"}]}]`
			state, server := newV8ProviderServer(t, raw, false)
			db := openMetadataTestDatabase(t, "v8-first-priority.db")
			for _, index := range []string{"target", "sibling", "outside"} {
				seedProviderCredential(t, db, providerType, index, index+"-key")
			}
			refresher := &credentialStatusRefresherStub{}
			provider := service.NewCredentialPriorityService(db, cpa.NewClient(server.URL, "management-secret", time.Second, false), refresher, &service.CredentialMutationLocks{}, nil)
			if _, err := provider.SetAIProviderPriority(context.Background(), "target", 0); err != nil {
				t.Fatal(err)
			}
			final := state.snapshot(t)
			if string(final.Groups[1].Fields["priority"]) != "5" || string(final.Groups[0].Keys[1].Fields["priority"]) != "4" {
				t.Fatal("unrelated config priority changed")
			}
			if providerType == "openai" {
				if string(final.Groups[0].Fields["priority"]) != "0" || string(final.Groups[0].Keys[0].Fields["priority"]) != "null" {
					t.Fatal("OpenAI member priority changed")
				}
				requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "sibling"), 0)
			} else {
				if string(final.Groups[0].Fields["priority"]) != "3" || string(final.Groups[0].Keys[0].Fields["priority"]) != "0" {
					t.Fatal("ordinary group priority changed")
				}
				if got := loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "sibling"); got != nil {
					t.Fatal("ordinary sibling local priority changed")
				}
			}
			requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "target"), 0)
			if got := loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "outside"); got != nil {
				t.Fatal("other group local priority changed")
			}
			if state.reads != 1 || state.writes != 1 || refresher.count() != 1 {
				t.Fatalf("requests = %d/%d, refresh = %d", state.reads, state.writes, refresher.count())
			}
		})
	}
}

func TestV8FailedPUTPreservesUpstreamAndLocalState(t *testing.T) {
	for _, kind := range []string{"status", "priority"} {
		t.Run(kind, func(t *testing.T) {
			raw := `[{"priority":3,"keys":[{"api-key":"key","auth_index":"target","excluded-models":["model-x"]}]}]`
			state, server := newV8ProviderServer(t, raw, false)
			state.failWrite = true
			before := state.snapshot(t)
			db := openMetadataTestDatabase(t, "v8-failed-put.db")
			seedProviderCredential(t, db, "codex", "target", "key")
			client := cpa.NewClient(server.URL, "management-secret", time.Second, false)
			locks := &service.CredentialMutationLocks{}
			refresh := &credentialStatusRefresherStub{}
			var err error
			if kind == "status" {
				_, err = service.NewCredentialStatusService(db, client, refresh, locks, nil).SetAIProviderDisabled(context.Background(), "target", true)
			} else {
				_, err = service.NewCredentialPriorityService(db, client, refresh, locks, nil).SetAIProviderPriority(context.Background(), "target", 9)
			}
			if err == nil {
				t.Fatal("PUT failure returned success")
			}
			final := state.snapshot(t)
			if !reflect.DeepEqual(before, final) {
				t.Fatal("rejected PUT changed upstream state")
			}
			if loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "target") != nil || loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAIProvider, "target") != nil {
				t.Fatal("rejected PUT changed local state")
			}
			if refresh.count() != 0 {
				t.Fatal("rejected PUT requested success refresh")
			}
		})
	}
}
