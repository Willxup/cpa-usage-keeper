package repository

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/models"
)

func TestUsageIdentityReplaceForAuthTypeMarksStaleRowsDeletedAndPreservesStats(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	firstUsedAt := now.Add(-2 * time.Hour)
	lastUsedAt := now.Add(-time.Hour)
	statsUpdatedAt := now.Add(-30 * time.Minute)

	existingActive := models.UsageIdentity{
		Name:                       "Old Name",
		AuthType:                   models.UsageIdentityAuthTypeAuthFile,
		Identity:                   "auth-1",
		Type:                       "account",
		Provider:                   "claude",
		TotalRequests:              10,
		SuccessCount:               8,
		FailureCount:               2,
		InputTokens:                100,
		OutputTokens:               50,
		TotalTokens:                150,
		LastAggregatedUsageEventID: 42,
		FirstUsedAt:                &firstUsedAt,
		LastUsedAt:                 &lastUsedAt,
		StatsUpdatedAt:             &statsUpdatedAt,
	}
	existingStale := models.UsageIdentity{
		Name:     "Stale",
		AuthType: models.UsageIdentityAuthTypeAuthFile,
		Identity: "auth-stale",
		Type:     "account",
		Provider: "claude",
	}
	unrelatedProvider := models.UsageIdentity{
		Name:     "Provider",
		AuthType: models.UsageIdentityAuthTypeAIProvider,
		Identity: "provider-1",
		Type:     "openai",
		Provider: "OpenAI",
	}
	if err := db.Create(&[]models.UsageIdentity{existingActive, existingStale, unrelatedProvider}).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}

	err := ReplaceUsageIdentitiesForAuthType(ctx, db, []models.UsageIdentity{
		{
			Name:         "New Name",
			AuthTypeName: "oauth",
			Identity:     "auth-1",
			Type:         "account",
			Provider:     "claude-code",
		},
		{
			Name:         "New Auth",
			AuthTypeName: "oauth",
			Identity:     "auth-2",
			Type:         "account",
			Provider:     "claude-code",
		},
	}, models.UsageIdentityAuthTypeAuthFile, now)
	if err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForAuthType returned error: %v", err)
	}

	rows, err := ListUsageIdentities(ctx, db)
	if err != nil {
		t.Fatalf("ListUsageIdentities returned error: %v", err)
	}
	byIdentity := usageIdentitiesByIdentity(rows)

	updated := byIdentity["auth-1"]
	if updated.Name != "New Name" || updated.Provider != "claude-code" || updated.AuthType != models.UsageIdentityAuthTypeAuthFile || updated.IsDeleted {
		t.Fatalf("expected active metadata update for auth-1, got %+v", updated)
	}
	if updated.TotalRequests != 10 || updated.SuccessCount != 8 || updated.FailureCount != 2 || updated.InputTokens != 100 || updated.OutputTokens != 50 || updated.TotalTokens != 150 || updated.LastAggregatedUsageEventID != 42 {
		t.Fatalf("expected stats to be preserved, got %+v", updated)
	}
	if updated.FirstUsedAt == nil || !updated.FirstUsedAt.Equal(firstUsedAt) || updated.LastUsedAt == nil || !updated.LastUsedAt.Equal(lastUsedAt) || updated.StatsUpdatedAt == nil || !updated.StatsUpdatedAt.Equal(statsUpdatedAt) {
		t.Fatalf("expected usage timestamps to be preserved, got %+v", updated)
	}

	inserted := byIdentity["auth-2"]
	if inserted.ID == 0 || inserted.IsDeleted || inserted.AuthType != models.UsageIdentityAuthTypeAuthFile || inserted.Name != "New Auth" {
		t.Fatalf("expected active inserted auth-2, got %+v", inserted)
	}

	stale := byIdentity["auth-stale"]
	if !stale.IsDeleted || stale.DeletedAt == nil || !stale.DeletedAt.Equal(now) {
		t.Fatalf("expected stale auth identity to be deleted at %s, got %+v", now, stale)
	}

	provider := byIdentity["provider-1"]
	if provider.IsDeleted || provider.DeletedAt != nil {
		t.Fatalf("expected unrelated provider identity untouched, got %+v", provider)
	}
}

