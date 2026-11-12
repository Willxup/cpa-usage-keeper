package test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/credentials"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestSyncCredentialsSIWCSnapshot(t *testing.T) {
	db := openMetadataTestDatabase(t, "siwc.db")
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	fetcher := newMetadataTestFetcher()
	decode := func(raw string) credentials.CredentialsResponse {
		t.Helper()
		var payload credentials.CredentialsResponse
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	payload := decode(`{"files":[
		{"auth_index":"shared","type":" Codex ","name":"before.json","siwc_bind_supported":true,"siwc_bound":true,"use_siwc":true,"siwc_status":"valid","siwc_expires_at":"2026-11-01T15:30:00+08:00"},
		{"auth_index":"claude","type":"claude","siwc_bind_supported":true,"siwc_bound":true,"use_siwc":true,"siwc_status":"valid","siwc_expires_at":"2026-11-01T15:30:00+08:00"}
	]}`)
	fetcher.setCredentials(payload.Files)
	fetcher.standardResults["codex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{AuthIndex: "shared", APIKey: "provider-key"}}}
	syncer := newMetadataTestSyncer(db, fetcher, func() time.Time { return now })
	sync := func() {
		t.Helper()
		if err := syncer.SyncMetadata(ctx); err != nil {
			t.Fatal(err)
		}
	}
	read := func(authType entities.UsageIdentityAuthType, index string) entities.UsageIdentity {
		t.Helper()
		rows, err := repository.ListUsageIdentities(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.AuthType == authType && row.Identity == index {
				return row
			}
		}
		t.Fatalf("missing identity %d/%s", authType, index)
		return entities.UsageIdentity{}
	}
	assertUnknown := func(row entities.UsageIdentity) {
		t.Helper()
		if row.SIWCBindSupported != nil || row.SIWCBound != nil || row.UseSIWC != nil || row.SIWCStatus != nil || row.SIWCExpiresAt != nil {
			t.Fatalf("expected unknown SIWC fields: %+v", row)
		}
	}
	sync()
	row := read(entities.UsageIdentityAuthTypeCredential, "shared")
	expiry := time.Date(2026, 11, 1, 7, 30, 0, 0, time.UTC)
	if row.SIWCBindSupported == nil || !*row.SIWCBindSupported || row.SIWCBound == nil || !*row.SIWCBound || row.UseSIWC == nil || !*row.UseSIWC || row.SIWCStatus == nil || *row.SIWCStatus != "valid" || row.SIWCExpiresAt == nil || !row.SIWCExpiresAt.Equal(expiry) {
		t.Fatalf("insert: %+v", row)
	}
	assertUnknown(read(entities.UsageIdentityAuthTypeCredential, "claude"))
	assertUnknown(read(entities.UsageIdentityAuthTypeAIProvider, "shared"))
	if err := db.Model(&entities.UsageIdentity{}).Where("id = ?", row.ID).Updates(map[string]any{"alias": "my alias", "total_requests": 37, "last_aggregated_usage_event_id": 71}).Error; err != nil {
		t.Fatal(err)
	}

	// 成功响应按 auth_index 原位更新；false 与旧值和 NULL 均不同。
	now = now.Add(time.Hour)
	fetcher.setCredentials(decode(`{"files":[{"auth_index":"shared","type":"codex","name":"after.json","siwc_bind_supported":false,"siwc_bound":false,"use_siwc":false,"siwc_status":"off","siwc_expires_at":"2026-11-02T01:00:00-07:00"}]}`).Files)
	sync()
	updated := read(entities.UsageIdentityAuthTypeCredential, "shared")
	newExpiry := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	if updated.ID != row.ID || updated.Name != "after.json" || updated.Alias == nil || *updated.Alias != "my alias" || updated.TotalRequests != 37 || updated.LastAggregatedUsageEventID != 71 || updated.SIWCBindSupported == nil || *updated.SIWCBindSupported || updated.SIWCBound == nil || *updated.SIWCBound || updated.UseSIWC == nil || *updated.UseSIWC || updated.SIWCStatus == nil || *updated.SIWCStatus != "off" || updated.SIWCExpiresAt == nil || !updated.SIWCExpiresAt.Equal(newExpiry) {
		t.Fatalf("update: %+v", updated)
	}

	// 同一快照无需写入；抓取失败不得清空任何字段或推进版本。
	now = now.Add(time.Hour)
	sync()
	same := read(entities.UsageIdentityAuthTypeCredential, "shared")
	if !same.UpdatedAt.Equal(updated.UpdatedAt) {
		t.Fatalf("unchanged snapshot rewrote row: %v -> %v", updated.UpdatedAt, same.UpdatedAt)
	}
	fetcher.credentialsErr = errors.New("offline")
	fetcher.credentialsResult = nil
	if err := syncer.SyncMetadata(ctx); err == nil {
		t.Fatal("expected fetch failure")
	}
	retained := read(entities.UsageIdentityAuthTypeCredential, "shared")
	if retained.SIWCStatus == nil || *retained.SIWCStatus != "off" || retained.UseSIWC == nil || *retained.UseSIWC || retained.SIWCExpiresAt == nil || !retained.SIWCExpiresAt.Equal(newExpiry) || !retained.UpdatedAt.Equal(same.UpdatedAt) {
		t.Fatalf("fetch failure lost snapshot: %+v", retained)
	}

	fetcher.setCredentials(decode(`{"files":[{"auth_index":"shared","type":"codex","name":"after.json"}]}`).Files)
	sync()
	cleared := read(entities.UsageIdentityAuthTypeCredential, "shared")
	assertUnknown(cleared)
	if cleared.ID != row.ID || cleared.Alias == nil || *cleared.Alias != "my alias" || cleared.TotalRequests != 37 {
		t.Fatalf("clear changed identity or local data: %+v", cleared)
	}
	assertUnknown(read(entities.UsageIdentityAuthTypeAIProvider, "shared"))
	if got := fetcher.callCount("credentials"); got != 5 {
		t.Fatalf("credentials requests = %d, want 5", got)
	}
}

func TestSyncCredentialsSIWCFieldsClearWhenTypeChanges(t *testing.T) {
	db := openMetadataTestDatabase(t, "siwc-type.db")
	fetcher := newMetadataTestFetcher()
	fetcher.setCredentials([]credentials.Credential{{AuthIndex: "same", Type: "codex", SIWCBound: new(true), UseSIWC: new(true), SIWCStatus: new("valid")}})
	syncer := newMetadataTestSyncer(db, fetcher, time.Now)
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatal(err)
	}
	fetcher.setCredentials([]credentials.Credential{{AuthIndex: "same", Type: "claude", SIWCBound: new(true), UseSIWC: new(true), SIWCStatus: new("valid")}})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatal(err)
	}
	row := loadMetadataIdentityMap(t, db)[metadataIdentityKey(entities.UsageIdentityAuthTypeCredential, "same")]
	if row.Type != "claude" || row.SIWCBound != nil || row.UseSIWC != nil || row.SIWCStatus != nil {
		t.Fatalf("non-codex retained SIWC: %+v", row)
	}
}
