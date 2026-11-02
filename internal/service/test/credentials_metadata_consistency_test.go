package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/credentials"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestCredentialBulkMutationsInvalidateOldMetadataOnAnySuccess(t *testing.T) {
	for _, operation := range []string{"status", "delete"} {
		for _, outcome := range []string{"success", "partial", "failure"} {
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				db := openMetadataTestDatabase(t, "bulk-metadata.db")
				fetcher := newMetadataTestFetcher()
				files := []credentials.Credential{
					{AuthIndex: "target", Name: "target.json", Type: "codex", Disabled: metadataBoolPointer(false)},
					{AuthIndex: "other", Name: "other.json", Type: "codex", Disabled: metadataBoolPointer(false)},
				}
				fetcher.setCredentials(files)
				coordinator := &service.CredentialMetadataCoordinator{}
				syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{MetadataFetcher: fetcher, CredentialMetadata: coordinator})
				if err := syncer.SyncMetadata(ctx); err != nil {
					t.Fatal(err)
				}
				before := loadMetadataIdentityMap(t, db)
				// 批量入口没有本地写回；用额外旧字段变化观察是否错误应用了快照。
				for i := range files {
					files[i].Email = "stale-snapshot"
				}
				releaseSnapshot := startHeldMetadataSnapshot(t, ctx, syncer, fetcher)
				var writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					writes.Add(1)
					if got := r.Header.Get("If-Match"); got != "" {
						t.Errorf("Credential mutation received If-Match %q", got)
					}
					if operation == "status" {
						var body struct {
							Name      string `json:"name"`
							AuthIndex string `json:"auth_index"`
							Disabled  bool   `json:"disabled"`
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
							return
						}
						if r.Method != http.MethodPatch || body.AuthIndex != "" || !body.Disabled {
							t.Errorf("invalid bulk status request: %s %+v", r.Method, body)
						}
						if outcome == "failure" || outcome == "partial" && body.Name == "other.json" {
							w.WriteHeader(http.StatusBadGateway)
						}
						return
					}
					if r.Method != http.MethodDelete {
						t.Errorf("unexpected delete method %s", r.Method)
					}
					switch outcome {
					case "partial":
						w.WriteHeader(http.StatusMultiStatus)
						fmt.Fprint(w, `{"status":"partial","deleted":1,"files":["target.json"],"failed":[{"name":"other.json","error":"failed"}]}`)
					case "failure":
						w.WriteHeader(http.StatusBadGateway)
					default:
						fmt.Fprint(w, `{"status":"ok","deleted":2}`)
					}
				}))
				defer server.Close()
				refresh := &credentialStatusRefresherStub{}
				mutation := service.NewCredentialsManagementService(cpa.NewClient(server.URL, "management-secret", time.Second, false), refresh, &service.CredentialMutationLocks{}, coordinator)
				var err error
				if operation == "status" {
					_, err = mutation.SetCredentialsDisabled(ctx, []string{"target.json", "other.json"}, true)
				} else {
					_, err = mutation.DeleteCredentials(ctx, []string{"target.json", "other.json"})
				}
				wantError := outcome == "failure" || operation == "status" && outcome == "partial"
				if (err != nil) != wantError {
					t.Fatalf("error=%v, want error=%v", err, wantError)
				}
				if err := releaseSnapshot(); err != nil {
					t.Fatal(err)
				}
				after := loadMetadataIdentityMap(t, db)
				if outcome == "failure" {
					if after[metadataIdentityKey(entities.UsageIdentityAuthTypeCredential, "target")].Name != "stale-snapshot" || refresh.count() != 0 {
						t.Fatalf("failed batch invalidated metadata or queued refresh: rows=%+v refresh=%d", after, refresh.count())
					}
				} else if !reflect.DeepEqual(before, after) || refresh.count() != 1 {
					t.Fatalf("successful batch applied obsolete snapshot or lost refresh: before=%+v after=%+v refresh=%d", before, after, refresh.count())
				}
				wantWrites := int32(1)
				if operation == "status" {
					wantWrites = 2
				}
				if writes.Load() != wantWrites {
					t.Fatalf("writes=%d, want %d", writes.Load(), wantWrites)
				}
				// 新一轮按远端实际结果对齐，删除仍沿用既有 stale 标记。
				delete(fetcher.standardHooks, "codex")
				fresh := make([]credentials.Credential, 0, len(files))
				for _, file := range files {
					succeeded := outcome == "success" || outcome == "partial" && file.AuthIndex == "target"
					if operation == "delete" && succeeded {
						continue
					}
					file.Email = "fresh-snapshot"
					file.Disabled = metadataBoolPointer(operation == "status" && succeeded)
					fresh = append(fresh, file)
				}
				fetcher.setCredentials(fresh)
				if err := syncer.SyncMetadata(ctx); err != nil {
					t.Fatal(err)
				}
				rows := loadMetadataIdentityMap(t, db)
				for _, index := range []string{"target", "other"} {
					row := rows[metadataIdentityKey(entities.UsageIdentityAuthTypeCredential, index)]
					succeeded := outcome == "success" || outcome == "partial" && index == "target"
					if operation == "delete" {
						if row.IsDeleted != succeeded {
							t.Fatalf("fresh delete state differs: %+v", row)
						}
					} else if row.Disabled == nil || *row.Disabled != succeeded {
						t.Fatalf("fresh status differs: %+v", row)
					}
				}
			})
		}
	}
}
