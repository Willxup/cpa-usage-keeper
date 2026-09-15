package repository

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func TestSyncCPAAPIKeysCreatesRowsWithDisplayKeyAndEmptyAlias(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)
	syncedAt := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)

	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, syncedAt); err != nil {
		t.Fatalf("SyncCPAAPIKeys returned error: %v", err)
	}

	var row entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-alpha123456").First(&row).Error; err != nil {
		t.Fatalf("expected synced key row: %v", err)
	}
	if row.DisplayKey != "sk-*********123456" || row.KeyAlias != "" || row.IsDeleted {
		t.Fatalf("unexpected row after sync: %+v", row)
	}
	if row.LastSyncedAt == nil || !row.LastSyncedAt.Equal(syncedAt) {
		t.Fatalf("unexpected last synced at: %+v", row.LastSyncedAt)
	}
}

func TestSyncCPAAPIKeysPreservesAliasAndMarksMissingRowsDeleted(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)
	firstSync := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	secondSync := firstSync.Add(time.Hour)

	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta654321"}, firstSync); err != nil {
		t.Fatalf("initial sync returned error: %v", err)
	}
	if err := UpdateCPAAPIKeyAlias(db, 1, "Primary Key"); err != nil {
		t.Fatalf("UpdateCPAAPIKeyAlias returned error: %v", err)
	}
	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, secondSync); err != nil {
		t.Fatalf("second sync returned error: %v", err)
	}

	var active entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-alpha123456").First(&active).Error; err != nil {
		t.Fatalf("expected active key: %v", err)
	}
	if active.KeyAlias != "Primary Key" || active.IsDeleted {
		t.Fatalf("expected alias to be preserved on active row, got %+v", active)
	}

	var deleted entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-beta654321").First(&deleted).Error; err != nil {
		t.Fatalf("expected deleted key: %v", err)
	}
	if !deleted.IsDeleted {
		t.Fatalf("expected missing key to be marked deleted: %+v", deleted)
	}
}

func TestSyncCPAAPIKeysRestoresDeletedRowsAndDeduplicatesInput(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)

	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("initial sync returned error: %v", err)
	}
	if err := UpdateCPAAPIKeyAlias(db, 1, "Primary Key"); err != nil {
		t.Fatalf("UpdateCPAAPIKeyAlias returned error: %v", err)
	}
	if err := SyncCPAAPIKeys(db, nil, time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("empty sync returned error: %v", err)
	}
	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-alpha123456"}, time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("restore sync returned error: %v", err)
	}

	var rows []entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-alpha123456").Find(&rows).Error; err != nil {
		t.Fatalf("query rows returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected deduplicated row count 1, got %d", len(rows))
	}
	if rows[0].IsDeleted || rows[0].KeyAlias != "Primary Key" {
		t.Fatalf("expected restored row to preserve alias, got %+v", rows[0])
	}
}

func TestCPAAPIKeyQueriesFilterDeletedRows(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)

	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta654321"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("sync returned error: %v", err)
	}
	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("second sync returned error: %v", err)
	}

	rows, err := ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].APIKey != "sk-alpha123456" {
		t.Fatalf("unexpected active rows: %+v", rows)
	}

	_, err = FindActiveCPAAPIKeyByID(db, 2)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted id to be hidden, got %v", err)
	}

	row, err := FindActiveCPAAPIKeyByValue(db, "sk-alpha123456")
	if err != nil || row.ID != 1 {
		t.Fatalf("expected active key lookup by value to return row 1, got %+v err=%v", row, err)
	}
	_, err = FindActiveCPAAPIKeyByValue(db, "sk-beta654321")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted value lookup to be hidden, got %v", err)
	}
}

func TestSyncCPAAPIKeysDoesNotConsumeIDsForExistingKeys(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)

	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("initial sync returned error: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 11, i, 0, 0, time.UTC)); err != nil {
			t.Fatalf("repeat sync returned error: %v", err)
		}
	}
	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta654321"}, time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("new key sync returned error: %v", err)
	}

	var row entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-beta654321").First(&row).Error; err != nil {
		t.Fatalf("expected new key row: %v", err)
	}
	if row.ID != 2 {
		t.Fatalf("expected second key id to be 2 without upsert sequence burn, got %d", row.ID)
	}
}

