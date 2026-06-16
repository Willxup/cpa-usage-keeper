package dto

// StorageCleanupResult 是仓储层每日清理的结果。
type StorageCleanupResult struct {
	RedisInbox         RedisUsageInboxCleanupResult
	UsageEventsDeleted int64
}