func TestUsageIdentityReplaceForAuthTypeRestoresDeletedIdentity(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	deletedAt := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	now := deletedAt.Add(24 * time.Hour)

	deleted := models.UsageIdentity{
		Name:          "Deleted",
		AuthType:      models.UsageIdentityAuthTypeAuthFile,
		AuthTypeName:  "oauth",
		Identity:      "auth-1",
		Type:          "account",
		Provider:      "claude",
		TotalRequests: 7,
		IsDeleted:     true,
		DeletedAt:     &deletedAt,
	}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatalf("seed deleted identity: %v", err)
	}

	err := ReplaceUsageIdentitiesForAuthType(ctx, db, []models.UsageIdentity{
		{
			Name:         "Restored",
			AuthTypeName: "oauth",
			Identity:     "auth-1",
			Type:         "account",
			Provider:     "claude-code",
		},
	}, models.UsageIdentityAuthTypeAuthFile, now)
	if err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForAuthType returned error: %v", err)
	}

	rows, err := ListUsageIdentities(ctx, db)
	if err != nil {
		t.Fatalf("ListUsageIdentities returned error: %v", err)
	}
	restored := usageIdentitiesByIdentity(rows)["auth-1"]
	if restored.IsDeleted || restored.DeletedAt != nil {
		t.Fatalf("expected deleted identity to be restored, got %+v", restored)
	}
	if restored.Name != "Restored" || restored.Provider != "claude-code" || restored.TotalRequests != 7 {
		t.Fatalf("expected metadata update with stats preserved, got %+v", restored)
	}
}

func TestUsageIdentityReplaceForProviderTypesMarksOnlyScopedProviderTypesDeleted(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)

	seed := []models.UsageIdentity{
		{Name: "OpenAI Keep", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "openai-keep", Type: "openai", Provider: "OpenAI", TotalRequests: 3},
		{Name: "OpenAI Stale", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "openai-stale", Type: "openai", Provider: "OpenAI"},
		{Name: "Gemini Untouched", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "gemini-untouched", Type: "gemini", Provider: "Gemini"},
		{Name: "Auth Untouched", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-untouched", Type: "account", Provider: "claude"},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}

	err := ReplaceUsageIdentitiesForProviderTypes(ctx, db, []models.UsageIdentity{
		{Name: "OpenAI Updated", AuthTypeName: "apikey", Identity: "openai-keep", Type: "openai", Provider: "OpenAI"},
		{Name: "Anthropic New", AuthTypeName: "apikey", Identity: "anthropic-new", Type: "anthropic", Provider: "Anthropic"},
	}, []string{"openai", "anthropic"}, now)
	if err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForProviderTypes returned error: %v", err)
	}

	rows, err := ListUsageIdentities(ctx, db)
	if err != nil {
		t.Fatalf("ListUsageIdentities returned error: %v", err)
	}
	byIdentity := usageIdentitiesByIdentity(rows)

	openAIKeep := byIdentity["openai-keep"]
	if openAIKeep.IsDeleted || openAIKeep.Name != "OpenAI Updated" || openAIKeep.TotalRequests != 3 {
		t.Fatalf("expected scoped provider identity updated with stats preserved, got %+v", openAIKeep)
	}

	openAIStale := byIdentity["openai-stale"]
	if !openAIStale.IsDeleted || openAIStale.DeletedAt == nil || !openAIStale.DeletedAt.Equal(now) {
		t.Fatalf("expected missing scoped provider identity to be deleted, got %+v", openAIStale)
	}

	gemini := byIdentity["gemini-untouched"]
	if gemini.IsDeleted || gemini.DeletedAt != nil {
		t.Fatalf("expected unmentioned provider type untouched, got %+v", gemini)
	}

	auth := byIdentity["auth-untouched"]
	if auth.IsDeleted || auth.DeletedAt != nil {
		t.Fatalf("expected auth identity untouched by provider replacement, got %+v", auth)
	}

	anthropic := byIdentity["anthropic-new"]
	if anthropic.ID == 0 || anthropic.IsDeleted || anthropic.AuthType != models.UsageIdentityAuthTypeAIProvider {
		t.Fatalf("expected new provider identity active, got %+v", anthropic)
	}
}

