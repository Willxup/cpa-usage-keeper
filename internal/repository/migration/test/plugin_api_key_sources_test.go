package test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
)

func TestPluginAPIKeySourceMigrationOnlyAddsNullableEventAndNativeKeyColumns(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	for _, table := range []string{"usage_events", "usage_events_archive", "cpa_api_keys"} {
		for _, statement := range []string{
			"CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, api_group_key TEXT, api_key TEXT, key_alias TEXT)",
			"INSERT INTO " + table + " (id, api_group_key, api_key, key_alias) VALUES (7,'legacy-group','legacy-key','keep alias')",
			"CREATE TRIGGER guard_" + table + " BEFORE UPDATE ON " + table + " BEGIN SELECT RAISE(ABORT,'legacy row rewrite'); END",
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// 原始事件不完整也不得触碰既有聚合内容或处理水位。
	for _, statement := range []string{
		"CREATE TABLE usage_overview_hourly_stats (id INTEGER PRIMARY KEY, api_group_key TEXT, request_count INTEGER)",
		"INSERT INTO usage_overview_hourly_stats VALUES (3,'old-plugin-group',987)",
		"CREATE TABLE usage_aggregation_checkpoints (name TEXT PRIMARY KEY, last_aggregated_usage_event_id INTEGER)",
		"INSERT INTO usage_aggregation_checkpoints VALUES ('overview',12345)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	var statsSchemaBefore, checkpointSchemaBefore string
	db.Raw("SELECT sql FROM sqlite_master WHERE name = 'usage_overview_hourly_stats'").Scan(&statsSchemaBefore)
	db.Raw("SELECT sql FROM sqlite_master WHERE name = 'usage_aggregation_checkpoints'").Scan(&checkpointSchemaBefore)
	runOnlyMigration(t, db, "20261028_plugin_api_key_sources")
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var row entities.UsageEvent
		if err := db.Table(table).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.ID != 7 || row.APIGroupKey != "legacy-group" || row.IsNativeKey != nil || row.AccessProvider != nil {
			t.Fatalf("legacy event changed: %+v", row)
		}
	}
	var key entities.CPAAPIKey
	if err := db.Take(&key).Error; err != nil {
		t.Fatal(err)
	}
	if key.ID != 7 || key.APIKey != "legacy-key" || key.KeyAlias != "keep alias" || key.SourceType != entities.CPAAPIKeySourceNative || key.AccessProvider != "" {
		t.Fatalf("legacy key changed: %+v", key)
	}
	// 删除版本记录后幂等重跑，已有来源字段和值也不能被重置。
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "20261028_plugin_api_key_sources").Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatal(err)
	}
	var statsSchemaAfter, checkpointSchemaAfter string
	db.Raw("SELECT sql FROM sqlite_master WHERE name = 'usage_overview_hourly_stats'").Scan(&statsSchemaAfter)
	db.Raw("SELECT sql FROM sqlite_master WHERE name = 'usage_aggregation_checkpoints'").Scan(&checkpointSchemaAfter)
	var stats struct {
		APIGroupKey  string
		RequestCount int64
	}
	var cursor int64
	if err := db.Table("usage_overview_hourly_stats").Take(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("usage_aggregation_checkpoints").Select("last_aggregated_usage_event_id").Scan(&cursor).Error; err != nil {
		t.Fatal(err)
	}
	if statsSchemaAfter != statsSchemaBefore || checkpointSchemaAfter != checkpointSchemaBefore || stats.APIGroupKey != "old-plugin-group" || stats.RequestCount != 987 || cursor != 12345 {
		t.Fatalf("source migration changed aggregation contract: stats=%+v cursor=%d", stats, cursor)
	}
}
