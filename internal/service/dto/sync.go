package dto

// SyncResult 是同步服务的结果。
type SyncResult struct {
	Status         string
	InsertedEvents int
	DedupedEvents  int
}

// RedisBatchSyncResult 是 Redis 批次同步的结果。
type RedisBatchSyncResult struct {
	Empty          bool
	Status         string
	InsertedEvents int
	DedupedEvents  int
}

// RedisInboxPullResult 是 Redis inbox 拉取结果。
type RedisInboxPullResult struct {
	Empty        bool
	Status       string
	InsertedRows int
}

// ProviderMetadataInput 是 provider metadata 拉平后的服务层输入。
type ProviderMetadataInput struct {
	LookupKey    string
	Prefix       string
	ProviderType string
	DisplayName  string
	AuthIndex    string
	BaseURL      string
	Priority     *int
	Disabled     *bool
	Note         *string
}
