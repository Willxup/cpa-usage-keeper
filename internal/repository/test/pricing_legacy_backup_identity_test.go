package test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestPricingLegacyBackupRelocationResumesThroughM6(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	ctx := context.Background()
	baseline, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var original entities.PricingMigrationState
	if err := fixture.reader.Take(&original).Error; err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(*original.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	if baseline.BackupSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("M1 fingerprint not persisted: %+v", baseline)
	}
	newDir := filepath.Join(t.TempDir(), "new-volume-backups")
	if err := os.Rename(fixture.backupDir, newDir); err != nil {
		t.Fatal(err)
	}
	closePublishedPricingPools(fixture.reader, fixture.writer)
	writer, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: fixture.path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(reader, writer) })
	resumed, err := repository.MigrateLegacyPricingEvents(ctx, writer, reader, newDir, time.Now())
	if err != nil || resumed.BackupSHA256 != baseline.BackupSHA256 {
		t.Fatalf("relocated M1 did not resume: %+v %v", resumed, err)
	}
	var relocated entities.PricingMigrationState
	if err := reader.Take(&relocated).Error; err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(newDir, filepath.Base(filepath.Dir(*original.BackupPath)), filepath.Base(*original.BackupPath))
	if relocated.BackupPath == nil || *relocated.BackupPath != want {
		t.Fatalf("verified relocation not recorded: %+v", relocated)
	}
	if err := repository.CompleteLegacyPricingData(ctx, writer, reader, resumed); err != nil {
		t.Fatalf("M6 did not read verified relocated backup: %v", err)
	}
	after, err := os.ReadFile(want)
	if err != nil || string(after) != string(content) {
		t.Fatalf("relocation changed M1 snapshot: %v", err)
	}
}

func TestPricingLegacyBackupIdentityRejectsWrongOriginalOrRelocation(t *testing.T) {
	for _, originalExists := range []bool{false, true} {
		t.Run(map[bool]string{false: "wrong_relocation", true: "wrong_original_has_no_fallback"}[originalExists], func(t *testing.T) {
			db, reader, backupDir := openLegacyProtectionFixture(t)
			if _, err := repository.ProtectPricingLegacyMigration(context.Background(), db, reader, backupDir, time.Now()); err != nil {
				t.Fatal(err)
			}
			var original entities.PricingMigrationState
			if err := reader.Take(&original).Error; err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(*original.BackupPath)
			if err != nil {
				t.Fatal(err)
			}
			newDir := filepath.Join(t.TempDir(), "new-backups")
			candidate := filepath.Join(newDir, filepath.Base(filepath.Dir(*original.BackupPath)), filepath.Base(*original.BackupPath))
			if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(candidate, content, 0o600); err != nil {
				t.Fatal(err)
			}
			content[len(content)-1] ^= 1
			wrongPath := candidate
			if originalExists {
				wrongPath = *original.BackupPath
			} else if err := os.Remove(*original.BackupPath); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(wrongPath, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.ProtectPricingLegacyMigration(context.Background(), db, reader, newDir, time.Now()); err == nil || !strings.Contains(err.Error(), "SHA-256 differs") {
				t.Fatalf("wrong backup accepted: %v", err)
			}
			var after entities.PricingMigrationState
			if err := reader.Take(&after).Error; err != nil || *after.BackupPath != *original.BackupPath || *after.BaselineJSON != *original.BaselineJSON {
				t.Fatalf("failed identity check rewrote state: %+v %v", after, err)
			}
		})
	}
}

func TestPricingLegacyBackupAddsFingerprintOnlyFromVerifiedOriginal(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade_old_state", true: "lost_old_state_identity"}[missing], func(t *testing.T) {
			db, reader, backupDir := openLegacyProtectionFixture(t)
			if _, err := repository.ProtectPricingLegacyMigration(context.Background(), db, reader, backupDir, time.Now()); err != nil {
				t.Fatal(err)
			}
			var original entities.PricingMigrationState
			if err := reader.Take(&original).Error; err != nil {
				t.Fatal(err)
			}
			var old map[string]any
			if err := json.Unmarshal([]byte(*original.BaselineJSON), &old); err != nil {
				t.Fatal(err)
			}
			delete(old, "backup_sha256")
			encoded, err := json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&entities.PricingMigrationState{}).Where("id=1").Update("baseline_json", string(encoded)).Error; err != nil {
				t.Fatal(err)
			}
			if missing {
				if err := os.Remove(*original.BackupPath); err != nil {
					t.Fatal(err)
				}
			}
			baseline, err := repository.ProtectPricingLegacyMigration(context.Background(), db, reader, backupDir, time.Now())
			if missing {
				if err == nil || !strings.Contains(err.Error(), "recorded SHA-256") {
					t.Fatalf("old state claimed missing backup: %+v %v", baseline, err)
				}
				return
			}
			if err != nil || len(baseline.BackupSHA256) != 64 {
				t.Fatalf("old verified original did not gain fingerprint: %+v %v", baseline, err)
			}
			var updated entities.PricingMigrationState
			if err := reader.Take(&updated).Error; err != nil || !strings.Contains(*updated.BaselineJSON, baseline.BackupSHA256) {
				t.Fatalf("fingerprint not persisted: %+v %v", updated, err)
			}
		})
	}
}
