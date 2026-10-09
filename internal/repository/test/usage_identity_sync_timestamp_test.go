package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

type timestampReplaceCase struct {
	name          string
	authType      entities.UsageIdentityAuthType
	authTypeName  string
	identityType  string
	providerTypes []string
}

var timestampReplaceCases = []timestampReplaceCase{
	{name: "auth_file", authType: entities.UsageIdentityAuthTypeAuthFile, authTypeName: "oauth", identityType: "codex"},
	{name: "ai_provider", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identityType: "codex", providerTypes: []string{"codex"}},
}

func TestUsageIdentitySyncTimestampContract(t *testing.T) {
	for _, testCase := range timestampReplaceCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openTestDatabase(t)
			ctx := context.Background()
			createdAt := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
			oldUpdatedAt := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
			deletedAt := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
			alreadyDeletedUpdatedAt := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
			// 不同 offset 的输入仍须在所有写入路径表示同一时刻。
			nowInput := time.Date(2026, 7, 15, 2, 30, 0, 0, time.FixedZone("source", -7*60*60))
			wantNow := timeutil.NormalizeStorageTime(nowInput)
			alias := "Local Alias"
			firstUsedAt := createdAt.Add(time.Hour)
			lastUsedAt := createdAt.Add(2 * time.Hour)
			statsUpdatedAt := createdAt.Add(3 * time.Hour)
			seed := []entities.UsageIdentity{
				{
					Name:                       "Old Refreshed",
					Alias:                      &alias,
					AuthType:                   testCase.authType,
					AuthTypeName:               testCase.authTypeName,
					Identity:                   "refresh-auth-index",
					Type:                       testCase.identityType,
					Provider:                   "Old Provider",
					TotalRequests:              11,
					SuccessCount:               8,
					FailureCount:               3,
					InputTokens:                101,
					OutputTokens:               102,
					ReasoningTokens:            103,
					CachedTokens:               104,
					CacheReadTokens:            105,
					TotalTokens:                515,
					LastAggregatedUsageEventID: 99,
					FirstUsedAt:                &firstUsedAt,
					LastUsedAt:                 &lastUsedAt,
					StatsUpdatedAt:             &statsUpdatedAt,
					CreatedAt:                  createdAt,
					UpdatedAt:                  oldUpdatedAt,
				},
				{Name: "Old Restored", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "restore-auth-index", Type: testCase.identityType, Provider: "Old Provider", IsDeleted: true, CreatedAt: createdAt, UpdatedAt: oldUpdatedAt, DeletedAt: &deletedAt},
				{Name: "Old Stale", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "stale-auth-index", Type: testCase.identityType, Provider: "Old Provider", CreatedAt: createdAt, UpdatedAt: oldUpdatedAt},
				{Name: "Already Deleted", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "already-deleted-auth-index", Type: testCase.identityType, Provider: "Old Provider", IsDeleted: true, CreatedAt: createdAt, UpdatedAt: alreadyDeletedUpdatedAt, DeletedAt: &deletedAt},
			}
			if err := db.Create(&seed).Error; err != nil {
				t.Fatalf("seed usage identities: %v", err)
			}
			incoming := []entities.UsageIdentity{
				{Name: "New Refreshed", AuthTypeName: testCase.authTypeName, Identity: "refresh-auth-index", Type: testCase.identityType, Provider: "New Provider"},
				{Name: "New Restored", AuthTypeName: testCase.authTypeName, Identity: "restore-auth-index", Type: testCase.identityType, Provider: "New Provider"},
				{Name: "Fresh", AuthTypeName: testCase.authTypeName, Identity: "fresh-auth-index", Type: testCase.identityType, Provider: "New Provider"},
			}
			if err := replaceUsageIdentityTimestampScope(ctx, db, testCase, incoming, nowInput); err != nil {
				t.Fatalf("replace %s identities: %v", testCase.name, err)
			}
			rows := loadUsageIdentityTimestampRows(t, db, testCase.authType)
			assertUsageIdentityTime(t, rows["fresh-auth-index"].CreatedAt, wantNow, "fresh created_at")
			assertUsageIdentityTime(t, rows["fresh-auth-index"].UpdatedAt, wantNow, "fresh updated_at")
			assertUsageIdentityTime(t, rows["refresh-auth-index"].CreatedAt, createdAt, "refreshed created_at")
			assertUsageIdentityTime(t, rows["refresh-auth-index"].UpdatedAt, wantNow, "refreshed updated_at")
			if rows["refresh-auth-index"].Name != "New Refreshed" || rows["refresh-auth-index"].Provider != "New Provider" {
				t.Fatalf("refreshed metadata = %+v", rows["refresh-auth-index"])
			}
			assertUsageIdentityStatsPreserved(t, rows["refresh-auth-index"], alias, firstUsedAt, lastUsedAt, statsUpdatedAt)
			assertUsageIdentityTime(t, rows["restore-auth-index"].CreatedAt, createdAt, "restored created_at")
			assertUsageIdentityTime(t, rows["restore-auth-index"].UpdatedAt, wantNow, "restored updated_at")
			if rows["restore-auth-index"].IsDeleted || rows["restore-auth-index"].DeletedAt != nil {
				t.Fatalf("restored identity = %+v", rows["restore-auth-index"])
			}
			assertUsageIdentityTimePointer(t, rows["stale-auth-index"].DeletedAt, wantNow, "stale deleted_at")
			assertUsageIdentityTime(t, rows["stale-auth-index"].UpdatedAt, wantNow, "stale updated_at")
			if !rows["stale-auth-index"].IsDeleted {
				t.Fatalf("stale identity remained active: %+v", rows["stale-auth-index"])
			}
			assertUsageIdentityTimePointer(t, rows["already-deleted-auth-index"].DeletedAt, deletedAt, "already-deleted deleted_at")
			assertUsageIdentityTime(t, rows["already-deleted-auth-index"].UpdatedAt, alreadyDeletedUpdatedAt, "already-deleted updated_at")
		})
	}
}

