package test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

var metadataDiffTestTime = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func metadataDiffIdentity(authType entities.UsageIdentityAuthType, n int) entities.UsageIdentity {
	row := entities.UsageIdentity{
		Name:     fmt.Sprintf("Credential %04d", n),
		Identity: fmt.Sprintf("auth-%04d", n),
		Type:     "claude",
		Provider: "Claude",
	}
	if authType == entities.UsageIdentityAuthTypeAuthFile {
		row.AuthTypeName = "oauth"
	} else {
		row.AuthTypeName = "apikey"
	}
	return row
}

func metadataDiffRows(authType entities.UsageIdentityAuthType, count int) []entities.UsageIdentity {
	rows := make([]entities.UsageIdentity, count)
	for i := range rows {
		rows[i] = metadataDiffIdentity(authType, i)
	}
	return rows
}

func metadataDiffKeys(count int) []string {
	keys := make([]string, count)
	for i := range keys {
		keys[i] = fmt.Sprintf("synthetic-key-%04d", i)
	}
	return keys
}

func metadataDiffSync(ctx context.Context, db *gorm.DB, kind string, rows []entities.UsageIdentity, keys []string, now time.Time) error {
	switch kind {
	case "auth":
		return repository.ReplaceUsageIdentitiesForAuthType(ctx, db, rows, entities.UsageIdentityAuthTypeAuthFile, now)
	case "provider":
		return repository.ReplaceUsageIdentitiesForProviderTypes(ctx, db, rows, []string{"claude"}, now)
	default:
		return repository.SyncCPAAPIKeys(db.WithContext(ctx), keys, now)
	}
}

func metadataDiffUpdateCounter(tb testing.TB, db *gorm.DB) *atomic.Int64 {
	tb.Helper()
	var updates atomic.Int64
	name := "test:count_metadata_updates"
	if err := db.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Error == nil && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(tx.Statement.SQL.String())), "UPDATE ") {
			updates.Add(1)
		}
	}); err != nil {
		tb.Fatalf("register update counter: %v", err)
	}
	return &updates
}

func TestMetadataDiffSyncUnchangedSnapshotDoesNotUpdate(t *testing.T) {
	for _, kind := range []string{"auth", "provider", "key"} {
		t.Run(kind, func(t *testing.T) {
			db := openTestDatabase(t)
			ctx := context.Background()
			rows := metadataDiffRows(entities.UsageIdentityAuthTypeAuthFile, 3)
			if kind == "provider" {
				rows = metadataDiffRows(entities.UsageIdentityAuthTypeAIProvider, 3)
			}
			keys := metadataDiffKeys(3)
			if err := metadataDiffSync(ctx, db, kind, rows, keys, metadataDiffTestTime); err != nil {
				t.Fatalf("seed metadata: %v", err)
			}
			updates := metadataDiffUpdateCounter(t, db)
			if err := metadataDiffSync(ctx, db, kind, rows, keys, metadataDiffTestTime.Add(time.Hour)); err != nil {
				t.Fatalf("repeat metadata: %v", err)
			}
			if got := updates.Load(); got != 0 {
				t.Fatalf("unchanged metadata executed %d UPDATE statements", got)
			}
			if kind == "key" {
				var stored entities.CPAAPIKey
				if err := db.Where("api_key = ?", keys[0]).First(&stored).Error; err != nil {
					t.Fatal(err)
				}
				if stored.LastSyncedAt == nil || !stored.LastSyncedAt.Equal(metadataDiffTestTime) {
					t.Fatalf("unchanged key advanced last_synced_at: %v", stored.LastSyncedAt)
				}
			}
		})
	}
}

func TestMetadataDiffSyncUnchangedSnapshotDoesNotNeedWriter(t *testing.T) {
	for _, kind := range []string{"auth", "provider", "key"} {
		t.Run(kind, func(t *testing.T) {
			db, writer, _ := openTestDatabasePools(t, kind+".db")
			rows := metadataDiffRows(entities.UsageIdentityAuthTypeAuthFile, 2)
			if kind == "provider" {
				rows = metadataDiffRows(entities.UsageIdentityAuthTypeAIProvider, 2)
			}
			keys := metadataDiffKeys(2)
			if err := metadataDiffSync(context.Background(), db, kind, rows, keys, metadataDiffTestTime); err != nil {
				t.Fatal(err)
			}
			writerTx, err := writer.BeginTx(context.Background(), &sql.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = writerTx.Rollback() })
			if _, err := writerTx.Exec("UPDATE usage_identities SET updated_at = updated_at WHERE id = -1"); err != nil {
				t.Fatalf("hold writer transaction: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			if err := metadataDiffSync(ctx, db, kind, rows, keys, metadataDiffTestTime.Add(time.Hour)); err != nil {
				t.Fatalf("unchanged sync waited for occupied writer: %v", err)
			}
		})
	}
}

