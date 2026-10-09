package test

import "testing"

func TestPricingStructureAddsSpeedWithoutRewritingOldCounts(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	seedPhysicalPricingStorageBeforeCost(t, db)
	runOnlyMigration(t, db, pricingStorageStructureVersion)
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		for _, column := range []string{"speed_tps_sum", "decode_speed_tps_sum"} {
			assertPricingColumnType(t, db, table, column, "REAL")
		}
		for _, column := range []string{"speed_sample_count", "decode_speed_sample_count"} {
			assertPricingColumnType(t, db, table, column, "INTEGER")
		}
		var requests, count, decodeCount int64
		var speed, decode float64
		if err := db.Raw("SELECT request_count,speed_tps_sum,speed_sample_count,decode_speed_tps_sum,decode_speed_sample_count FROM "+table+" WHERE id=1").Row().Scan(&requests, &speed, &count, &decode, &decodeCount); err != nil {
			t.Fatal(err)
		}
		if requests != 7 || speed != 0 || count != 0 || decode != 0 || decodeCount != 0 {
			t.Fatal("structure must preserve old facts and start speed at zero before rebuild")
		}
		if err := db.Exec("UPDATE " + table + " SET speed_tps_sum=10.5,speed_sample_count=2,decode_speed_tps_sum=20.25,decode_speed_sample_count=1").Error; err != nil {
			t.Fatal(err)
		}
	}
	runOnlyMigration(t, db, pricingStorageStructureVersion)
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var count int64
		if err := db.Table(table).Where("speed_tps_sum=10.5 AND speed_sample_count=2 AND decode_speed_tps_sum=20.25 AND decode_speed_sample_count=1").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("rerun reset speed: %d %v", count, err)
		}
	}
}
