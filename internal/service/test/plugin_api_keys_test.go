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
	for _, tc := range []struct {
		name     string
		metadata map[string]any
		plugin   bool
	}{
		{"plugin", map[string]any{"is_native_key": false, "access_provider": "plugin:example:auth"}, true},
		{"native", map[string]any{"is_native_key": true, "access_provider": "config-api-key"}, false},
		{"legacy", nil, false},
		{"missing source", map[string]any{"is_native_key": false}, false},
		{"missing flag", map[string]any{"access_provider": "plugin:example:auth"}, false},
		{"non plugin source", map[string]any{"is_native_key": false, "access_provider": "custom-auth"}, false},
		{"empty plugin source", map[string]any{"is_native_key": false, "access_provider": "plugin:"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"execution_id": "execution", "api_key": `team:01:"\\用户`}
			for key, value := range tc.metadata {
				payload[key] = value
			}
			raw, _ := json.Marshal(payload)
			event, _, err := service.DecodeRedisUsageMessage(string(raw), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			want := payload["api_key"].(string)
			if tc.plugin {
				want = helper.PluginAPIKeyGroupKey(payload["access_provider"].(string), want)
			}
			if event.APIGroupKey != want {
				t.Fatalf("unexpected group: %q", event.APIGroupKey)
			}
			if _, exists := tc.metadata["is_native_key"]; exists != (event.IsNativeKey != nil) {
				t.Fatal("lost nullable native metadata")
			}
			if _, exists := tc.metadata["access_provider"]; exists != (event.AccessProvider != nil) {
				t.Fatal("lost nullable provider metadata")
			}
		})
	}
	for _, key := range []string{"", "   "} {
		payload, _ := json.Marshal(map[string]any{"execution_id": "execution", "api_key": key, "provider": "fallback", "is_native_key": false, "access_provider": "plugin:test:auth"})
		event, _, err := service.DecodeRedisUsageMessage(string(payload), time.Now())
		if err != nil || event.APIGroupKey != "fallback" {
			t.Fatalf("empty identity was encoded: %+v %v", event, err)
		}
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
