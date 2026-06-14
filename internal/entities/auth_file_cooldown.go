package entities

import "time"

// AuthFileCooldownState 表示 cooldown 记录的生命周期状态。
type AuthFileCooldownState string

const (
	AuthFileCooldownActive          AuthFileCooldownState = "active"
	AuthFileCooldownRecovered       AuthFileCooldownState = "recovered"
	AuthFileCooldownRecoveredExt    AuthFileCooldownState = "recovered_external"
	AuthFileCooldownRestoreFailed   AuthFileCooldownState = "restore_failed"
	AuthFileCooldownSkippedManual   AuthFileCooldownState = "skipped_manual"
	AuthFileCooldownMissing         AuthFileCooldownState = "missing"
	AuthFileCooldownDisableFailed   AuthFileCooldownState = "disable_failed"
)

// AuthFileCooldownSource 表示 cooldown 的来源。
type AuthFileCooldownSource string

const (
	AuthFileCooldownSourceRequestEvent AuthFileCooldownSource = "request_event"
	AuthFileCooldownSourceInspection   AuthFileCooldownSource = "quota_inspection"
)

// AuthFileCooldownReason 表示 cooldown 触发原因。
type AuthFileCooldownReason string

const (
	AuthFileCooldownReasonCodex429       AuthFileCooldownReason = "codex_usage_limit_reached"
	AuthFileCooldownReasonInspection429 AuthFileCooldownReason = "inspection_http_429"
)

// AuthFileCooldownOwner 表示 cooldown 的创建者。
type AuthFileCooldownOwner string

const (
	AuthFileCooldownOwnerUsage429       AuthFileCooldownOwner = "keeper_usage_429"
	AuthFileCooldownOwnerInspection429 AuthFileCooldownOwner = "keeper_inspection_429"
)

// AuthFileCooldown 是 Codex 429 usage_limit_reached 自动禁用/恢复的 cooldown 记录。
// Keeper 通过此表记录 ownership，确保不覆盖用户手动 disabled 的 auth file。
type AuthFileCooldown struct {
	ID int64 `gorm:"primaryKey"`

	// Provider 是 cooldown 针对的 provider，固定为 codex。
	Provider string `gorm:"column:provider;not null;index:idx_auth_file_cooldowns_provider_auth_index_owner,priority:1"`
	// AuthIndex 是 auth_index，关联 usage_events.auth_index。
	AuthIndex string `gorm:"column:auth_index;not null;index:idx_auth_file_cooldowns_auth_index"`
	// AuthFileName 是 CPA auth file 的名称。
	AuthFileName string `gorm:"column:auth_file_name;not null"`
	// AuthFilePath 是 CPA auth file 的路径，可选。
	AuthFilePath string `gorm:"column:auth_file_path"`

	// RecoverAt 是预计恢复时间，source of truth。
	RecoverAt time.Time `gorm:"serializer:storageTime;not null;index:idx_auth_file_cooldowns_state_recover_at,priority:2"`
	// Reason 是触发原因。
	Reason AuthFileCooldownReason `gorm:"column:reason;not null;default:'codex_usage_limit_reached'"`
	// Owner 标识创建者，用于区分不同来源的 cooldown。
	Owner AuthFileCooldownOwner `gorm:"column:owner;not null;default:'keeper_usage_429';index:idx_auth_file_cooldowns_provider_auth_index_owner,priority:3"`

	// State 是 cooldown 生命周期状态。
	State AuthFileCooldownState `gorm:"column:state;not null;default:'active';index:idx_auth_file_cooldowns_state_recover_at,priority:1"`

	// DisabledByKeeper 表示该 auth file 是由 Keeper 自动禁用的。
	// 只有此字段为 true 时恢复 worker 才会自动恢复。
	DisabledByKeeper bool `gorm:"column:disabled_by_keeper;not null;default:false"`
	// PreviousDisabled 记录禁用前的 disabled 状态。
	PreviousDisabled bool `gorm:"column:previous_disabled;not null;default:false"`

	// Source 表示 cooldown 的来源：request_event 或 quota_inspection。
	Source AuthFileCooldownSource `gorm:"column:source;not null;default:'request_event';index:idx_auth_file_cooldowns_state_recover_at,priority:3"`
	// LastSource 记录最后一次更新的来源。
	LastSource AuthFileCooldownSource `gorm:"column:last_source"`
	// UpstreamStatusCode 记录上游返回的 HTTP 状态码。
	UpstreamStatusCode int `gorm:"column:upstream_status_code;not null;default:0"`
	// UpstreamMessage 记录上游返回的简短错误消息。
	UpstreamMessage string `gorm:"column:upstream_message"`

	// SourceEventKey 是触发 cooldown 的 usage event key (request_id)。
	SourceEventKey string `gorm:"column:source_event_key"`
	// SourceRequestID 是触发 cooldown 的 request_id。
	SourceRequestID string `gorm:"column:source_request_id"`
	// LastError 是最近一次操作失败的错误信息。
	LastError string `gorm:"column:last_error"`
	// LastErrorBody 是最近一次原始错误 body，截断保存。
	LastErrorBody string `gorm:"column:last_error_body"`
	// RestoreAttempts 是自动恢复尝试次数。
	RestoreAttempts int `gorm:"column:restore_attempts;not null;default:0"`

	// CreatedAt 是记录创建时间。
	CreatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// UpdatedAt 是记录更新时间。
	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// DisabledAt 是实际禁用时间。
	DisabledAt *time.Time `gorm:"serializer:storageTime"`
	// RecoveredAt 是实际恢复时间。
	RecoveredAt *time.Time `gorm:"serializer:storageTime"`
}

func (AuthFileCooldown) TableName() string {
	return "auth_file_cooldowns"
}