func TestUsageIdentitySyncTimestampRejectsZeroNow(t *testing.T) {
	for _, testCase := range timestampReplaceCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openTestDatabase(t)
			ctx := context.Background()
			originalUpdatedAt := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
			existing := entities.UsageIdentity{Name: "Before", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "existing-auth-index", Type: testCase.identityType, Provider: "Before", CreatedAt: originalUpdatedAt, UpdatedAt: originalUpdatedAt}
			if err := db.Create(&existing).Error; err != nil {
				t.Fatalf("seed zero-time identity: %v", err)
			}
			incoming := []entities.UsageIdentity{{Name: "After", AuthTypeName: testCase.authTypeName, Identity: "existing-auth-index", Type: testCase.identityType, Provider: "After"}, {Name: "New", AuthTypeName: testCase.authTypeName, Identity: "new-auth-index", Type: testCase.identityType, Provider: "After"}}
			err := replaceUsageIdentityTimestampScope(ctx, db, testCase, incoming, time.Time{})
			if err == nil || !strings.Contains(err.Error(), "sync time is zero") {
				t.Fatalf("zero now error = %v", err)
			}
			rows := loadUsageIdentityTimestampRows(t, db, testCase.authType)
			if len(rows) != 1 {
				t.Fatalf("rows after zero now = %+v", rows)
			}
			if rows["existing-auth-index"].Name != "Before" || rows["existing-auth-index"].Provider != "Before" || rows["existing-auth-index"].IsDeleted {
				t.Fatalf("existing row changed after zero now: %+v", rows["existing-auth-index"])
			}
			assertUsageIdentityTime(t, rows["existing-auth-index"].UpdatedAt, originalUpdatedAt, "zero-now existing updated_at")
		})
	}
}

func replaceUsageIdentityTimestampScope(ctx context.Context, db *gorm.DB, testCase timestampReplaceCase, identities []entities.UsageIdentity, now time.Time) error {
	if testCase.authType == entities.UsageIdentityAuthTypeAuthFile {
		return repository.ReplaceUsageIdentitiesForAuthType(ctx, db, identities, testCase.authType, now)
	}
	return repository.ReplaceUsageIdentitiesForProviderTypes(ctx, db, identities, testCase.providerTypes, now)
}

func loadUsageIdentityTimestampRows(t *testing.T, db *gorm.DB, authType entities.UsageIdentityAuthType) map[string]entities.UsageIdentity {
	t.Helper()
	var rows []entities.UsageIdentity
	if err := db.Where("auth_type = ?", authType).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load usage identity timestamp rows: %v", err)
	}
	byIdentity := make(map[string]entities.UsageIdentity, len(rows))
	for _, row := range rows {
		byIdentity[row.Identity] = row
	}
	return byIdentity
}

func assertUsageIdentityTime(t *testing.T, got time.Time, want time.Time, field string) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s = %s, want %s", field, got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func assertUsageIdentityTimePointer(t *testing.T, got *time.Time, want time.Time, field string) {
	t.Helper()
	if got == nil || !got.Equal(want) {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
}

func assertUsageIdentityStatsPreserved(t *testing.T, row entities.UsageIdentity, alias string, firstUsedAt time.Time, lastUsedAt time.Time, statsUpdatedAt time.Time) {
	t.Helper()
	if row.Alias == nil || *row.Alias != alias {
		t.Fatalf("refreshed alias = %+v, want %q", row.Alias, alias)
	}
	if row.TotalRequests != 11 || row.SuccessCount != 8 || row.FailureCount != 3 || row.InputTokens != 101 || row.OutputTokens != 102 || row.ReasoningTokens != 103 || row.CachedTokens != 104 || row.CacheReadTokens != 105 || row.TotalTokens != 515 || row.LastAggregatedUsageEventID != 99 {
		t.Fatalf("refreshed stats changed: %+v", row)
	}
	assertUsageIdentityTimePointer(t, row.FirstUsedAt, firstUsedAt, "refreshed first_used_at")
	assertUsageIdentityTimePointer(t, row.LastUsedAt, lastUsedAt, "refreshed last_used_at")
	assertUsageIdentityTimePointer(t, row.StatsUpdatedAt, statsUpdatedAt, "refreshed stats_updated_at")
}
