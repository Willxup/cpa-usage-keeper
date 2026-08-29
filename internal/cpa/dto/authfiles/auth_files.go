package authfiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
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
	xaiOAuthSet bool
	xaiUserSet  bool
}

// AuthFileClaims 是 CPA auth file 中可能携带身份 subject 的通用 claims DTO。
type AuthFileClaims struct {
	Sub         *AuthFileXAIUserIDValue `json:"sub,omitempty"`
	Subject     *AuthFileXAIUserIDValue `json:"subject,omitempty"`
	UserID      *AuthFileXAIUserIDValue `json:"user_id,omitempty"`
	UserIDCamel *AuthFileXAIUserIDValue `json:"userId,omitempty"`
	OAuth       *AuthFileOAuth          `json:"oauth,omitempty"`
	User        *AuthFileUser           `json:"user,omitempty"`
	xaiOAuthSet bool
	xaiUserSet  bool
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

// UnmarshalJSON 先沿用既有 AuthFile 字段解码，再对 xAI 候选按精确 JSON key 做容错解析。
func (file *AuthFile) UnmarshalJSON(data []byte) error {
	type authFileAlias AuthFile
	var decoded authFileAlias
	shadow := struct {
		*authFileAlias
		Sub         json.RawMessage `json:"sub"`
		Subject     json.RawMessage `json:"subject"`
		UserID      json.RawMessage `json:"user_id"`
		UserIDCamel json.RawMessage `json:"userId"`
		Metadata    json.RawMessage `json:"metadata"`
		Attributes  json.RawMessage `json:"attributes"`
		OAuth       json.RawMessage `json:"oauth"`
		User        json.RawMessage `json:"user"`
	}{authFileAlias: &decoded}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return fmt.Errorf("decode auth file: %w", err)
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("decode auth file claims: %w", err)
	}
	decoded.Sub = authFileXAIUserIDValue(object["sub"])
	decoded.Subject = authFileXAIUserIDValue(object["subject"])
	decoded.UserID = authFileXAIUserIDValue(object["user_id"])
	decoded.UserIDCamel = authFileXAIUserIDValue(object["userId"])
	decoded.Metadata = authFileClaims(object["metadata"])
	decoded.Attributes = authFileClaims(object["attributes"])
	decoded.OAuth, decoded.xaiOAuthSet = authFileOAuth(object["oauth"])
	decoded.User, decoded.xaiUserSet = authFileUser(object["user"])
	*file = AuthFile(decoded)
	return nil
}

// ResolvedXAIOAuth 按 file.oauth ?? metadata.oauth ?? attributes.oauth 选择候选对象。
func (file AuthFile) ResolvedXAIOAuth() *AuthFileOAuth {
	if file.OAuth != nil || file.xaiOAuthSet {
		return file.OAuth
	}
	if file.Metadata != nil && (file.Metadata.OAuth != nil || file.Metadata.xaiOAuthSet) {
		return file.Metadata.OAuth
	}
	if file.Attributes != nil && (file.Attributes.OAuth != nil || file.Attributes.xaiOAuthSet) {
		return file.Attributes.OAuth
	}
	return nil
}

// ResolvedXAIUser 按 file.user ?? metadata.user ?? attributes.user 选择候选对象。
func (file AuthFile) ResolvedXAIUser() *AuthFileUser {
	if file.User != nil || file.xaiUserSet {
		return file.User
	}
	if file.Metadata != nil && (file.Metadata.User != nil || file.Metadata.xaiUserSet) {
		return file.Metadata.User
	}
	if file.Attributes != nil && (file.Attributes.User != nil || file.Attributes.xaiUserSet) {
		return file.Attributes.User
	}
	return nil
}

// UnmarshalJSON 将有限数字规范成字符串；对象、数组和布尔值保持为空，交给同步层跳过。
func (value *AuthFileXAIUserIDValue) UnmarshalJSON(data []byte) error {
	parsed := authFileXAIUserIDValue(data)
	if parsed == nil {
		*value = ""
		return nil
	}
	*value = *parsed
	return nil
}

