package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/credentials"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

// 远端旧快照已经取到、本地 diff 尚未读取时完成用户操作，旧快照不能覆盖已经确认的新值。
func TestMetadataSnapshotDoesNotOverwriteCompletedCredentialMutation(t *testing.T) {
	for _, operation := range []struct {
		name               string
		provider, priority bool
	}{
		{name: "credential-status"},
		{name: "credential-priority", priority: true},
		{name: "provider-status", provider: true},
		{name: "provider-priority", provider: true, priority: true},
	} {
		provider, priority := operation.provider, operation.priority
		for _, outcome := range []string{"success", "upstream-failure", "local-failure"} {
			t.Run(operation.name+"/"+outcome, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				db := openMetadataTestDatabase(t, "metadata-mutation.db")
				fetcher := newMetadataTestFetcher()
				if provider {
					fetcher.standardResults["codex"].Payload = []providerconfig.ProviderKeyConfig{{
						APIKey: "test-key", AuthIndex: "target", Priority: metadataIntPointer(0), Disabled: metadataBoolPointer(false),
					}}
				} else {
					fetcher.setCredentials([]credentials.Credential{{
						AuthIndex: "target", Name: "target.json", Type: "codex", Provider: "codex",
						Priority: metadataIntPointer(0), Disabled: metadataBoolPointer(false),
					}})
				}
				coordinator := &service.CredentialMetadataCoordinator{}
				syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{MetadataFetcher: fetcher, CredentialMetadata: coordinator})
				if err := syncer.SyncMetadata(ctx); err != nil {
					t.Fatal(err)
				}
				// 额外变化用于辨认整份旧快照是否仍然落库，本地写失败也能观察到差异。
				if provider {
					fetcher.standardResults["codex"].Payload[0].Name = "stale-snapshot"
				} else {
					fetcher.credentialsResult.Payload.Files[0].Email = "stale-snapshot"
				}
				// 管理 Key 是独立来源，即使本轮凭据快照失效也应完成同步。
				fetcher.managementAPIKeysResult.Payload.APIKeys = []string{"independent-key"}
				releaseSnapshot := startHeldMetadataSnapshot(t, ctx, syncer, fetcher)
				var writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method == http.MethodGet {
						fmt.Fprint(w, `[{"keys":[{"api-key":"test-key","auth_index":"target","priority":0,"excluded-models":[]}]}]`)
						return
					}
					if r.Method != http.MethodPut && r.Method != http.MethodPatch {
						t.Errorf("unexpected method %s", r.Method)
					}
					writes.Add(1)
					if outcome == "upstream-failure" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					fmt.Fprint(w, `{"status":"ok"}`)
				}))
				defer server.Close()
				client := cpa.NewClient(server.URL, "management-secret", time.Second, false)
				locks, refresh := &service.CredentialMutationLocks{}, &credentialStatusRefresherStub{}
				// 只让用户操作的 UPDATE 失败；放开旧同步前撤下故障，证明它是被版本拦住。
				var failLocal atomic.Bool
				if outcome == "local-failure" {
					failLocal.Store(true)
					if err := db.Callback().Update().Before("gorm:update").Register("test:fail_mutation_persist", func(tx *gorm.DB) {
						if failLocal.Load() {
							tx.AddError(errors.New("local persistence unavailable"))
						}
					}); err != nil {
						t.Fatal(err)
					}
				}
				statusService := service.NewCredentialStatusService(db, client, refresh, locks, coordinator)
				priorityService := service.NewCredentialPriorityService(db, client, refresh, locks, coordinator)
				setDisabled, setPriority := statusService.SetCredentialDisabled, priorityService.SetCredentialPriority
				if provider {
					setDisabled, setPriority = statusService.SetAIProviderDisabled, priorityService.SetAIProviderPriority
				}
				var err error
				if priority {
					_, err = setPriority(ctx, "target", 10)
				} else {
					_, err = setDisabled(ctx, "target", true)
				}
				failLocal.Store(false)
				if (err == nil) != (outcome == "success") {
					t.Fatalf("mutation outcome %s: %v", outcome, err)
				}
				authType := entities.UsageIdentityAuthTypeCredential
				if provider {
					authType = entities.UsageIdentityAuthTypeAIProvider
				}
				read := func() entities.UsageIdentity {
					return loadMetadataIdentityMap(t, db)[metadataIdentityKey(authType, "target")]
				}
				before := read()
				if outcome == "success" {
					if priority {
						requirePriority(t, before.Priority, 10)
					} else if before.Disabled == nil || !*before.Disabled {
						t.Fatalf("saved disabled state was lost: %+v", before)
					}
				}
				if err := releaseSnapshot(); err != nil {
					t.Fatal(err)
				}
				after := read()
				if outcome == "upstream-failure" {
					if after.Name != "stale-snapshot" {
						t.Fatalf("failed remote mutation invalidated a usable snapshot: %+v", after)
					}
				} else if !reflect.DeepEqual(before, after) {
					t.Fatalf("superseded snapshot changed local state: before=%+v after=%+v", before, after)
				}
				var key entities.CPAAPIKey
				if err := db.Where("api_key = ?", "independent-key").First(&key).Error; err != nil {
					t.Fatalf("independent management key sync was skipped: %v", err)
				}
				wantRefresh := 1
				if outcome == "upstream-failure" {
					wantRefresh = 0
				}
				if writes.Load() != 1 || refresh.count() != wantRefresh {
					t.Fatalf("writes=%d refresh=%d", writes.Load(), refresh.count())
				}
				// 后续正常一轮仍可落下新快照，不留下持续跳过状态。
				delete(fetcher.standardHooks, "codex")
				if provider {
					fetcher.standardResults["codex"].Payload[0].Priority = metadataIntPointer(10)
					fetcher.standardResults["codex"].Payload[0].Disabled = metadataBoolPointer(true)
				} else {
					fetcher.credentialsResult.Payload.Files[0].Priority = metadataIntPointer(10)
					fetcher.credentialsResult.Payload.Files[0].Disabled = metadataBoolPointer(true)
				}
				if err := syncer.SyncMetadata(ctx); err != nil {
					t.Fatal(err)
				}
				fresh := read()
				requirePriority(t, fresh.Priority, 10)
				if fresh.Disabled == nil || !*fresh.Disabled {
					t.Fatalf("fresh sync did not persist disabled state: %+v", fresh)
				}
			})
		}
	}
}

