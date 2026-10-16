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

	"cpa-usage-keeper/internal/backup"
)

func recordRestoreFingerprint(t *testing.T, fixture restoreFixture) string {
	t.Helper()
	content, err := os.ReadFile(fixture.backupPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	fingerprint := hex.EncodeToString(digest[:])
	var value string
	if err := fixture.fault.QueryRow("SELECT baseline_json FROM pricing_migration_state WHERE id=1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	var baseline map[string]any
	if err := json.Unmarshal([]byte(value), &baseline); err != nil {
		t.Fatal(err)
	}
	baseline["backup_sha256"] = fingerprint
	encoded, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	mustRestoreExec(t, fixture.fault, "UPDATE pricing_migration_state SET baseline_json=? WHERE id=1", string(encoded))
	return fingerprint
}

func copyRestoreBackup(t *testing.T, fixture restoreFixture) string {
	t.Helper()
	content, err := os.ReadFile(fixture.backupPath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "explicit renamed backup.db")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestorePricingBackupAcceptsVerifiedCopiesAndRejectsLostLegacyIdentity(t *testing.T) {
	for _, fingerprint := range []bool{false, true} {
		for _, moved := range []bool{false, true} {
			name := "legacy"
			if fingerprint {
				name = "fingerprint"
			}
			if moved {
				name += "/moved"
			} else {
				name += "/copy"
			}
			t.Run(name, func(t *testing.T) {
				fixture := makeRestoreFixture(t, true)
				if fingerprint {
					recordRestoreFingerprint(t, fixture)
				}
				selected := copyRestoreBackup(t, fixture)
				if moved {
					if err := os.Remove(fixture.backupPath); err != nil {
						t.Fatal(err)
					}
				}
				insertRestoreInbox(t, fixture.fault, "queue_key", 6, "after-copy", "processed", "2026-09-22 10:05:00", "2026-09-22 10:05:01", "2026-09-22 10:05:02")
				output := filepath.Join(t.TempDir(), "restored.db")
				result, err := backup.RestorePricingBackup(context.Background(), selected, fixture.faultPath, output)
				if moved && !fingerprint {
					if err == nil || !strings.Contains(err.Error(), "cannot prove selected backup identity") {
						t.Fatalf("unproven legacy copy accepted: %+v %v", result, err)
					}
					if _, err := os.Stat(output); !os.IsNotExist(err) {
						t.Fatalf("failed restore published output: %v", err)
					}
					return
				}
				if err != nil || result.BackupInboxMaxID != 5 || result.ReplayedRows != 1 {
					t.Fatalf("verified copy rejected or replayed incorrectly: %+v %v", result, err)
				}
				var recorded string
				if err := fixture.fault.QueryRow("SELECT backup_path FROM pricing_migration_state WHERE id=1").Scan(&recorded); err != nil || recorded != fixture.backupPath {
					t.Fatalf("read-only restore rewrote source identity: %q %v", recorded, err)
				}
				restored := openRestoreTestDB(t, output)
				var raw, status string
				if err := restored.QueryRow("SELECT raw_message,status FROM redis_usage_inboxes WHERE id=6").Scan(&raw, &status); err != nil || raw != "after-copy" || status != "pending" {
					t.Fatalf("copy replay lost original row/reset: raw=%q status=%q error=%v", raw, status, err)
				}
			})
		}
	}
}

func TestRestorePricingBackupRejectsSameSizeAndBoundaryImpostor(t *testing.T) {
	for _, fingerprint := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy_original_hash", true: "persisted_hash"}[fingerprint], func(t *testing.T) {
			fixture := makeRestoreFixture(t, true)
			if fingerprint {
				recordRestoreFingerprint(t, fixture)
			}
			selected := copyRestoreBackup(t, fixture)
			impostor := openRestoreTestDB(t, selected)
			mustRestoreExec(t, impostor, "UPDATE redis_usage_inboxes SET raw_message='wrong--one' WHERE id=1")
			if err := impostor.Close(); err != nil {
				t.Fatal(err)
			}
			originalInfo, err := os.Stat(fixture.backupPath)
			if err != nil {
				t.Fatal(err)
			}
			selectedInfo, err := os.Stat(selected)
			if err != nil || selectedInfo.Size() != originalInfo.Size() {
				t.Fatalf("impostor size drifted: %v", err)
			}
			output := filepath.Join(t.TempDir(), "must-not-exist.db")
			if _, err := backup.RestorePricingBackup(context.Background(), selected, fixture.faultPath, output); err == nil || !strings.Contains(err.Error(), "SHA-256 differs") {
				t.Fatalf("same-size same-maxID wrong database accepted: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("impostor published output: %v", err)
			}
		})
	}
}

func TestRestorePricingBackupRejectsCompletedMigrationAndSidecarBackup(t *testing.T) {
	fixture := makeRestoreFixture(t, true)
	recordRestoreFingerprint(t, fixture)
	mustRestoreExec(t, fixture.fault, "UPDATE pricing_migration_state SET data_complete=1 WHERE id=1")
	output := filepath.Join(t.TempDir(), "must-not-exist.db")
	if _, err := backup.RestorePricingBackup(context.Background(), fixture.backupPath, fixture.faultPath, output); err == nil || !strings.Contains(err.Error(), "only supports incomplete legacy migrations") {
		t.Fatalf("completed migration accepted: %v", err)
	}
	mustRestoreExec(t, fixture.fault, "UPDATE pricing_migration_state SET data_complete=0 WHERE id=1")
	for _, suffix := range []string{"-wal", "-journal"} {
		selected := copyRestoreBackup(t, fixture)
		if err := os.WriteFile(selected+suffix, []byte("not part of recorded standalone snapshot"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := backup.RestorePricingBackup(context.Background(), selected, fixture.faultPath, output); err == nil || !strings.Contains(err.Error(), "nonempty SQLite sidecar") {
			t.Fatalf("sidecar %s accepted: %v", suffix, err)
		}
		content, err := os.ReadFile(selected + suffix)
		if err != nil || string(content) != "not part of recorded standalone snapshot" {
			t.Fatalf("rejected sidecar changed: %q %v", content, err)
		}
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("rejected restore created output: %v", err)
	}
}
