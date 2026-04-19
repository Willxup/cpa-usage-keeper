package repository

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"gorm.io/gorm"
)

func TestReplaceProviderMetadataUpsertsSoftDeletesAndRestoresRows(t *testing.T) {
	db := openProviderMetadataTestDatabase(t)
	if err := ReplaceProviderMetadata(db, []ProviderMetadataInput{{
		LookupKey:    "sk-a",
		ProviderType: "openai",
		DisplayName:  "Provider A",
		ProviderKey:  "openai:Provider A",
		MatchKind:    "api_key",
	}, {
		LookupKey:    "prefix-b",
		ProviderType: "claude",
		DisplayName:  "Provider B",
		ProviderKey:  "claude:Provider B",
		MatchKind:    "prefix",
	}}); err != nil {
		t.Fatalf("ReplaceProviderMetadata returned error: %v", err)
	}

	if err := ReplaceProviderMetadata(db, []ProviderMetadataInput{{
		LookupKey:    "prefix-b",
		ProviderType: "claude",
		DisplayName:  "Provider B Updated",
		ProviderKey:  "claude:Provider B Updated",
		MatchKind:    "prefix",
	}}); err != nil {
		t.Fatalf("ReplaceProviderMetadata returned error: %v", err)
	}

	items, err := ListProviderMetadata(db)
	if err != nil {
		t.Fatalf("ListProviderMetadata returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 provider metadata row after replacement, got %d", len(items))
	}
	if items[0].LookupKey != "prefix-b" || items[0].DisplayName != "Provider B Updated" {
		t.Fatalf("unexpected provider metadata after replacement: %+v", items[0])
	}

	if err := ReplaceProviderMetadata(db, []ProviderMetadataInput{{
		LookupKey:    "sk-a",
		ProviderType: "openai",
		DisplayName:  "Provider A Restored",
		ProviderKey:  "openai:Provider A Restored",
		MatchKind:    "api_key",
	}}); err != nil {
		t.Fatalf("ReplaceProviderMetadata restore returned error: %v", err)
	}

	items, err = ListProviderMetadata(db)
	if err != nil {
		t.Fatalf("ListProviderMetadata returned error: %v", err)
	}
	if len(items) != 1 || items[0].LookupKey != "sk-a" || items[0].DisplayName != "Provider A Restored" {
		t.Fatalf("unexpected restored provider metadata: %+v", items)
	}
}

func openProviderMetadataTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "provider_metadata.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	return db
}