func TestMetadataSnapshotDoesNotRevertOpenAIGroupPriority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := openMetadataTestDatabase(t, "metadata-openai-mutation.db")
	fetcher := newMetadataTestFetcher()
	fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{
		{Name: "group", Priority: metadataIntPointer(0), APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "a", AuthIndex: "target"}, {APIKey: "b", AuthIndex: "sibling"}}},
		{Name: "other", Priority: metadataIntPointer(2), APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "c", AuthIndex: "other"}}},
	}
	coordinator := &service.CredentialMetadataCoordinator{}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{MetadataFetcher: fetcher, CredentialMetadata: coordinator})
	if err := syncer.SyncMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	// 配置本身也可能在旧快照里不同；必须跳过整份过期凭据快照。
	fetcher.openAIResult.Payload[0].Name = "stale-group"
	releaseSnapshot := startHeldMetadataSnapshot(t, ctx, syncer, fetcher)
	var reads, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			reads.Add(1)
			fmt.Fprint(w, `[{"name":"group","priority":0,"keys":[{"api-key":"a","auth_index":"target"},{"api-key":"b","auth_index":"sibling"}]},{"name":"other","priority":2,"keys":[{"api-key":"c","auth_index":"other"}]}]`)
			return
		}
		writes.Add(1)
	}))
	defer server.Close()
	refresh := &credentialStatusRefresherStub{}
	mutation := service.NewCredentialPriorityService(db, cpa.NewClient(server.URL, "management-secret", time.Second, false), refresh, &service.CredentialMutationLocks{}, coordinator)
	if _, err := mutation.SetAIProviderPriority(ctx, "target", 11); err != nil {
		t.Fatal(err)
	}
	before := loadMetadataIdentityMap(t, db)
	for _, index := range []string{"target", "sibling"} {
		requirePriority(t, before[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, index)].Priority, 11)
	}
	requirePriority(t, before[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "other")].Priority, 2)
	if err := releaseSnapshot(); err != nil {
		t.Fatal(err)
	}
	if after := loadMetadataIdentityMap(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("old OpenAI snapshot replaced saved group: before=%+v after=%+v", before, after)
	}
	if reads.Load() != 1 || writes.Load() != 1 || refresh.count() != 1 {
		t.Fatalf("reads=%d writes=%d refresh=%d", reads.Load(), writes.Load(), refresh.count())
	}
	delete(fetcher.standardHooks, "codex")
	fetcher.openAIResult.Payload[0].Name = "fresh-group"
	fetcher.openAIResult.Payload[0].Priority = metadataIntPointer(11)
	if err := syncer.SyncMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	fresh := loadMetadataIdentityMap(t, db)
	for _, index := range []string{"target", "sibling"} {
		row := fresh[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, index)]
		if row.Name != "fresh-group" {
			t.Fatalf("fresh group did not persist: %+v", row)
		}
		requirePriority(t, row.Priority, 11)
	}
}

// Credentials 已读取；provider 读取被阻塞，尚未进入任何凭据本地 diff/写入。
func startHeldMetadataSnapshot(t *testing.T, ctx context.Context, syncer *service.SyncService, fetcher *metadataTestFetcher) func() error {
	t.Helper()
	captured, release := make(chan struct{}), make(chan struct{})
	old := fetcher.standardResults["codex"]
	fetcher.standardHooks["codex"] = func(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
		close(captured)
		select {
		case <-release:
			return old, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() { done <- syncer.SyncMetadata(ctx) }()
	select {
	case <-captured:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return func() error {
		close(release)
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