func TestUsageIdentityReplaceForProviderTypesWithEmptyProviderTypesDoesNotDeleteExistingRows(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	deletedAt := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	now := deletedAt.Add(24 * time.Hour)

	seed := []models.UsageIdentity{
		{Name: "OpenAI Active", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "openai-active", Type: "openai", Provider: "OpenAI"},
		{Name: "Gemini Active", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "gemini-active", Type: "gemini", Provider: "Gemini"},
		{Name: "Deleted Provider", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "provider-restore", Type: "anthropic", Provider: "Anthropic", TotalRequests: 9, IsDeleted: true, DeletedAt: &deletedAt},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}

	err := ReplaceUsageIdentitiesForProviderTypes(ctx, db, []models.UsageIdentity{
		{Name: "Restored Provider", AuthTypeName: "apikey", Identity: "provider-restore", Type: "anthropic", Provider: "Anthropic Updated"},
	}, []string{"", "  ", "\t"}, now)
	if err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForProviderTypes returned error: %v", err)
	}

	rows, err := ListUsageIdentities(ctx, db)
	if err != nil {
		t.Fatalf("ListUsageIdentities returned error: %v", err)
	}
	byIdentity := usageIdentitiesByIdentity(rows)

	for _, identity := range []string{"openai-active", "gemini-active"} {
		row := byIdentity[identity]
		if row.IsDeleted || row.DeletedAt != nil {
			t.Fatalf("expected existing provider identity %s untouched, got %+v", identity, row)
		}
	}

	restored := byIdentity["provider-restore"]
	if restored.IsDeleted || restored.DeletedAt != nil {
		t.Fatalf("expected incoming provider identity restored, got %+v", restored)
	}
	if restored.Name != "Restored Provider" || restored.Provider != "Anthropic Updated" || restored.AuthTypeName != "apikey" || restored.TotalRequests != 9 {
		t.Fatalf("expected incoming provider identity updated with stats preserved, got %+v", restored)
	}
}

