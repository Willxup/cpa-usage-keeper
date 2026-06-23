package entities

// All 返回需要 AutoMigrate 的核心数据库实体列表。
func All() []any {
	return []any{
		&UsageEvent{},
		&RedisUsageInbox{},
		&ModelPriceSetting{},
		&UsageIdentity{},
		&CPAAPIKey{},
		&UsageOverviewHourlyStat{},
		&UsageOverviewDailyStat{},
		&UsageOverviewHealthStat{},
		&UsageOverviewAggregationCheckpoint{},
		&AuthSession{},
	}
}
