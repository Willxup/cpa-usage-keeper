package test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func TestDecodePluginAPIKeyMetadataKeepsLegacyAndNativeGroups(t *testing.T) {
	const key = `team:01:"\\用户`
	for _, tc := range []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{"plugin", map[string]any{"is_native_key": false, "access_provider": "plugin:example:auth"}, helper.PluginAPIKeyGroupKey("plugin:example:auth", key)},
		{"native", map[string]any{"is_native_key": true, "access_provider": "config-api-key"}, key},
		{"legacy", nil, key},
		{"missing source", map[string]any{"is_native_key": false}, key},
		{"missing flag", map[string]any{"access_provider": "plugin:example:auth"}, key},
		{"non plugin source", map[string]any{"is_native_key": false, "access_provider": "custom-auth"}, key},
		{"empty plugin source", map[string]any{"is_native_key": false, "access_provider": "plugin:"}, key},
		{"empty key", map[string]any{"api_key": "", "is_native_key": false, "access_provider": "plugin:test:auth"}, "fallback"},
		{"blank key", map[string]any{"api_key": "   ", "is_native_key": false, "access_provider": "plugin:test:auth"}, "fallback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"execution_id": "execution", "api_key": key, "provider": "fallback"}
			for field, value := range tc.metadata {
				payload[field] = value
			}
			raw, _ := json.Marshal(payload)
			event, _, err := service.DecodeRedisUsageMessage(string(raw), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if event.APIGroupKey != tc.want {
				t.Fatalf("group = %q, want %q", event.APIGroupKey, tc.want)
			}
			if _, exists := tc.metadata["is_native_key"]; exists != (event.IsNativeKey != nil) {
				t.Fatal("lost nullable native metadata")
			}
			if _, exists := tc.metadata["access_provider"]; exists != (event.AccessProvider != nil) {
				t.Fatal("lost nullable provider metadata")
			}
		})
	}
}

type committedPluginAPIKeyRecorder struct {
	db         *gorm.DB
	calls      int
	identities []entities.PluginAPIKeyIdentity
	eventCount int64
	err        error
}

func (r *committedPluginAPIKeyRecorder) NotifyPluginAPIKeys(identities []entities.PluginAPIKeyIdentity) {
	r.calls++
	r.identities = identities
	r.err = r.db.Model(&entities.UsageEvent{}).Count(&r.eventCount).Error
}

func TestPluginAPIKeyNotificationsRequireCommittedUsageAndDeduplicateIdentities(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", failCommit), func(t *testing.T) {
			db := openSyncTestDatabase(t)
			for i := 0; i < 2; i++ {
				seedRedisInboxMessagesForTest(t, db, fmt.Sprintf(`{"execution_id":"plugin-%d","provider":"codex","executor_type":"CodexExecutor","api_key":"team","is_native_key":false,"access_provider":"plugin:example:auth","tokens":{"total_tokens":1}}`, i))
			}
			if failCommit {
				if err := db.Exec("CREATE TRIGGER fail_plugin_usage_commit BEFORE UPDATE OF status ON redis_usage_inboxes WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'injected commit failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			recorder := &committedPluginAPIKeyRecorder{db: db}
			s := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: emptyPricingCatalogForTest(), PluginAPIKeys: recorder, UsageAggregationNotifier: &recordingUsageAggregationNotifier{}})
			_, err := s.ProcessRedisUsageInbox(context.Background())
			if failCommit {
				if err == nil || recorder.calls != 0 {
					t.Fatalf("rollback sent notification: err=%v calls=%d", err, recorder.calls)
				}
				var count int64
				if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("usage transaction did not roll back: count=%d err=%v", count, err)
				}
				if err := db.Exec("DROP TRIGGER fail_plugin_usage_commit").Error; err != nil {
					t.Fatal(err)
				}
				_, err = s.ProcessRedisUsageInbox(context.Background())
			}
			if err != nil || recorder.err != nil || recorder.calls != 1 || len(recorder.identities) != 1 || recorder.eventCount != 2 {
				t.Fatalf("unexpected committed identities: %+v processErr=%v", recorder, err)
			}
			if recorder.identities[0].APIGroupKey != helper.PluginAPIKeyGroupKey("plugin:example:auth", "team") {
				t.Fatal("notification must reuse persisted group identity")
			}
		})
	}
}