func TestUsageIdentityListOrdersByAuthTypeNameIDAndIncludesDeletedRows(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	deletedAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)

	seed := []models.UsageIdentity{
		{Name: "Zulu", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "provider-zulu", Type: "openai", Provider: "OpenAI"},
		{Name: "Beta", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-beta-1", Type: "account", Provider: "claude"},
		{Name: "Alpha", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-alpha", Type: "account", Provider: "claude", IsDeleted: true, DeletedAt: &deletedAt},
		{Name: "Beta", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-beta-2", Type: "account", Provider: "claude"},
		{Name: "Alpha", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "provider-alpha", Type: "gemini", Provider: "Gemini", IsDeleted: true, DeletedAt: &deletedAt},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}

	rows, err := ListUsageIdentities(ctx, db)
	if err != nil {
		t.Fatalf("ListUsageIdentities returned error: %v", err)
	}

	got := make([]string, 0, len(rows))
	for _, row := range rows {
		deleted := "active"
		if row.IsDeleted {
			deleted = "deleted"
		}
		got = append(got, row.Identity+":"+deleted)
	}

	want := []string{
		"auth-alpha:deleted",
		"auth-beta-1:active",
		"auth-beta-2:active",
		"provider-alpha:deleted",
		"provider-zulu:active",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d identities, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected identities ordered by auth_type asc, name asc, id asc including deleted rows\nwant: %v\n got: %v", want, got)
		}
	}
}

func TestUsageIdentityAggregateStatsForAuthFileUsesOAuthAuthIndex(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	first := now.Add(-3 * time.Hour)
	last := now.Add(-time.Hour)

	identity := models.UsageIdentity{
		Name:         "Auth Account",
		AuthType:     models.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     "auth-1",
		Type:         "account",
		Provider:     "claude",
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "auth-1", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-1", Source: "wrong-source", RequestID: "r1", Timestamp: last, Failed: false, InputTokens: 10, OutputTokens: 20, ReasoningTokens: 3, CachedTokens: 4, TotalTokens: 37},
		{EventKey: "auth-2", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-1", Source: "wrong-source", RequestID: "r2", Timestamp: first, Failed: true, InputTokens: 5, OutputTokens: 6, ReasoningTokens: 7, CachedTokens: 8, TotalTokens: 26},
		{EventKey: "auth-ignore-auth-type", APIGroupKey: "g1", AuthType: "apikey", AuthIndex: "auth-1", Source: "auth-1", RequestID: "r3", Timestamp: now, Failed: false, InputTokens: 100, TotalTokens: 100},
		{EventKey: "auth-ignore-index", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "other-auth", Source: "auth-1", RequestID: "r4", Timestamp: now, Failed: false, InputTokens: 100, TotalTokens: 100},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	if err := AggregateUsageIdentityStats(ctx, db, now); err != nil {
		t.Fatalf("AggregateUsageIdentityStats returned error: %v", err)
	}

	var got models.UsageIdentity
	if err := db.First(&got, identity.ID).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	if got.TotalRequests != 2 || got.SuccessCount != 1 || got.FailureCount != 1 || got.InputTokens != 15 || got.OutputTokens != 26 || got.ReasoningTokens != 10 || got.CachedTokens != 12 || got.TotalTokens != 63 {
		t.Fatalf("expected aggregated auth stats, got %+v", got)
	}
	if got.FirstUsedAt == nil || !got.FirstUsedAt.Equal(first) || got.LastUsedAt == nil || !got.LastUsedAt.Equal(last) || got.StatsUpdatedAt == nil || !got.StatsUpdatedAt.Equal(now) {
		t.Fatalf("expected usage timestamps first=%s last=%s updated=%s, got %+v", first, last, now, got)
	}
	if got.LastAggregatedUsageEventID != events[1].ID {
		t.Fatalf("expected cursor %d, got %d", events[1].ID, got.LastAggregatedUsageEventID)
	}
}

func TestUsageIdentityAggregateStatsForAIProviderUsesAPIKeySourceNotProvider(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)

	identity := models.UsageIdentity{Name: "Provider", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "provider-source", Type: "openai", Provider: "Display Provider"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "provider-source-1", APIGroupKey: "g1", Provider: "wrong-provider", AuthType: "apikey", Source: "provider-source", RequestID: "r1", Timestamp: now.Add(-2 * time.Hour), Failed: false, InputTokens: 11, OutputTokens: 12, ReasoningTokens: 13, CachedTokens: 14, TotalTokens: 50},
		{EventKey: "provider-source-2", APIGroupKey: "g1", Provider: "Display Provider", AuthType: "apikey", Source: "provider-source", RequestID: "r2", Timestamp: now.Add(-time.Hour), Failed: true, InputTokens: 1, OutputTokens: 2, ReasoningTokens: 3, CachedTokens: 4, TotalTokens: 10},
		{EventKey: "provider-ignore-provider", APIGroupKey: "g1", Provider: "provider-source", AuthType: "apikey", Source: "other-source", RequestID: "r3", Timestamp: now, Failed: false, InputTokens: 100, TotalTokens: 100},
		{EventKey: "provider-ignore-auth-type", APIGroupKey: "g1", Provider: "wrong-provider", AuthType: "oauth", Source: "provider-source", RequestID: "r4", Timestamp: now, Failed: false, InputTokens: 100, TotalTokens: 100},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	if err := AggregateUsageIdentityStats(ctx, db, now); err != nil {
		t.Fatalf("AggregateUsageIdentityStats returned error: %v", err)
	}

	var got models.UsageIdentity
	if err := db.First(&got, identity.ID).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	if got.TotalRequests != 2 || got.SuccessCount != 1 || got.FailureCount != 1 || got.InputTokens != 12 || got.OutputTokens != 14 || got.ReasoningTokens != 16 || got.CachedTokens != 18 || got.TotalTokens != 60 {
		t.Fatalf("expected provider stats matched by source, got %+v", got)
	}
	if got.LastAggregatedUsageEventID != events[1].ID {
		t.Fatalf("expected cursor %d, got %d", events[1].ID, got.LastAggregatedUsageEventID)
	}
}

func TestUsageIdentityAggregateStatsSecondRunOnlyIncludesEventsAfterCursor(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	first := now.Add(-2 * time.Hour)
	last := now.Add(-time.Hour)

	identity := models.UsageIdentity{Name: "Auth Account", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-cursor", Type: "account", Provider: "claude"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	initialEvents := []models.UsageEvent{
		{EventKey: "cursor-1", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-cursor", RequestID: "r1", Timestamp: first, Failed: false, InputTokens: 10, TotalTokens: 10},
		{EventKey: "cursor-2", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-cursor", RequestID: "r2", Timestamp: last, Failed: true, InputTokens: 20, TotalTokens: 20},
	}
	if err := db.Create(&initialEvents).Error; err != nil {
		t.Fatalf("seed initial usage events: %v", err)
	}
	if err := AggregateUsageIdentityStats(ctx, db, now); err != nil {
		t.Fatalf("first AggregateUsageIdentityStats returned error: %v", err)
	}

	newEvent := models.UsageEvent{EventKey: "cursor-3", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-cursor", RequestID: "r3", Timestamp: now, Failed: false, InputTokens: 30, OutputTokens: 5, TotalTokens: 35}
	if err := db.Create(&newEvent).Error; err != nil {
		t.Fatalf("seed new usage event: %v", err)
	}
	secondNow := now.Add(time.Hour)
	if err := AggregateUsageIdentityStats(ctx, db, secondNow); err != nil {
		t.Fatalf("second AggregateUsageIdentityStats returned error: %v", err)
	}

	var got models.UsageIdentity
	if err := db.First(&got, identity.ID).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	if got.TotalRequests != 3 || got.SuccessCount != 2 || got.FailureCount != 1 || got.InputTokens != 60 || got.OutputTokens != 5 || got.TotalTokens != 65 {
		t.Fatalf("expected second aggregation to include only new event once, got %+v", got)
	}
	if got.LastAggregatedUsageEventID != newEvent.ID || got.StatsUpdatedAt == nil || !got.StatsUpdatedAt.Equal(secondNow) {
		t.Fatalf("expected cursor %d and updated timestamp %s, got %+v", newEvent.ID, secondNow, got)
	}
}

func TestUsageIdentityAggregateStatsLateTimestampWithLargerIDStillAggregates(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 15, 0, 0, 0, time.UTC)
	initialTime := now.Add(-time.Hour)
	earlierLateTime := now.Add(-24 * time.Hour)

	identity := models.UsageIdentity{Name: "Auth Late", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-late", Type: "account", Provider: "claude"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	initialEvent := models.UsageEvent{EventKey: "late-1", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-late", RequestID: "r1", Timestamp: initialTime, Failed: false, InputTokens: 10, TotalTokens: 10}
	if err := db.Create(&initialEvent).Error; err != nil {
		t.Fatalf("seed initial event: %v", err)
	}
	if err := AggregateUsageIdentityStats(ctx, db, now); err != nil {
		t.Fatalf("first AggregateUsageIdentityStats returned error: %v", err)
	}

	lateEvent := models.UsageEvent{EventKey: "late-2", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-late", RequestID: "r2", Timestamp: earlierLateTime, Failed: true, InputTokens: 20, TotalTokens: 20}
	if err := db.Create(&lateEvent).Error; err != nil {
		t.Fatalf("seed late event: %v", err)
	}
	if err := AggregateUsageIdentityStats(ctx, db, now.Add(time.Hour)); err != nil {
		t.Fatalf("second AggregateUsageIdentityStats returned error: %v", err)
	}

	var got models.UsageIdentity
	if err := db.First(&got, identity.ID).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	if got.TotalRequests != 2 || got.SuccessCount != 1 || got.FailureCount != 1 || got.InputTokens != 30 || got.TotalTokens != 30 {
		t.Fatalf("expected late timestamp event with larger DB id aggregated, got %+v", got)
	}
	if got.FirstUsedAt == nil || !got.FirstUsedAt.Equal(earlierLateTime) || got.LastUsedAt == nil || !got.LastUsedAt.Equal(initialTime) || got.LastAggregatedUsageEventID != lateEvent.ID {
		t.Fatalf("expected first_used_at to move earlier and cursor to late event id %d, got %+v", lateEvent.ID, got)
	}
}

func TestUsageIdentityAggregateStatsUsesDatabaseIDNotRequestIDOrdering(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 16, 0, 0, 0, time.UTC)

	identity := models.UsageIdentity{Name: "Auth Request", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-request", Type: "account", Provider: "claude"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	events := []models.UsageEvent{
		{EventKey: "request-1", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-request", RequestID: "z-last-lexically", Timestamp: now.Add(-2 * time.Hour), Failed: false, InputTokens: 10, TotalTokens: 10},
		{EventKey: "request-2", APIGroupKey: "g1", AuthType: "oauth", AuthIndex: "auth-request", RequestID: "a-first-lexically", Timestamp: now.Add(-time.Hour), Failed: false, InputTokens: 20, TotalTokens: 20},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}
	if err := AggregateUsageIdentityStats(ctx, db, now); err != nil {
		t.Fatalf("AggregateUsageIdentityStats returned error: %v", err)
	}

	var got models.UsageIdentity
	if err := db.First(&got, identity.ID).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	if got.TotalRequests != 2 || got.InputTokens != 30 || got.TotalTokens != 30 || got.LastAggregatedUsageEventID != events[1].ID {
		t.Fatalf("expected unordered request_id values aggregated by DB id, got %+v", got)
	}
}

func TestUsageIdentityAggregateStatsDeletedIdentityStillAggregates(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 17, 0, 0, 0, time.UTC)
	deletedAt := now.Add(-time.Hour)

	identity := models.UsageIdentity{Name: "Deleted Provider", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "deleted-source", Type: "openai", Provider: "OpenAI", IsDeleted: true, DeletedAt: &deletedAt}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed deleted usage identity: %v", err)
	}
	event := models.UsageEvent{EventKey: "deleted-1", APIGroupKey: "g1", AuthType: "apikey", Source: "deleted-source", RequestID: "r1", Timestamp: now, Failed: false, InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	if err := AggregateUsageIdentityStats(ctx, db, now); err != nil {
		t.Fatalf("AggregateUsageIdentityStats returned error: %v", err)
	}

	var got models.UsageIdentity
	if err := db.First(&got, identity.ID).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	if !got.IsDeleted || got.DeletedAt == nil || !got.DeletedAt.Equal(deletedAt) {
		t.Fatalf("expected deleted state preserved, got %+v", got)
	}
	if got.TotalRequests != 1 || got.SuccessCount != 1 || got.FailureCount != 0 || got.InputTokens != 10 || got.OutputTokens != 5 || got.TotalTokens != 15 || got.LastAggregatedUsageEventID != event.ID {
		t.Fatalf("expected deleted identity to aggregate matching event, got %+v", got)
	}
}

func usageIdentitiesByIdentity(rows []models.UsageIdentity) map[string]models.UsageIdentity {
	result := make(map[string]models.UsageIdentity, len(rows))
	for _, row := range rows {
		result[row.Identity] = row
	}
	return result
}
