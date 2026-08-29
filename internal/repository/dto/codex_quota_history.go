package dto

import "time"

// CodexMainQuotaObservation 是 quota 采集层交给 repository 的主额度状态变化，不包含 Token 或 cost。
type CodexMainQuotaObservation struct {
	// AuthIndex 是 CPA OAuth Auth File 的稳定账号键，未来 UsageEvent 回溯必须同时限定 auth_type=oauth。
	AuthIndex string
	// WindowRole 只允许 primary/secondary，明确排除 Code Review 与 Additional。
	WindowRole string
	// WindowSeconds 保存上游原始正整数窗口秒数，并参与周期唯一身份。
	WindowSeconds int64
	// ResetAtSource 标识周期结束时刻来自上游直接返回，还是根据倒计时推算。
	ResetAtSource string
	// ResetAt 是归一化后的周期结束 instant；repository 会用它推导周期起点。
	ResetAt time.Time
	// RemainingPercent 是 Keeper 统一的 0–100 整数剩余额度，用于单调不增比较。
	RemainingPercent int
	// FirstObservedAt 是本批状态段第一份 observation instant，使用真实观察时间而非入队时间。
	FirstObservedAt time.Time
	// LastObservedAt 是本批状态段最后一份 observation instant，且不得早于 FirstObservedAt。
	LastObservedAt time.Time
	// ObservationCount 是本批实际合并的新 observation 数量，最小值为一。
	ObservationCount int64
	// Authoritative 仅在运行期标识专门额度接口的可信事实，允许 repository 校准同周期 reset 并纠正错误低值；不写入数据库。
	Authoritative bool
}

// CodexQuotaHistoryState 是 runner 缓存缺失时从 writer 数据库恢复的当前周期和尾段状态。
type CodexQuotaHistoryState struct {
	// Found 表示账号与窗口角色已经存在当前周期；false 时其余字段均不可作为事实使用。
	Found bool
	// WindowSeconds 是当前周期上游原始窗口秒数。
	WindowSeconds int64
	// ResetAtSource 标识当前周期结束时刻来自上游直接返回，还是根据倒计时推算。
	ResetAtSource string
	// ResetAt 是当前周期的归一化结束 instant。
	ResetAt time.Time
	// HasTail 表示当前周期已经存在至少一个整数百分比状态段。
	HasTail bool
	// TailRemainingPercent 是当前尾段已接受的最低整数剩余百分比。
	TailRemainingPercent int
	// TailLastObservedAt 是当前尾段最近 observation instant。
	TailLastObservedAt time.Time
}