func authFileClaims(data json.RawMessage) *AuthFileClaims {
	object := authFileObject(data)
	if object == nil {
		return nil
	}
	claims := &AuthFileClaims{
		Sub:         authFileXAIUserIDValue(object["sub"]),
		Subject:     authFileXAIUserIDValue(object["subject"]),
		UserID:      authFileXAIUserIDValue(object["user_id"]),
		UserIDCamel: authFileXAIUserIDValue(object["userId"]),
	}
	claims.OAuth, claims.xaiOAuthSet = authFileOAuth(object["oauth"])
	claims.User, claims.xaiUserSet = authFileUser(object["user"])
	return claims
}

func authFileOAuth(data json.RawMessage) (*AuthFileOAuth, bool) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, false
	}
	object := authFileObject(data)
	if object == nil {
		return nil, true
	}
	return &AuthFileOAuth{
		Sub:     authFileXAIUserIDValue(object["sub"]),
		Subject: authFileXAIUserIDValue(object["subject"]),
	}, true
}

func authFileUser(data json.RawMessage) (*AuthFileUser, bool) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, false
	}
	object := authFileObject(data)
	if object == nil {
		return nil, true
	}
	return &AuthFileUser{
		Sub: authFileXAIUserIDValue(object["sub"]),
		ID:  authFileXAIUserIDValue(object["id"]),
	}, true
}

func authFileObject(data json.RawMessage) map[string]json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil
	}
	return object
}

func authFileXAIUserIDValue(data json.RawMessage) *AuthFileXAIUserIDValue {
	if len(data) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil
	}
	switch candidate := raw.(type) {
	case string:
		value := AuthFileXAIUserIDValue(candidate)
		return &value
	case json.Number:
		number, err := strconv.ParseFloat(candidate.String(), 64)
		if err == nil && !math.IsInf(number, 0) && !math.IsNaN(number) {
			value := AuthFileXAIUserIDValue(formatJavaScriptNumber(number))
			return &value
		}
	}
	return nil
}

func formatJavaScriptNumber(value float64) string {
	if value == 0 {
		return "0"
	}
	formatted := strconv.FormatFloat(value, 'g', -1, 64)
	abs := math.Abs(value)
	if abs >= 1e-6 && abs < 1e21 {
		return expandScientificNumber(formatted)
	}
	return normalizeScientificExponent(formatted)
}

func expandScientificNumber(value string) string {
	mantissa, exponentText, ok := strings.Cut(value, "e")
	if !ok {
		return value
	}
	exponent, err := strconv.Atoi(exponentText)
	if err != nil {
		return value
	}
	sign := ""
	if strings.HasPrefix(mantissa, "-") {
		sign = "-"
		mantissa = strings.TrimPrefix(mantissa, "-")
	}
	decimalIndex := strings.IndexByte(mantissa, '.')
	if decimalIndex < 0 {
		decimalIndex = len(mantissa)
	} else {
		mantissa = strings.Replace(mantissa, ".", "", 1)
	}
	decimalIndex += exponent
	switch {
	case decimalIndex <= 0:
		return sign + "0." + strings.Repeat("0", -decimalIndex) + mantissa
	case decimalIndex >= len(mantissa):
		return sign + mantissa + strings.Repeat("0", decimalIndex-len(mantissa))
	default:
		return sign + mantissa[:decimalIndex] + "." + mantissa[decimalIndex:]
	}
}

func normalizeScientificExponent(value string) string {
	mantissa, exponentText, ok := strings.Cut(value, "e")
	if !ok {
		return value
	}
	exponent, err := strconv.Atoi(exponentText)
	if err != nil {
		return value
	}
	if exponent >= 0 {
		return mantissa + "e+" + strconv.Itoa(exponent)
	}
	return mantissa + "e" + strconv.Itoa(exponent)
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
