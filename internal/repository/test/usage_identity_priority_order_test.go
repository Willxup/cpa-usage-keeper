package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestAuthFilePrioritySortUsesCPAFileNameAsTieBreak(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	priority := 5
	enabled := false
	laterFileName := "codex-bbbbbbbb-user@example.com-free.json"
	earlierFileName := "codex-6ac2d0d0-user@example.com-free.json"
	rows := []entities.UsageIdentity{
		{
			Identity:  "later-file-name",
			Name:      "user@example.com",
			FileName:  &laterFileName,
			AuthType:  entities.UsageIdentityAuthTypeAuthFile,
			Priority:  &priority,
			Disabled:  &enabled,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			Identity:  "earlier-file-name",
			Name:      "user@example.com",
			FileName:  &earlierFileName,
			AuthType:  entities.UsageIdentityAuthTypeAuthFile,
			Priority:  &priority,
			Disabled:  &enabled,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}

	authType := entities.UsageIdentityAuthTypeAuthFile
	activeOnly := true
	items, total, _, err := repository.ListActiveUsageIdentitiesPage(context.Background(), db, repository.ListUsageIdentitiesPageRequest{
		AuthType:   &authType,
		ActiveOnly: &activeOnly,
		Sort:       repository.UsageIdentityPageSortPriority,
		Page:       1,
		PageSize:   10,
	})
	if err != nil {
		t.Fatalf("list auth files by priority: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 auth files, got %d", total)
	}
	if got := []string{items[0].Identity, items[1].Identity}; !reflect.DeepEqual(got, []string{"earlier-file-name", "later-file-name"}) {
		t.Fatalf("expected equal-priority auth files sorted by CPA file name, got %v", got)
	}
}
