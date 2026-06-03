package authfiles

import "time"

// AuthFilesResponse 是 CPA /management/auth-files 响应 DTO。
type AuthFilesResponse struct {
	Files []AuthFile `json:"files"`
}

// AuthFile 是 CPA /management/auth-files 中单个 auth file 的原始响应 DTO。
type AuthFile struct {
	AuthIndex   string           `json:"auth_index"`
	Name        string           `json:"name"`
	Path        string           `json:"path"`
	Email       string           `json:"email"`
	Type        string           `json:"type"`
	Provider    string           `json:"provider"`
	Label       string           `json:"label"`
	Status      string           `json:"status"`
	Source      string           `json:"source"`
	Prefix      string           `json:"prefix"`
	Priority    *int             `json:"priority"`
	Disabled    *bool            `json:"disabled"`
	Note        *string          `json:"note"`
	Unavailable bool             `json:"unavailable"`
	RuntimeOnly bool             `json:"runtime_only"`
	Account     string           `json:"account,omitempty"`
	ProjectID   string           `json:"project_id,omitempty"`
	IDToken     *AuthFileIDToken `json:"id_token"`
}

// AuthFileIDToken 是 Codex auth file 的 id_token 订阅元数据 DTO。
type AuthFileIDToken struct {
	AccountID        *string    `json:"chatgpt_account_id,omitempty"`
	AccountIDCamel   *string    `json:"chatgptAccountId,omitempty"`
	ActiveStart      *time.Time `json:"chatgpt_subscription_active_start,omitempty"`
	ActiveStartCamel *time.Time `json:"chatgptSubscriptionActiveStart,omitempty"`
	ActiveUntil      *time.Time `json:"chatgpt_subscription_active_until,omitempty"`
	ActiveUntilCamel *time.Time `json:"chatgptSubscriptionActiveUntil,omitempty"`
	PlanType         *string    `json:"plan_type,omitempty"`
	PlanTypeCamel    *string    `json:"planType,omitempty"`
}
