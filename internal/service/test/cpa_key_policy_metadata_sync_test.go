package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/cpaapikeys"
	"cpa-usage-keeper/internal/cpa/dto/keypolicy"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestSyncMetadataImportsNativeAndPluginKeys(t *testing.T) {
	db := openMetadataTestDatabase(t, "native-plugin-keys.db")
	fetcher := newMetadataTestFetcher()
	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-native"}}}
	fetcher.cpaKeyPolicyResult = &response.CPAKeyPolicyKeysResult{StatusCode: 200, Payload: keypolicy.KeysResponse{Keys: []keypolicy.Key{
		{ID: "marketplace1", Name: "белослудцев", KeyPreview: "cpa_mar...ce1", Enabled: true},
		{ID: "task-dispatcher", Name: "Task Dispatcher", KeyPreview: "cpa_tas...her", Enabled: false},
	}}}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	rows, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	byKey := make(map[string]entities.CPAAPIKey, len(rows))
	for _, row := range rows {
		byKey[row.APIKey] = row
	}
	if len(byKey) != 3 || byKey["sk-native"].Source != entities.CPAAPIKeySourceNative {
		t.Fatalf("native and plugin snapshot did not coexist: %+v", byKey)
	}
	if row := byKey["marketplace1"]; row.KeyAlias != "белослудцев" || row.DisplayKey != "cpa_mar...ce1" || row.Source != entities.CPAAPIKeySourceCPAKeyPolicy {
		t.Fatalf("marketplace1 mapping = %+v", row)
	}
	if row := byKey["task-dispatcher"]; row.KeyAlias != "Task Dispatcher" || row.IsDeleted {
		t.Fatalf("disabled plugin key lost metadata: %+v", row)
	}
}

func TestSyncMetadataPluginFailurePreservesPluginAndReconcilesNative(t *testing.T) {
	db := openMetadataTestDatabase(t, "plugin-unavailable.db")
	fetcher := newMetadataTestFetcher()
	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-old"}}}
	fetcher.cpaKeyPolicyResult = &response.CPAKeyPolicyKeysResult{StatusCode: 200, Payload: keypolicy.KeysResponse{Keys: []keypolicy.Key{{ID: "marketplace1", Name: "белослудцев", KeyPreview: "cpa_mar...ce1", Enabled: true}}}}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("initial SyncMetadata returned error: %v", err)
	}

	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-new"}}}
	fetcher.cpaKeyPolicyResult = nil
	fetcher.cpaKeyPolicyErr = errors.New("plugin endpoint unavailable")
	err := syncer.SyncMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fetch cpa-key-policy keys") {
		t.Fatalf("expected plugin warning, got %v", err)
	}
	rows, listErr := repository.ListActiveCPAAPIKeys(db)
	if listErr != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", listErr)
	}
	active := make(map[string]entities.CPAAPIKey, len(rows))
	for _, row := range rows {
		active[row.APIKey] = row
	}
	if len(active) != 2 || active["sk-new"].Source != entities.CPAAPIKeySourceNative || active["marketplace1"].KeyAlias != "белослудцев" {
		t.Fatalf("partial failure reconcile = %+v", active)
	}
	if _, ok := active["sk-old"]; ok {
		t.Fatalf("native authoritative snapshot was not reconciled: %+v", active)
	}
}

func TestSyncMetadataExplicitEmptyPluginSnapshotDeletesOnlyPluginKeys(t *testing.T) {
	db := openMetadataTestDatabase(t, "plugin-empty.db")
	fetcher := newMetadataTestFetcher()
	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-native"}}}
	fetcher.cpaKeyPolicyResult = &response.CPAKeyPolicyKeysResult{StatusCode: 200, Payload: keypolicy.KeysResponse{Keys: []keypolicy.Key{{ID: "marketplace1", Name: "белослудцев", Enabled: true}}}}
	currentNow := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return currentNow }})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("initial SyncMetadata returned error: %v", err)
	}
	fetcher.cpaKeyPolicyResult = &response.CPAKeyPolicyKeysResult{StatusCode: 200, Payload: keypolicy.KeysResponse{Keys: []keypolicy.Key{}}}
	currentNow = currentNow.Add(time.Hour)
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("empty plugin SyncMetadata returned error: %v", err)
	}
	rows, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].APIKey != "sk-native" || rows[0].Source != entities.CPAAPIKeySourceNative {
		t.Fatalf("explicit empty plugin snapshot changed native keys: %+v", rows)
	}
}
