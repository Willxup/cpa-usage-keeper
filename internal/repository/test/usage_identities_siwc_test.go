package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageIdentitySIWCMetadataChangesIndependently(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)
	expiry := now.Add(10 * time.Hour)
	for _, tc := range []struct {
		name   string
		change func(*entities.UsageIdentity)
		check  func(entities.UsageIdentity) bool
	}{
		{"bind capability", func(row *entities.UsageIdentity) { row.SIWCBindSupported = new(false) }, func(row entities.UsageIdentity) bool { return row.SIWCBindSupported != nil && !*row.SIWCBindSupported }},
		{"bound", func(row *entities.UsageIdentity) { row.SIWCBound = new(false) }, func(row entities.UsageIdentity) bool { return row.SIWCBound != nil && !*row.SIWCBound }},
		{"use", func(row *entities.UsageIdentity) { row.UseSIWC = new(false) }, func(row entities.UsageIdentity) bool { return row.UseSIWC != nil && !*row.UseSIWC }},
		{"status", func(row *entities.UsageIdentity) { row.SIWCStatus = new("future") }, func(row entities.UsageIdentity) bool { return row.SIWCStatus != nil && *row.SIWCStatus == "future" }},
		{"expiry", func(row *entities.UsageIdentity) { row.SIWCExpiresAt = &expiry }, func(row entities.UsageIdentity) bool {
			return row.SIWCExpiresAt != nil && row.SIWCExpiresAt.Equal(expiry)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			row := entities.UsageIdentity{Identity: "auth", AuthTypeName: "oauth", Type: "codex"}
			sync := func(at time.Time) {
				t.Helper()
				if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{row}, entities.UsageIdentityAuthTypeCredential, at); err != nil {
					t.Fatal(err)
				}
			}
			read := func() entities.UsageIdentity {
				t.Helper()
				got, err := repository.GetActiveCredentialUsageIdentityByAuthIndex(ctx, db, "auth")
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			sync(now)
			tc.change(&row)
			sync(now.Add(time.Hour))
			if got := read(); !tc.check(got) || !got.UpdatedAt.Equal(now.Add(time.Hour)) {
				t.Fatalf("single-field update: %+v", got)
			}
			sync(now.Add(2 * time.Hour))
			if got := read(); !got.UpdatedAt.Equal(now.Add(time.Hour)) {
				t.Fatalf("unchanged field rewrote identity: %+v", got)
			}
			row = entities.UsageIdentity{Identity: "auth", AuthTypeName: "oauth", Type: "codex"}
			sync(now.Add(3 * time.Hour))
			if got := read(); got.SIWCBindSupported != nil || got.SIWCBound != nil || got.UseSIWC != nil || got.SIWCStatus != nil || got.SIWCExpiresAt != nil || !got.UpdatedAt.Equal(now.Add(3*time.Hour)) {
				t.Fatalf("single-field clear: %+v", got)
			}
		})
	}
}
