package entities

import "time"

// ErrorEvent 保存 Keeper 在线期间从 CPA errors 订阅收到的一次凭证错误。
//
// 该表是最终业务数据，不是 Inbox：写入后不会再经过 processed/status 二次消费。
// CPA 当前没有 error event ID、request ID 或 auth type，因此既不做内容去重，也不关联具体 UsageEvent。
type ErrorEvent struct {
	// ID 是 Keeper 本地自增主键，同时作为相同 timestamp 下的稳定分页次序。
	ID int64 `gorm:"primaryKey;index:idx_error_events_auth_index_timestamp_id,sort:desc,priority:3"`
	// Timestamp 是 CPA 生成 Error Event 时写入的错误发生时间。
	Timestamp time.Time `gorm:"serializer:storageTime;index:idx_error_events_auth_index_timestamp_id,sort:desc,priority:2"`
	// ReceivedAt 是 Keeper 收到订阅消息的本地时间，只用于区分上游发生时间和本地接收时间。
	ReceivedAt time.Time `gorm:"serializer:storageTime"`

	// Provider 原样保存 CPA result.provider；它是展示维度，不作为 Credential 关联键。
	Provider string
	// Model 原样保存 CPA result.model；空字符串表示 CPA 没有提供模型。
	Model string
	// AuthID 原样保存 CPA 运行时 auth_id，仅供内部排障，禁止通过详情 API 输出。
	AuthID string
	// AuthIndex 保存 CPA 稳定 auth_index，并与 usage_identities.identity 关联详情页数据。
	AuthIndex string `gorm:"index:idx_error_events_auth_index_timestamp_id,priority:1"`
	// StatusCode 保存 CPA Error 的 HTTPStatus；CPA 无明确状态时当前契约写入 500。
	StatusCode int
	// Body 保存 CPA Error.Message 原文；数据库不改写，API 展示摘要会精确删除当前 Identity 的真实 API Key。
	Body string
	// Code 保存 CPA Error.Code；空字符串表示上游没有结构化错误码。
	Code string
	// Retryable 保存 CPA Error.Retryable，表示错误发生时上游是否认为可以重试。
	Retryable bool

	// AuthStatus 保存错误发生时凭证级 auth_status.status，不代表 Keeper 当前 Credential Health。
	AuthStatus string
	// AuthStatusMessage 保存错误发生时凭证级状态说明，仅供数据库排障，当前详情 API 不输出。
	AuthStatusMessage string
	// AuthDisabled 保存错误发生时 CPA 凭证是否被显式禁用。
	AuthDisabled bool
	// AuthUnavailable 保存错误发生时 CPA 凭证是否暂时不可用。
	AuthUnavailable bool
	// AuthNextRetryAfter 保存凭证级下一次可重试时间；nil 表示 CPA 未提供。
	AuthNextRetryAfter *time.Time `gorm:"serializer:storageTime"`

	// AuthQuotaExceeded 非 nil 表示 CPA payload 含凭证级 quota 对象，其值是 exceeded。
	AuthQuotaExceeded *bool
	// AuthQuotaReason 在凭证级 quota 对象存在时保存 reason；指针用于保留对象存在语义。
	AuthQuotaReason *string
	// AuthQuotaNextRecoverAt 保存凭证级 quota 预计恢复时间；nil 表示对象缺失或未提供时间。
	AuthQuotaNextRecoverAt *time.Time `gorm:"serializer:storageTime"`
	// AuthQuotaBackoffLevel 在凭证级 quota 对象存在时保存 backoff_level，包括零值。
	AuthQuotaBackoffLevel *int

	// AuthModelName 非 nil 表示 CPA payload 含 auth_status.model 对象，其值是该模型快照名称。
	AuthModelName *string
	// AuthModelStatus 在模型快照存在时保存模型级 status，包括空字符串。
	AuthModelStatus *string
	// AuthModelStatusMessage 在模型快照存在时保存状态说明，仅供数据库排障，当前详情 API 不输出。
	AuthModelStatusMessage *string
	// AuthModelUnavailable 在模型快照存在时保存模型是否暂时不可用。
	AuthModelUnavailable *bool
	// AuthModelNextRetryAfter 保存模型级下一次可重试时间；nil 表示模型对象缺失或未提供时间。
	AuthModelNextRetryAfter *time.Time `gorm:"serializer:storageTime"`

	// AuthModelQuotaExceeded 非 nil 表示模型快照含 quota 对象，其值是 exceeded。
	AuthModelQuotaExceeded *bool
	// AuthModelQuotaReason 在模型级 quota 对象存在时保存 reason；当前详情 API 不输出完整 quota 快照。
	AuthModelQuotaReason *string
	// AuthModelQuotaNextRecoverAt 保存模型级 quota 预计恢复时间；nil 表示对象缺失或未提供时间。
	AuthModelQuotaNextRecoverAt *time.Time `gorm:"serializer:storageTime"`
	// AuthModelQuotaBackoffLevel 在模型级 quota 对象存在时保存 backoff_level，包括零值。
	AuthModelQuotaBackoffLevel *int
}
