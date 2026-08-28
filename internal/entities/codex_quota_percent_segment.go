package entities

import "time"

// CodexQuotaPercentSegment 保存一个周期内真实观察到的整数剩余百分比连续状态段。
type CodexQuotaPercentSegment struct {
	// ID 是百分比状态段的 SQLite 自增主键，用于稳定决胜同时间读取顺序。
	ID int64 `gorm:"primaryKey"`
	// CycleID 指向 codex_quota_cycles.id，并与 RemainingPercent 共同保证每周期每整数值最多一行。
	CycleID int64 `gorm:"not null;uniqueIndex:uniq_codex_quota_percent_segments_cycle_percent,priority:1"`
	// RemainingPercent 是 Keeper 统一的 0–100 整数剩余额度，不是上游 used_percent 小数。
	RemainingPercent int `gorm:"not null;check:chk_codex_quota_percent_segments_remaining,remaining_percent BETWEEN 0 AND 100;uniqueIndex:uniq_codex_quota_percent_segments_cycle_percent,priority:2"`
	// FirstRawUsedPercent 保存首次进入该整数桶时上游返回的已用小数百分比，不用于反推统一值。
	FirstRawUsedPercent float64 `gorm:"not null"`
	// LastRawUsedPercent 保存最近一次合并同整数桶时上游返回的已用小数百分比。
	LastRawUsedPercent float64 `gorm:"not null"`
	// FirstObservedAt 是该连续状态段首次被接受的 observation instant，使用 sortableTime。
	FirstObservedAt time.Time `gorm:"serializer:sortableTime;not null;check:chk_codex_quota_percent_segments_observed_bounds,first_observed_at <= last_observed_at"`
	// LastObservedAt 是该连续状态段最近一次被接受的 observation instant，使用 sortableTime。
	LastObservedAt time.Time `gorm:"serializer:sortableTime;not null"`
	// ObservationCount 累计实际合并到该状态段的新 observation 数量，初始值和最小值都是一。
	ObservationCount int64 `gorm:"not null;default:1;check:chk_codex_quota_percent_segments_count,observation_count >= 1"`
	// CreatedAt 记录状态段首次持久化时间，沿用项目 storageTime 审计语义。
	CreatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// UpdatedAt 记录状态段最近一次 observation 合并时间，沿用项目 storageTime 审计语义。
	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
	// Cycle 只声明真实外键和删除限制，不创建额外查询索引，也不作为持久化列。
	Cycle CodexQuotaCycle `gorm:"foreignKey:CycleID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}

// TableName 固定百分比子表名，避免 GORM 命名策略变化破坏 migration 和外键合同。
func (CodexQuotaPercentSegment) TableName() string {
	// 返回计划冻结的表名，repository 和 schema 检查均以此为准。
	return "codex_quota_percent_segments"
}
