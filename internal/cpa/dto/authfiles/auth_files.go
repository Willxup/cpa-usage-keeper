package authfiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// AuthFilesResponse 是 CPA /management/auth-files 响应 DTO。
type AuthFilesResponse struct {
	Files []AuthFile `json:"files"`
}

// AuthFile 是 CPA /management/auth-files 中单个 auth file 的原始响应 DTO。
type AuthFile struct {
	AuthIndex   string                  `json:"auth_index"`
	Name        string                  `json:"name"`
	Path        string                  `json:"path"`
	Email       string                  `json:"email"`
	Type        string                  `json:"type"`
	Provider    string                  `json:"provider"`
	Label       string                  `json:"label"`
	Status      string                  `json:"status"`
	Source      string                  `json:"source"`
	Prefix      string                  `json:"prefix"`
	Priority    *int                    `json:"priority"`
	Disabled    *bool                   `json:"disabled"`
	Note        *string                 `json:"note"`
	Unavailable bool                    `json:"unavailable"`
	RuntimeOnly bool                    `json:"runtime_only"`
	Account     string                  `json:"account,omitempty"`
	ProjectID   string                  `json:"project_id,omitempty"`
	Sub         *AuthFileXAIUserIDValue `json:"sub,omitempty"`
	Subject     *AuthFileXAIUserIDValue `json:"subject,omitempty"`
	UserID      *AuthFileXAIUserIDValue `json:"user_id,omitempty"`
	UserIDCamel *AuthFileXAIUserIDValue `json:"userId,omitempty"`
	Metadata    *AuthFileClaims         `json:"metadata,omitempty"`
	Attributes  *AuthFileClaims         `json:"attributes,omitempty"`
	OAuth       *AuthFileOAuth          `json:"oauth,omitempty"`
	User        *AuthFileUser           `json:"user,omitempty"`
	IDToken     *AuthFileIDToken        `json:"id_token"`
}

// AuthFileClaims 是 CPA auth file 中可能携带身份 subject 的通用 claims DTO。
type AuthFileClaims struct {
	Sub         *AuthFileXAIUserIDValue `json:"sub,omitempty"`
	Subject     *AuthFileXAIUserIDValue `json:"subject,omitempty"`
	UserID      *AuthFileXAIUserIDValue `json:"user_id,omitempty"`
	UserIDCamel *AuthFileXAIUserIDValue `json:"userId,omitempty"`
	OAuth       *AuthFileOAuth          `json:"oauth,omitempty"`
	User        *AuthFileUser           `json:"user,omitempty"`
}

// AuthFileOAuth 是 CPA auth file 中 oauth 身份字段的最小 DTO。
type AuthFileOAuth struct {
	Sub     *AuthFileXAIUserIDValue `json:"sub,omitempty"`
	Subject *AuthFileXAIUserIDValue `json:"subject,omitempty"`
}

// AuthFileUser 是 CPA auth file 中 user 身份字段的最小 DTO。
type AuthFileUser struct {
	Sub *AuthFileXAIUserIDValue `json:"sub,omitempty"`
	ID  *AuthFileXAIUserIDValue `json:"id,omitempty"`
}

// AuthFileXAIUserIDValue 兼容 CPAMC 对 xAI user id 字符串和有限数字候选的读取语义。
type AuthFileXAIUserIDValue string

// UnmarshalJSON 将有限数字规范成字符串；对象、数组和布尔值保持为空，交给同步层跳过。
func (value *AuthFileXAIUserIDValue) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("decode xAI user id candidate: %w", err)
	}
	switch candidate := raw.(type) {
	case string:
		*value = AuthFileXAIUserIDValue(candidate)
	case json.Number:
		number, err := strconv.ParseFloat(candidate.String(), 64)
		if err == nil && !math.IsInf(number, 0) && !math.IsNaN(number) {
			*value = AuthFileXAIUserIDValue(strconv.FormatFloat(number, 'g', -1, 64))
		}
	default:
		*value = ""
	}
	return nil
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
