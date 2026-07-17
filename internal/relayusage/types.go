package relayusage

import (
	"time"

	"cpa-usage-keeper/internal/quota"
)

// RelayBalance 描述中转商账户余额。DeepSeek 等只返回余额的平台使用，
// GLM/MiniMax/Kimi 等返回用量窗口的平台该字段为空。
type RelayBalance struct {
	Available float64 `json:"available"`
	Granted   float64 `json:"granted,omitempty"`
	ToppedUp  float64 `json:"toppedUp,omitempty"`
	Currency  string  `json:"currency,omitempty"`
}

// RelayUsageResult 是单个中转商凭据的用量查询结果。
// Rows 复用 quota.QuotaRow，前端可直接复用 UsageQuotaRow 的渲染逻辑。
type RelayUsageResult struct {
	Platform  string           `json:"platform"`
	Balance   *RelayBalance    `json:"balance,omitempty"`
	Rows      []quota.QuotaRow `json:"rows,omitempty"`
	FetchedAt time.Time        `json:"fetchedAt"`
	Error     string           `json:"error,omitempty"`
}
