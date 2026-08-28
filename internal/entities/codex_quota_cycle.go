package entities

import "time"

// CodexQuotaWindowRole 标识 Codex 官方主额度中的 Primary 或 Secondary 窗口位置。
type CodexQuotaWindowRole string

const (
	// CodexQuotaWindowRolePrimary 表示官方 rate_limit.primary_window 主窗口。
	CodexQuotaWindowRolePrimary CodexQuotaWindowRole = "primary"
	// CodexQuotaWindowRoleSecondary 表示官方 rate_limit.secondary_window 次窗口。
	CodexQuotaWindowRoleSecondary CodexQuotaWindowRole = "secondary"
)

// CodexQuotaWindowKind 是 Keeper 对已知窗口秒数的展示友好分类，不参与周期唯一身份。
type CodexQuotaWindowKind string

const (
	// CodexQuotaWindowKindFiveHour 表示当前已知的五小时主额度窗口。
	CodexQuotaWindowKindFiveHour CodexQuotaWindowKind = "five_hour"
	// CodexQuotaWindowKindWeekly 表示当前已知的七天主额度窗口。
	CodexQuotaWindowKindWeekly CodexQuotaWindowKind = "weekly"
	// CodexQuotaWindowKindMonthly 表示当前已知的三十天或平均月主额度窗口。
	CodexQuotaWindowKindMonthly CodexQuotaWindowKind = "monthly"
)

// CodexQuotaResetAtSource 标识周期 reset_at 是官方绝对值还是根据相对倒计时推导。
type CodexQuotaResetAtSource string

const (
	// CodexQuotaResetAtSourceAbsolute 表示上游明确提供了绝对 Unix reset_at。
	CodexQuotaResetAtSourceAbsolute CodexQuotaResetAtSource = "absolute"
	// CodexQuotaResetAtSourceRelative 表示 reset_at 由观察时间加 reset_after_seconds 归一化得到。
	CodexQuotaResetAtSourceRelative CodexQuotaResetAtSource = "relative"
)

// CodexQuotaCycle 保存一个 Codex Auth File 主额度窗口的稳定周期边界，不包含 Token 或 cost。
type CodexQuotaCycle struct {
	// ID 是周期父行的 SQLite 自增主键，也是百分比状态段的外键目标。
	ID int64 `gorm:"primaryKey"`
	// AuthIndex 是 CPA Auth File 的稳定账号键，并作为未来 UsageEvent 回溯的 OAuth identity。
	AuthIndex string `gorm:"type:text;not null;uniqueIndex:uniq_codex_quota_cycles_identity,priority:1"`
	// WindowRole 区分 Primary/Secondary；它参与周期唯一身份，且只允许两个已确认值。
	WindowRole CodexQuotaWindowRole `gorm:"type:text;not null;check:chk_codex_quota_cycles_window_role,window_role IN ('primary','secondary');uniqueIndex:uniq_codex_quota_cycles_identity,priority:2"`
	// WindowKind 保存 five_hour/weekly/monthly 分类；未知正窗口必须为 NULL，且不参与唯一身份。
	WindowKind *string `gorm:"type:text;check:chk_codex_quota_cycles_window_kind,window_kind IS NULL OR window_kind IN ('five_hour','weekly','monthly')"`
	// WindowSeconds 保存上游原始窗口秒数；任何正整数都合法，并参与周期唯一身份。
	WindowSeconds int64 `gorm:"not null;check:chk_codex_quota_cycles_window_seconds,window_seconds > 0;uniqueIndex:uniq_codex_quota_cycles_identity,priority:3"`
	// ResetAtSource 标识周期结束时间的质量来源；absolute 可以在后续升级 relative 周期。
	ResetAtSource CodexQuotaResetAtSource `gorm:"type:text;not null;check:chk_codex_quota_cycles_reset_source,reset_at_source IN ('absolute','relative')"`
	// WindowStartedAt 是 reset_at 减原始窗口秒数得到的周期起点，使用 sortableTime 保证 instant 排序。
	WindowStartedAt time.Time `gorm:"serializer:sortableTime;not null;check:chk_codex_quota_cycles_window_bounds,window_started_at < reset_at"`
	// ResetAt 是周期结束 instant，使用 sortableTime 并与账号、角色、秒数共同组成唯一身份。
	ResetAt time.Time `gorm:"serializer:sortableTime;not null;uniqueIndex:uniq_codex_quota_cycles_identity,priority:4"`
	// FirstObservedAt 是该周期第一份被接受 observation 的时间，不等于周期开始时间。
	FirstObservedAt time.Time `gorm:"serializer:sortableTime;not null;check:chk_codex_quota_cycles_observed_bounds,first_observed_at <= last_observed_at"`
	// LastObservedAt 是该周期最近一份被接受 observation 的时间，只能随合法 observation 前进。
	LastObservedAt time.Time `gorm:"serializer:sortableTime;not null"`
	// CreatedAt 记录父行首次持久化时间，沿用项目 storageTime 审计语义。
	CreatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// UpdatedAt 记录父行最近一次边界升级或观察时间推进，沿用项目 storageTime 审计语义。
	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
}

// TableName 固定周期父表名，避免 GORM 命名策略变化破坏 migration 和查询合同。
func (CodexQuotaCycle) TableName() string {
	// 返回计划冻结的表名，所有 repository 和 migration 共用同一个实体来源。
	return "codex_quota_cycles"
}