func TestMetadataDiffSyncUpdatesOnlyChangedIdentity(t *testing.T) {
	for _, kind := range []string{"auth", "provider"} {
		t.Run(kind, func(t *testing.T) {
			db := openTestDatabase(t)
			ctx := context.Background()
			authType := entities.UsageIdentityAuthTypeAuthFile
			if kind == "provider" {
				authType = entities.UsageIdentityAuthTypeAIProvider
			}
			rows := metadataDiffRows(authType, 3)
			if err := metadataDiffSync(ctx, db, kind, rows, nil, metadataDiffTestTime); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", rows[0].Identity).
				Updates(map[string]any{"alias": "Local alias", "total_requests": 11, "last_aggregated_usage_event_id": 42}).Error; err != nil {
				t.Fatal(err)
			}
			rows[0].Name = "Changed name"
			updates := metadataDiffUpdateCounter(t, db)
			var updateSQL string
			if err := db.Callback().Update().After("gorm:update").Register("test:metadata_diff_changed_fields", func(tx *gorm.DB) {
				if tx.Error == nil {
					updateSQL = tx.Statement.SQL.String()
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := metadataDiffSync(ctx, db, kind, rows, nil, metadataDiffTestTime.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if got := updates.Load(); got != 1 {
				t.Fatalf("one changed identity executed %d UPDATE statements", got)
			}
			if !strings.Contains(updateSQL, "`name`") || !strings.Contains(updateSQL, "`updated_at`") || strings.Contains(updateSQL, "`provider`") || strings.Contains(updateSQL, "`lookup_key`") || strings.Contains(updateSQL, "`disabled`") {
				t.Fatalf("metadata change wrote unrelated columns: %s", updateSQL)
			}
			var stored []entities.UsageIdentity
			if err := db.Order("identity asc").Find(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if len(stored) != 3 || stored[0].Name != "Changed name" || stored[0].Alias == nil || *stored[0].Alias != "Local alias" || stored[0].TotalRequests != 11 || stored[0].LastAggregatedUsageEventID != 42 {
				t.Fatalf("changed identity lost local fields: %+v", stored)
			}
			if !stored[0].UpdatedAt.Equal(metadataDiffTestTime.Add(time.Hour)) || !stored[1].UpdatedAt.Equal(metadataDiffTestTime) || !stored[2].UpdatedAt.Equal(metadataDiffTestTime) {
				t.Fatalf("unchanged identities advanced updated_at: %+v", stored)
			}
		})
	}
}

func TestMetadataDiffSyncDoesNotOverwriteLaterCredentialEdit(t *testing.T) {
	priority := 1
	otherPriority := 9
	zeroPriority := 0
	disabled := true
	otherDisabled := false
	for _, tc := range []struct {
		name            string
		kind            string
		changeName      bool
		wantChangedName bool
		initialPriority *int
		initialDisabled *bool
		priority        *int
		disabled        *bool
		editPriority    *int
		editDisabled    *bool
		wantPriority    *int
		wantDisabled    *bool
	}{
		{name: "priority_null_old_value", kind: "auth", priority: &priority, editPriority: &otherPriority, wantPriority: &otherPriority},
		{name: "priority_nonnull_old_value", kind: "provider", initialPriority: &zeroPriority, priority: &priority, editPriority: &otherPriority, wantPriority: &otherPriority},
		{name: "disabled_null_old_value", kind: "provider", disabled: &disabled, editDisabled: &otherDisabled, wantDisabled: &otherDisabled},
		{name: "disabled_nonnull_old_value", kind: "auth", changeName: true, initialDisabled: &otherDisabled, disabled: &disabled, editDisabled: &disabled, wantDisabled: &disabled},
		{name: "both_changed_one_conflict", kind: "auth", changeName: true, priority: &priority, disabled: &disabled, editPriority: &otherPriority, wantPriority: &otherPriority},
		{name: "both_nonnull_changed_disabled_conflict", kind: "provider", changeName: true, initialPriority: &zeroPriority, initialDisabled: &otherDisabled, priority: &priority, disabled: &disabled, editDisabled: &disabled, wantPriority: &zeroPriority, wantDisabled: &disabled},
		{name: "unrelated_name_change_still_applies", kind: "auth", changeName: true, wantChangedName: true, editPriority: &otherPriority, wantPriority: &otherPriority},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			ctx := context.Background()
			authType := entities.UsageIdentityAuthTypeAuthFile
			if tc.kind == "provider" {
				authType = entities.UsageIdentityAuthTypeAIProvider
			}
			row := metadataDiffIdentity(authType, 0)
			row.Priority = tc.initialPriority
			row.Disabled = tc.initialDisabled
			if err := metadataDiffSync(ctx, db, tc.kind, []entities.UsageIdentity{row}, nil, metadataDiffTestTime); err != nil {
				t.Fatal(err)
			}
			var seeded entities.UsageIdentity
			if err := db.Where("identity = ?", row.Identity).First(&seeded).Error; err != nil {
				t.Fatal(err)
			}
			row.Priority = tc.priority
			row.Disabled = tc.disabled
			if tc.changeName {
				row.Name = "CPA changed name"
			}
			var injected bool
			var editErr error
			// reader 查询完成后、metadata writer 开始前插入本地编辑，稳定复现旧快照覆盖。
			if err := db.Callback().Query().After("gorm:query").Register("test:credential_edit_after_metadata_read", func(tx *gorm.DB) {
				if injected || tx.Error != nil || tx.Statement.Table != "usage_identities" {
					return
				}
				if _, ok := tx.Statement.Dest.(*[]entities.UsageIdentity); !ok {
					return
				}
				injected = true
				if tc.editPriority != nil {
					editErr = repository.UpdateUsageIdentityPriority(ctx, db, authType, row.Identity, *tc.editPriority)
				} else {
					editErr = repository.UpdateUsageIdentityDisabled(ctx, db, authType, row.Identity, *tc.editDisabled)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := metadataDiffSync(ctx, db, tc.kind, []entities.UsageIdentity{row}, nil, metadataDiffTestTime.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if !injected || editErr != nil {
				t.Fatalf("local edit injection: injected=%t err=%v", injected, editErr)
			}
			var stored entities.UsageIdentity
			if err := db.Where("id = ?", seeded.ID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			wantName := seeded.Name
			if tc.wantChangedName {
				wantName = row.Name
			}
			if !optionalIntEqual(stored.Priority, tc.wantPriority) || !optionalBoolEqual(stored.Disabled, tc.wantDisabled) || stored.Name != wantName {
				t.Fatalf("metadata sync overwrote later edit or partially applied conflicted row: %+v", stored)
			}
		})
	}
}

func optionalIntEqual(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func optionalBoolEqual(a, b *bool) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func TestMetadataDiffSyncAppliesPriorityAndDisabledWithoutConflict(t *testing.T) {
	zeroPriority := 0
	oldDisabled := false
	newPriority := 5
	newDisabled := true
	for _, tc := range []struct {
		name            string
		kind            string
		initialPriority *int
		initialDisabled *bool
	}{
		{name: "null_old_values", kind: "auth"},
		{name: "nonnull_old_values", kind: "provider", initialPriority: &zeroPriority, initialDisabled: &oldDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			row := metadataDiffIdentity(entities.UsageIdentityAuthTypeAuthFile, 0)
			if tc.kind == "provider" {
				row = metadataDiffIdentity(entities.UsageIdentityAuthTypeAIProvider, 0)
			}
			row.Priority = tc.initialPriority
			row.Disabled = tc.initialDisabled
			if err := metadataDiffSync(context.Background(), db, tc.kind, []entities.UsageIdentity{row}, nil, metadataDiffTestTime); err != nil {
				t.Fatal(err)
			}
			row.Priority = &newPriority
			row.Disabled = &newDisabled
			if err := metadataDiffSync(context.Background(), db, tc.kind, []entities.UsageIdentity{row}, nil, metadataDiffTestTime.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			var stored entities.UsageIdentity
			if err := db.Where("identity = ?", row.Identity).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if !optionalIntEqual(stored.Priority, &newPriority) || !optionalBoolEqual(stored.Disabled, &newDisabled) {
				t.Fatalf("non-conflicting metadata was not applied: %+v", stored)
			}
		})
	}
}

func TestMetadataDiffSyncPreservesNullableAndDeletionSemantics(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	zero := 0
	no := false
	row := metadataDiffIdentity(entities.UsageIdentityAuthTypeAuthFile, 0)
	row.Priority = &zero
	row.Disabled = &no
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{row}, entities.UsageIdentityAuthTypeAuthFile, metadataDiffTestTime); err != nil {
		t.Fatal(err)
	}
	updates := metadataDiffUpdateCounter(t, db)
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{row}, entities.UsageIdentityAuthTypeAuthFile, metadataDiffTestTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if updates.Load() != 0 {
		t.Fatalf("equal nullable fields executed %d UPDATE statements", updates.Load())
	}
	row.Priority = nil
	row.Disabled = nil
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{row}, entities.UsageIdentityAuthTypeAuthFile, metadataDiffTestTime.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var stored entities.UsageIdentity
	if err := db.Where("identity = ?", row.Identity).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if updates.Load() != 1 || stored.Priority != nil || stored.Disabled != nil {
		t.Fatalf("nullable fields were not cleared once: updates=%d row=%+v", updates.Load(), stored)
	}

	// AuthType 入口仍按整个类型删除；已 active 的异常残留 deleted_at 也需在命中时清理。
	provider := metadataDiffIdentity(entities.UsageIdentityAuthTypeAIProvider, 2)
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{provider}, entities.UsageIdentityAuthTypeAIProvider, metadataDiffTestTime); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", provider.Identity).Update("deleted_at", metadataDiffTestTime).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{provider}, entities.UsageIdentityAuthTypeAIProvider, metadataDiffTestTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	stored = entities.UsageIdentity{}
	if err := db.Where("identity = ?", provider.Identity).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DeletedAt != nil || stored.IsDeleted {
		t.Fatalf("active identity retained deleted_at: %+v", stored)
	}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, nil, entities.UsageIdentityAuthTypeAIProvider, metadataDiffTestTime.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	stored = entities.UsageIdentity{}
	if err := db.Where("identity = ?", provider.Identity).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.IsDeleted || stored.DeletedAt == nil {
		t.Fatalf("AuthType AIProvider empty snapshot did not delete its row: %+v", stored)
	}
}

func TestMetadataDiffSyncCodexExpiryNoopKeepsUpdatedAt(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	expiry := metadataDiffTestTime.Add(24 * time.Hour)
	row := metadataDiffIdentity(entities.UsageIdentityAuthTypeAuthFile, 0)
	row.Type = "codex"
	row.ActiveUntil = &expiry
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{row}, entities.UsageIdentityAuthTypeAuthFile, metadataDiffTestTime); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", row.Identity).
		Update("active_until", expiry.In(time.FixedZone("+09", 9*3600)).Format(time.RFC3339)).Error; err != nil {
		t.Fatal(err)
	}
	var before entities.UsageIdentity
	if err := db.Where("identity = ?", row.Identity).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	updates := metadataDiffUpdateCounter(t, db)
	for _, incoming := range []*time.Time{&expiry, nil} {
		row.ActiveUntil = incoming
		if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{row}, entities.UsageIdentityAuthTypeAuthFile, metadataDiffTestTime.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	older := expiry.Add(-time.Hour)
	row.ActiveUntil = &older
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{row}, entities.UsageIdentityAuthTypeAuthFile, metadataDiffTestTime.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var stored entities.UsageIdentity
	if err := db.Where("identity = ?", row.Identity).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if updates.Load() != 0 || !stored.UpdatedAt.Equal(before.UpdatedAt) || stored.ActiveUntil == nil || !stored.ActiveUntil.Equal(expiry) {
		t.Fatalf("Codex older/equal expiry changed metadata: updates=%d row=%+v", updates.Load(), stored)
	}
}

func BenchmarkMetadataDiffSync(b *testing.B) {
	for _, kind := range []string{"auth", "provider", "key"} {
		for _, scenario := range []string{"unchanged", "one_percent"} {
			b.Run(kind+"/"+scenario, func(b *testing.B) {
				path := filepath.Join(b.TempDir(), "metadata.db")
				db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: path})
				if err != nil {
					b.Fatal(err)
				}
				pool, err := db.DB()
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = pool.Close() })
				readerPool, err := reader.DB()
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = readerPool.Close() })
				rows := metadataDiffRows(entities.UsageIdentityAuthTypeAuthFile, 1000)
				if kind == "provider" {
					rows = metadataDiffRows(entities.UsageIdentityAuthTypeAIProvider, 1000)
				}
				keys := metadataDiffKeys(1000)
				if err := metadataDiffSync(context.Background(), db, kind, rows, keys, metadataDiffTestTime); err != nil {
					b.Fatalf("seed: %v", err)
				}
				updates := metadataDiffUpdateCounter(b, db)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if scenario == "one_percent" {
						if kind == "key" {
							for j := 0; j < 10; j++ {
								keys[j] = fmt.Sprintf("synthetic-key-extra-%04d-%d", j, i%2)
							}
						} else {
							for j := 0; j < 10; j++ {
								rows[j].Name = fmt.Sprintf("Credential changed %04d-%d", j, i%2)
							}
						}
					}
					if err := metadataDiffSync(context.Background(), db, kind, rows, keys, metadataDiffTestTime.Add(time.Duration(i+1)*time.Minute)); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(updates.Load())/float64(b.N), "updates/op")
			})
		}
	}
}
