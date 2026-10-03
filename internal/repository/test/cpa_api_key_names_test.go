package test

import (
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestSharedKeyNamesPreserveConflictHistoryAndRevocation(t *testing.T) {
	db := openTestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"alpha-secret", "beta-secret"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, 1, "Existing Keeper alias"); err != nil {
		t.Fatal(err)
	}
	alpha := fmt.Sprintf("%x", sha256.Sum256([]byte("alpha-secret")))
	if err := repository.SyncCPAAPIKeyNames(db, map[string]string{alpha: "Shared Team"}); err != nil {
		t.Fatal(err)
	}
	var row entities.CPAAPIKey
	db.First(&row, 1)
	if row.KeyAlias != "Shared Team" {
		t.Fatal("shared name not synchronized")
	}
	var backup entities.AppSetting
	db.First(&backup, "setting_key = ?", "api_key_names.previous_alias.1")
	if backup.Value == nil || *backup.Value != "Existing Keeper alias" {
		t.Fatal("conflict was not retained")
	}
	if err := repository.SyncCPAAPIKeys(db, []string{"beta-secret"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repository.SyncCPAAPIKeyNames(db, map[string]string{alpha: "Retired Team"}); err != nil {
		t.Fatal(err)
	}
	db.First(&row, 1)
	if !row.IsDeleted || row.KeyAlias != "Retired Team" {
		t.Fatal("historical name or revocation lost")
	}
	if err := repository.SyncCPAAPIKeyNames(db, nil); err != nil {
		t.Fatal(err)
	}
	db.First(&row, 1)
	if row.KeyAlias != "Retired Team" {
		t.Fatal("missing metadata cleared historical alias")
	}
	if err := repository.SyncCPAAPIKeyNames(db, map[string]string{alpha: ""}); err != nil {
		t.Fatal(err)
	}
	db.First(&row, 1)
	if row.KeyAlias != "" || !row.IsDeleted {
		t.Fatal("clear changed authentication")
	}
	db.First(&backup, "setting_key = ?", "api_key_names.previous_alias.1")
	if *backup.Value != "Existing Keeper alias" {
		t.Fatal("original conflict overwritten")
	}
}
