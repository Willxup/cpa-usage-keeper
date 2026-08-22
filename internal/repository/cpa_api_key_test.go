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

func TestSyncCPAAPIKeysKeepsPluginRowsActive(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)
	syncedAt := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

	// 插件 key 的 id（如 "kath"）不在 CPA 核心 api-keys 清单里，CPA 全量替换绝不能软删它。
	if err := SyncPluginAPIKeys(db, []PluginAPIKey{{ID: "kath", Name: "凯瑟琳", KeyPreview: "sk-8bc0...64426"}}, syncedAt); err != nil {
		t.Fatalf("seed plugin key: %v", err)
	}
	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, syncedAt); err != nil {
		t.Fatalf("SyncCPAAPIKeys returned error: %v", err)
	}

	var plugin entities.CPAAPIKey
	if err := db.Where("api_key = ?", "kath").First(&plugin).Error; err != nil {
		t.Fatalf("expected plugin key row: %v", err)
	}
	if plugin.IsDeleted || plugin.Source != entities.CPAAPIKeySourcePlugin {
		t.Fatalf("expected plugin row to stay active, got %+v", plugin)
	}
	var cpaRow entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-alpha123456").First(&cpaRow).Error; err != nil {
		t.Fatalf("expected cpa key row: %v", err)
	}
	if cpaRow.Source != entities.CPAAPIKeySourceCPA || cpaRow.IsDeleted {
		t.Fatalf("expected cpa row with source cpa, got %+v", cpaRow)
	}
}

func TestSyncPluginAPIKeysUpsertsAndSoftDeletes(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)
	firstSync := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	secondSync := firstSync.Add(time.Hour)

	// CPA 核心行不参与插件清单替换，用来验证两侧互不干扰。
	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, firstSync); err != nil {
		t.Fatalf("seed cpa key: %v", err)
	}
	if err := SyncPluginAPIKeys(db, []PluginAPIKey{
		{ID: "kath", Name: "凯瑟琳", KeyPreview: "sk-8bc0...64426"},
		{ID: "no-preview", Name: ""},
	}, firstSync); err != nil {
		t.Fatalf("initial plugin sync returned error: %v", err)
	}

	var kath entities.CPAAPIKey
	if err := db.Where("api_key = ?", "kath").First(&kath).Error; err != nil {
		t.Fatalf("expected plugin key row: %v", err)
	}
	if kath.Source != entities.CPAAPIKeySourcePlugin || kath.DisplayKey != "sk-8bc0...64426" || kath.KeyAlias != "凯瑟琳" || kath.IsDeleted {
		t.Fatalf("unexpected plugin row after sync: %+v", kath)
	}
	// key_preview 缺失时 display_key 回退到插件 id。
	var noPreview entities.CPAAPIKey
	if err := db.Where("api_key = ?", "no-preview").First(&noPreview).Error; err != nil {
		t.Fatalf("expected no-preview plugin row: %v", err)
	}
	if noPreview.DisplayKey != "no-preview" || noPreview.KeyAlias != "" {
		t.Fatalf("expected id fallback display key, got %+v", noPreview)
	}

	// 用户手改别名后，插件 name 不能覆盖；清单里消失的行才软删。
	if err := UpdateCPAAPIKeyAlias(db, kath.ID, "手工别名"); err != nil {
		t.Fatalf("UpdateCPAAPIKeyAlias returned error: %v", err)
	}
	if err := SyncPluginAPIKeys(db, []PluginAPIKey{{ID: "kath", Name: "凯瑟琳·改", KeyPreview: "sk-8bc0...64426"}}, secondSync); err != nil {
		t.Fatalf("second plugin sync returned error: %v", err)
	}

	if err := db.Where("api_key = ?", "kath").First(&kath).Error; err != nil {
		t.Fatalf("reload plugin key: %v", err)
	}
	if kath.KeyAlias != "手工别名" || kath.IsDeleted {
		t.Fatalf("expected manual alias to be preserved, got %+v", kath)
	}
	if err := db.Where("api_key = ?", "no-preview").First(&noPreview).Error; err != nil {
		t.Fatalf("expected soft-deleted plugin row to remain: %v", err)
	}
	if !noPreview.IsDeleted {
		t.Fatalf("expected missing plugin key to be marked deleted: %+v", noPreview)
	}
	var cpaRow entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-alpha123456").First(&cpaRow).Error; err != nil {
		t.Fatalf("expected cpa key row: %v", err)
	}
	if cpaRow.IsDeleted {
		t.Fatalf("expected plugin sync to leave cpa row active: %+v", cpaRow)
	}

	// 清单里重新出现的 plugin 行要恢复为 active，且空别名可以被插件 name 填充。
	if err := SyncPluginAPIKeys(db, []PluginAPIKey{{ID: "no-preview", Name: "新名字"}}, secondSync.Add(time.Hour)); err != nil {
		t.Fatalf("restore plugin sync returned error: %v", err)
	}
	if err := db.Where("api_key = ?", "no-preview").First(&noPreview).Error; err != nil {
		t.Fatalf("reload restored plugin row: %v", err)
	}
	if noPreview.IsDeleted || noPreview.KeyAlias != "新名字" {
		t.Fatalf("expected restored plugin row with filled alias, got %+v", noPreview)
	}
}

func TestFindActiveCPAAPIKeyByValueRejectsPluginKeys(t *testing.T) {
	db := openCPAAPIKeyTestDatabase(t)
	syncedAt := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

	if err := SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, syncedAt); err != nil {
		t.Fatalf("seed cpa key: %v", err)
	}
	if err := SyncPluginAPIKeys(db, []PluginAPIKey{{ID: "kath", Name: "凯瑟琳"}}, syncedAt); err != nil {
		t.Fatalf("seed plugin key: %v", err)
	}

	// CPA 核心 key 仍能按原值匹配。
	row, err := FindActiveCPAAPIKeyByValue(db, "sk-alpha123456")
	if err != nil || row.APIKey != "sk-alpha123456" {
		t.Fatalf("expected cpa key lookup to succeed, got %+v err=%v", row, err)
	}
	// 插件 key 的短 id 可猜测，绝不能作为登录凭据匹配。
	if _, err := FindActiveCPAAPIKeyByValue(db, "kath"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected plugin key lookup to be rejected, got %v", err)
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
