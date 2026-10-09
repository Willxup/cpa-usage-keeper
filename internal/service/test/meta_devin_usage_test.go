package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
)

func TestProcessRedisUsageInboxNormalizesMetaAndDevinAuthFileUsage(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	if err := db.Create([]entities.UsageIdentity{
		{
			Name:         "Meta OAuth",
			AuthType:     entities.UsageIdentityAuthTypeAuthFile,
			AuthTypeName: "oauth",
			Identity:     "meta-auth-index",
			Type:         "meta",
			Provider:     "meta",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			Name:         "Devin OAuth",
			AuthType:     entities.UsageIdentityAuthTypeAuthFile,
			AuthTypeName: "oauth",
			Identity:     "devin-auth-index",
			Type:         "devin",
			Provider:     "devin",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}).Error; err != nil {
		t.Fatalf("seed Meta/Devin Auth File identities: %v", err)
	}

	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{
		{
			Source: "meta-auth@example.com",
			RawMessage: `{
				"timestamp":"2026-09-19T08:00:00Z",
				"provider":"meta",
				"executor_type":"MetaExecutor",
				"auth_type":"oauth",
				"auth_index":"meta-auth-index",
				"request_id":"meta-auth-usage",
				"tokens":{"input_tokens":100,"output_tokens":20,"reasoning_tokens":5,"cached_tokens":30,"cache_read_tokens":30,"cache_creation_tokens":10,"total_tokens":120},
				"accounting_version":2,
				"token_breakdown":{"schema_version":2,"quality":"complete","total_tokens":120,"input":{"total_tokens":100,"uncached_tokens":60,"cache_read_tokens":30,"cache_write_tokens":10},"output":{"total_tokens":20,"non_reasoning_tokens":15,"reasoning_tokens":5},"unclassified_tokens":0
			}}`,
			PoppedAt: now,
		},
		{
			Source: "devin-auth@example.com",
			RawMessage: `{
				"timestamp":"2026-09-19T08:00:01Z",
				"provider":"devin",
				"executor_type":"DevinExecutor",
				"auth_type":"oauth",
				"auth_index":"devin-auth-index",
				"request_id":"devin-auth-usage",
				"tokens":{"input_tokens":6,"output_tokens":6,"reasoning_tokens":3,"cached_tokens":1,"cache_read_tokens":1,"cache_creation_tokens":0,"total_tokens":15},
				"accounting_version":2,
				"token_breakdown":{"schema_version":2,"quality":"complete","total_tokens":15,"input":{"total_tokens":6,"uncached_tokens":5,"cache_read_tokens":1,"cache_write_tokens":0},"output":{"total_tokens":9,"non_reasoning_tokens":6,"reasoning_tokens":3},"unclassified_tokens":0
			}}`,
			PoppedAt: now.Add(time.Second),
		},
	})
	if err != nil {
		t.Fatalf("seed Meta/Devin Redis usage inbox rows: %v", err)
	}

	result, err := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"}).ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 2 {
		t.Fatalf("expected two normalized Meta/Devin events, got %+v", result)
	}

	metaEvent := loadTokenProcessorSyncEvent(t, db, "meta-auth-usage")
	if metaEvent.AuthType != "oauth" || metaEvent.AuthIndex != "meta-auth-index" || metaEvent.InputTokens != 100 || metaEvent.OutputTokens != 20 || metaEvent.ReasoningTokens != 5 || metaEvent.TotalTokens != 120 {
		t.Fatalf("unexpected Meta Auth File usage event: %+v", metaEvent)
	}
	devinEvent := loadTokenProcessorSyncEvent(t, db, "devin-auth-usage")
	if devinEvent.AuthType != "oauth" || devinEvent.AuthIndex != "devin-auth-index" || devinEvent.InputTokens != 6 || devinEvent.OutputTokens != 6 || devinEvent.ReasoningTokens != 3 || devinEvent.TotalTokens != 15 {
		t.Fatalf("unexpected Devin Auth File usage event: %+v", devinEvent)
	}

	var metaIdentity, devinIdentity entities.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", entities.UsageIdentityAuthTypeAuthFile, "meta-auth-index").First(&metaIdentity).Error; err != nil {
		t.Fatalf("load Meta Auth File identity: %v", err)
	}
	if err := db.Where("auth_type = ? AND identity = ?", entities.UsageIdentityAuthTypeAuthFile, "devin-auth-index").First(&devinIdentity).Error; err != nil {
		t.Fatalf("load Devin Auth File identity: %v", err)
	}
	if metaIdentity.Type != "meta" || metaIdentity.TotalTokens != 120 || metaIdentity.OutputTokens != 20 || metaIdentity.ReasoningTokens != 5 {
		t.Fatalf("Meta usage was not aggregated to its Auth File identity: %+v", metaIdentity)
	}
	if devinIdentity.Type != "devin" || devinIdentity.TotalTokens != 15 || devinIdentity.OutputTokens != 6 || devinIdentity.ReasoningTokens != 3 {
		t.Fatalf("Devin usage was not aggregated to its Auth File identity: %+v", devinIdentity)
	}
}
