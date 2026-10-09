package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestCodexMetadataSyncKeepsNewerStoredExpiryAndAcceptsNewerCPAExpiry(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	accountID := "acct_123"
	live := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	seed := entities.UsageIdentity{Identity: "codex-auth", Type: "codex", Provider: "codex", AccountID: &accountID, ActiveUntil: &live}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, live.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	// 官方值允许早于旧值：写入路径不做 max，随后仅拒绝 CPA 的更旧 token 值。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, db, "codex-auth")
	if err != nil {
		t.Fatal(err)
	}
	shorterOfficial := live.Add(-time.Hour)
	if err := repository.UpdateCodexUsageIdentityActiveUntil(ctx, db, identity, shorterOfficial); err != nil {
		t.Fatal(err)
	}
	assertStoredExpiry(t, db, "codex-auth", shorterOfficial)

	stale := live.Add(-2 * time.Hour)
	for _, incoming := range []*time.Time{&stale, nil} {
		seed.ActiveUntil = incoming
		if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, live); err != nil {
			t.Fatal(err)
		}
		assertStoredExpiry(t, db, "codex-auth", shorterOfficial)
	}

	newer := live.Add(time.Hour)
	seed.ActiveUntil = &newer
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, live.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertStoredExpiry(t, db, "codex-auth", newer)
}

func TestCodexMetadataSyncComparesOffsetsAsInstants(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	old := time.Date(2026, 10, 1, 9, 0, 0, 0, time.FixedZone("+09", 9*3600))
	seed := entities.UsageIdentity{Identity: "codex-auth", Type: "codex", Provider: "codex", ActiveUntil: &old}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, old); err != nil {
		t.Fatal(err)
	}
	// 同一 instant 的不同 offset 不应改写原始值。
	if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", "codex-auth").Update("active_until", "2026-10-01T09:00:00+09:00").Error; err != nil {
		t.Fatal(err)
	}
	equal := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	seed.ActiveUntil = &equal
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, old.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertRawExpiry(t, db, "codex-auth", "2026-10-01T09:00:00+09:00")

	// 新值字面上更小，但真实时间晚半小时。
	later := equal.Add(30 * time.Minute)
	seed.ActiveUntil = &later
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, old.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertStoredExpiry(t, db, "codex-auth", later)
}

func TestCodexMetadataSyncAcceptsCPAExpiryWhenStoredValueIsNull(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	seed := entities.UsageIdentity{Identity: "codex-auth", Type: "codex"}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, time.Now()); err != nil {
		t.Fatal(err)
	}
	expiry := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	seed.ActiveUntil = &expiry
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, time.Now()); err != nil {
		t.Fatal(err)
	}
	assertStoredExpiry(t, db, "codex-auth", expiry)
}

func TestNonCodexMetadataSyncStillReplacesExpiry(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	newer := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	seed := entities.UsageIdentity{Identity: "claude-auth", Type: "claude", Provider: "claude", ActiveUntil: &newer}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, newer); err != nil {
		t.Fatal(err)
	}
	older := newer.Add(-time.Hour)
	seed.ActiveUntil = &older
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, newer.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertStoredExpiry(t, db, "claude-auth", older)
	seed.ActiveUntil = nil
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{seed}, entities.UsageIdentityAuthTypeAuthFile, newer.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var identity entities.UsageIdentity
	if err := db.Where("identity = ?", "claude-auth").First(&identity).Error; err != nil || identity.ActiveUntil != nil {
		t.Fatalf("non-Codex nil expiry was not applied: %+v, %v", identity, err)
	}
}

func assertStoredExpiry(t *testing.T, db *gorm.DB, authIndex string, expected time.Time) {
	t.Helper()
	var identity entities.UsageIdentity
	if err := db.Where("identity = ?", authIndex).First(&identity).Error; err != nil || identity.ActiveUntil == nil || !identity.ActiveUntil.Equal(expected) {
		t.Fatalf("%s expiry = %+v, want %s; err %v", authIndex, identity.ActiveUntil, expected, err)
	}
}

func assertRawExpiry(t *testing.T, db *gorm.DB, authIndex string, expected string) {
	t.Helper()
	var value string
	if err := db.Raw("SELECT active_until FROM usage_identities WHERE identity = ?", authIndex).Scan(&value).Error; err != nil || value != expected {
		t.Fatalf("raw %s expiry = %q, want %q; err %v", authIndex, value, expected, err)
	}
}
