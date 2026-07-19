package dto

// SyncResult 是同步服务的结果。
type SyncResult struct {
	Status         string
	InsertedEvents int
	DedupedEvents  int
}

// RedisBatchSyncResult 是 Redis 批次同步的结果。
type RedisBatchSyncResult struct {
	// Empty 表示本轮没有从 Redis inbox 取到可处理行。
	Empty bool
	// Status 表示本轮 Redis inbox 处理的最终状态。
	Status string
	// InsertedEvents 表示本轮新写入 usage_events 的事件数量。
	InsertedEvents int
	// DedupedEvents 表示本轮被 usage_events 去重保护拦下的事件数量。
	DedupedEvents int
	// ProcessedRows 表示本轮从 Redis inbox 取出的原始行数。
	ProcessedRows int
	// BatchFull 表示本轮取到的 inbox 行数已经达到 service 批次上限。
	BatchFull bool
	// RetryPending 表示部分成功后仍有 process_failed 行等待下一轮重试，runner 必须保留轮询间隔。
	RetryPending bool
	// DiscardedRows 表示本轮已经确认进入 discarded 终态的行数，runner 不得再重复输出批次错误。
	DiscardedRows int
}

// RedisInboxPullResult 是 Redis inbox 拉取结果。
type RedisInboxPullResult struct {
	Empty        bool
	Status       string
	InsertedRows int
}
