package dto

import (
	"time"

	"cpa-usage-keeper/internal/pricing"
)

// PricingModelsResponse 返回可直接编辑的完整配置和已保存价格修订号。
type PricingModelsResponse struct {
	Models         []pricing.ModelPricingConfig `json:"models"`
	ConfigRevision int64                        `json:"config_revision"`
}

type PricingModelOptionsResponse struct {
	Models []string `json:"models"`
}

type SavePricingModelResponse struct {
	Model          string `json:"model"`
	ConfigRevision int64  `json:"config_revision"`
}

type DeletePricingModelResponse struct {
	ConfigRevision int64 `json:"config_revision"`
}

// PricingFieldError 标明一个配置字段的问题；冲突时 BranchIDs 同时列出两条分支。
type PricingFieldError struct {
	Path      string   `json:"path"`
	Code      string   `json:"code"`
	BranchIDs []string `json:"branch_ids,omitempty"`
}

// PricingErrorResponse 是价格接口统一的可展示错误，不包含内部 SQL 或堆栈。
type PricingErrorResponse struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Fields  []PricingFieldError `json:"fields,omitempty"`
}

type RecalculationStatus string

const (
	RecalculationRunning   RecalculationStatus = "running"
	RecalculationCompleted RecalculationStatus = "completed"
	RecalculationFailed    RecalculationStatus = "failed"
)

type RecalculationStage string

const (
	RecalculationPreparing     RecalculationStage = "preparing"
	RecalculationEvents        RecalculationStage = "events"
	RecalculationUpdatingStats RecalculationStage = "updating_stats"
	RecalculationFinalizing    RecalculationStage = "finalizing"
)

type RecalculationTaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RecalculationTask 只描述当前进程中的一次运行；处理数只在费用事务提交后增加。
// TotalCount 为 nil 时总量尚未知，Error 为 nil 时没有任务错误。
type RecalculationTask struct {
	TaskID         string                  `json:"task_id"`
	Status         RecalculationStatus     `json:"status"`
	Stage          RecalculationStage      `json:"stage"`
	StartAt        time.Time               `json:"start_at"`
	EndAt          time.Time               `json:"end_at"`
	ConfigRevision int64                   `json:"config_revision"`
	ProcessedCount int64                   `json:"processed_count"`
	TotalCount     *int64                  `json:"total_count"`
	UpdatedAt      time.Time               `json:"updated_at"`
	Error          *RecalculationTaskError `json:"error"`
}

// RecalculationOptions 的边界由服务端按部署时区和受理时刻计算，时间含 UTC 偏移。
type RecalculationOptions struct {
	Timezone       string     `json:"timezone"`
	EarliestStart  *time.Time `json:"earliest_start"`
	LatestStart    *time.Time `json:"latest_start"`
	StepSeconds    int64      `json:"step_seconds"`
	MaxDays        int        `json:"max_days"`
	ConfigRevision int64      `json:"config_revision"`
}

type StartRecalculationRequest struct {
	StartAt        time.Time `json:"start_at"`
	ConfigRevision int64     `json:"config_revision"`
}

type StartRecalculationResponse struct {
	Started bool              `json:"started"`
	Task    RecalculationTask `json:"task"`
}