func TestSyncCPAAPIKeySnapshotsReconcilesOnlyIncludedSources(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)
	firstSync := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	if err := SyncCPAAPIKeySnapshots(db, []CPAAPIKeySourceSnapshot{
		{Source: entities.CPAAPIKeySourceNative, Keys: []CPAAPIKeyMetadata{{APIKey: "sk-native-a"}, {APIKey: "sk-native-b"}}},
		{Source: entities.CPAAPIKeySourceCPAKeyPolicy, Keys: []CPAAPIKeyMetadata{{APIKey: "marketplace1", DisplayKey: "cpa_mar...ce1", KeyAlias: "белослудцев"}, {APIKey: "task-dispatcher", DisplayKey: "cpa_tas...her", KeyAlias: "dispatcher"}}},
	}, firstSync); err != nil {
		t.Fatalf("initial source sync returned error: %v", err)
	}

	if err := SyncCPAAPIKeySnapshots(db, []CPAAPIKeySourceSnapshot{{Source: entities.CPAAPIKeySourceNative, Keys: []CPAAPIKeyMetadata{{APIKey: "sk-native-a"}}}}, firstSync.Add(time.Hour)); err != nil {
		t.Fatalf("native-only sync returned error: %v", err)
	}
	rows, err := ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("list active keys: %v", err)
	}
	active := make(map[string]entities.CPAAPIKey, len(rows))
	for _, row := range rows {
		active[row.APIKey] = row
	}
	if len(active) != 3 || active["sk-native-a"].Source != entities.CPAAPIKeySourceNative || active["marketplace1"].KeyAlias != "белослудцев" || active["task-dispatcher"].Source != entities.CPAAPIKeySourceCPAKeyPolicy {
		t.Fatalf("unexpected rows after native-only reconcile: %+v", active)
	}
	if _, ok := active["sk-native-b"]; ok {
		t.Fatalf("missing native key was not deleted: %+v", active)
	}

	if err := SyncCPAAPIKeySnapshots(db, []CPAAPIKeySourceSnapshot{{Source: entities.CPAAPIKeySourceCPAKeyPolicy, Keys: []CPAAPIKeyMetadata{}}}, firstSync.Add(2*time.Hour)); err != nil {
		t.Fatalf("explicit empty plugin sync returned error: %v", err)
	}
	rows, err = ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("list active keys after plugin empty: %v", err)
	}
	if len(rows) != 1 || rows[0].APIKey != "sk-native-a" || rows[0].Source != entities.CPAAPIKeySourceNative {
		t.Fatalf("explicit empty plugin snapshot changed native rows: %+v", rows)
	}
}

func TestSyncCPAAPIKeySnapshotsUpdatesPluginMetadataAndBlocksNativeLogin(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)
	if err := SyncCPAAPIKeySnapshots(db, []CPAAPIKeySourceSnapshot{{
		Source: entities.CPAAPIKeySourceCPAKeyPolicy,
		Keys:   []CPAAPIKeyMetadata{{APIKey: "marketplace1", DisplayKey: "cpa_mar...ce1", KeyAlias: "белослудцев"}},
	}}, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("plugin sync returned error: %v", err)
	}
	rows, err := ListActiveCPAAPIKeys(db)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list plugin metadata rows=%+v err=%v", rows, err)
	}
	if rows[0].APIKey != "marketplace1" || rows[0].DisplayKey != "cpa_mar...ce1" || rows[0].KeyAlias != "белослудцев" || rows[0].Source != entities.CPAAPIKeySourceCPAKeyPolicy {
		t.Fatalf("unexpected plugin metadata row: %+v", rows[0])
	}
	if _, err := FindActiveCPAAPIKeyByValue(db, "marketplace1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("plugin id must not authenticate as native key: %v", err)
	}
}

func openCPAAPIKeyTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "cpa-api-key.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}
