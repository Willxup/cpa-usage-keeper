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
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

// 模拟 GET 后其他配置写入推进整个文件版本；Keeper 必须停止本次操作，保留本地与远端的新状态。
func TestProviderConfigConflictPreservesLocalStateWithoutRetry(t *testing.T) {
	for _, operation := range []string{"status", "priority", "openai-priority"} {
		t.Run(operation, func(t *testing.T) {
			db := openMetadataTestDatabase(t, "provider-conflict.db")
			providerType := "codex"
			if operation == "openai-priority" {
				providerType = "openai"
			}
			for _, index := range []string{"target", "sibling"} {
				seedCredentialStatusIdentity(t, db, entities.UsageIdentity{
					Name: index, Identity: index, Type: providerType, AuthType: entities.UsageIdentityAuthTypeAIProvider,
					Priority: metadataIntPointer(0), Disabled: metadataBoolPointer(false),
				})
			}
			before := loadMetadataIdentityMap(t, db)
			var reads, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					reads.Add(1)
					w.Header().Set("ETag", `"before-external-change"`)
					fmt.Fprint(w, `[{"name":"group","priority":0,"keys":[{"api-key":"a","auth_index":"target"},{"api-key":"b","auth_index":"sibling"}]}]`)
					return
				}
				if r.Method != http.MethodPut {
					t.Errorf("unexpected method %s", r.Method)
				}
				writes.Add(1)
				if got := r.Header.Get("If-Match"); got != `"before-external-change"` {
					t.Errorf("If-Match = %q", got)
				}
				w.Header().Set("ETag", `"after-external-change"`)
				w.WriteHeader(http.StatusPreconditionFailed)
				fmt.Fprint(w, `{"error":"precondition_failed","detail":"upstream secret-xyz"}`)
			}))
			defer server.Close()
			client := cpa.NewClient(server.URL, "management-secret", time.Second, false)
			refresh := &credentialStatusRefresherStub{}
			locks := &service.CredentialMutationLocks{}
			var err error
			if operation == "status" {
				_, err = service.NewCredentialStatusService(db, client, refresh, locks, nil).SetAIProviderDisabled(context.Background(), "target", true)
			} else {
				_, err = service.NewCredentialPriorityService(db, client, refresh, locks, nil).SetAIProviderPriority(context.Background(), "target", 10)
			}
			if !errors.Is(err, service.ErrCredentialConfigChanged) {
				t.Fatalf("error = %v, want configuration changed", err)
			}
			if after := loadMetadataIdentityMap(t, db); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed PUT changed local identities: before=%+v after=%+v", before, after)
			}
			if reads.Load() != 1 || writes.Load() != 1 || refresh.count() != 0 {
				t.Fatalf("reads=%d writes=%d refresh=%d", reads.Load(), writes.Load(), refresh.count())
			}
		})
	}
}
