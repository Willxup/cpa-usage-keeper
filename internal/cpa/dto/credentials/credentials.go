package credentials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// CredentialsResponse 是 CPA 凭证列表接口的响应 DTO。
type CredentialsResponse struct {
	// CPA 的 /credentials 列表仍以 files 为响应字段，保留上游协议名称。
	Files []Credential `json:"files"`
}

// Credential 保存凭证列表中单个凭证的原始响应字段。
type Credential struct {
	AuthIndex   string                    `json:"auth_index"`
	Name        string                    `json:"name"`
	Path        string                    `json:"path"`
	Email       string                    `json:"email"`
	Type        string                    `json:"type"`
	Provider    string                    `json:"provider"`
	Label       string                    `json:"label"`
	Status      string                    `json:"status"`
	Source      string                    `json:"source"`
	Prefix      string                    `json:"prefix"`
	Priority    *int                      `json:"priority"`
	Disabled    *bool                     `json:"disabled"`
	Note        *string                   `json:"note"`
	Unavailable bool                      `json:"unavailable"`
	RuntimeOnly bool                      `json:"runtime_only"`
	Account     string                    `json:"account,omitempty"`
	ProjectID   string                    `json:"project_id,omitempty"`
	Sub         *CredentialXAIUserIDValue `json:"sub,omitempty"`
	Subject     *CredentialXAIUserIDValue `json:"subject,omitempty"`
	UserID      *CredentialXAIUserIDValue `json:"user_id,omitempty"`
	UserIDCamel *CredentialXAIUserIDValue `json:"userId,omitempty"`
	Metadata    *CredentialClaims         `json:"metadata,omitempty"`
	Attributes  *CredentialClaims         `json:"attributes,omitempty"`
	OAuth       *CredentialOAuth          `json:"oauth,omitempty"`
	User        *CredentialUser           `json:"user,omitempty"`
	IDToken     *CredentialIDToken        `json:"id_token"`
	xaiOAuthSet bool
	xaiUserSet  bool
}

// CredentialClaims 是 CPA credential 中可能携带身份 subject 的通用 claims DTO。
type CredentialClaims struct {
	Sub         *CredentialXAIUserIDValue `json:"sub,omitempty"`
	Subject     *CredentialXAIUserIDValue `json:"subject,omitempty"`
	UserID      *CredentialXAIUserIDValue `json:"user_id,omitempty"`
	UserIDCamel *CredentialXAIUserIDValue `json:"userId,omitempty"`
	OAuth       *CredentialOAuth          `json:"oauth,omitempty"`
	User        *CredentialUser           `json:"user,omitempty"`
	xaiOAuthSet bool
	xaiUserSet  bool
}

// CredentialOAuth 是 CPA credential 中 oauth 身份字段的最小 DTO。
type CredentialOAuth struct {
	Sub     *CredentialXAIUserIDValue `json:"sub,omitempty"`
	Subject *CredentialXAIUserIDValue `json:"subject,omitempty"`
}

// CredentialUser 是 CPA credential 中 user 身份字段的最小 DTO。
type CredentialUser struct {
	Sub *CredentialXAIUserIDValue `json:"sub,omitempty"`
	ID  *CredentialXAIUserIDValue `json:"id,omitempty"`
}

// CredentialXAIUserIDValue 兼容 CPAMC 对 xAI user id 字符串和有限数字候选的读取语义。
type CredentialXAIUserIDValue string

// UnmarshalJSON 先沿用既有 Credential 字段解码，再对 xAI 候选按精确 JSON key 做容错解析。
func (file *Credential) UnmarshalJSON(data []byte) error {
	type credentialAlias Credential
	var decoded credentialAlias
	shadow := struct {
		*credentialAlias
		Sub         json.RawMessage `json:"sub"`
		Subject     json.RawMessage `json:"subject"`
		UserID      json.RawMessage `json:"user_id"`
		UserIDCamel json.RawMessage `json:"userId"`
		Metadata    json.RawMessage `json:"metadata"`
		Attributes  json.RawMessage `json:"attributes"`
		OAuth       json.RawMessage `json:"oauth"`
		User        json.RawMessage `json:"user"`
	}{credentialAlias: &decoded}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return fmt.Errorf("decode credential: %w", err)
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("decode credential claims: %w", err)
	}
	decoded.Sub = credentialXAIUserIDValue(object["sub"])
	decoded.Subject = credentialXAIUserIDValue(object["subject"])
	decoded.UserID = credentialXAIUserIDValue(object["user_id"])
	decoded.UserIDCamel = credentialXAIUserIDValue(object["userId"])
	decoded.Metadata = credentialClaims(object["metadata"])
	decoded.Attributes = credentialClaims(object["attributes"])
	decoded.OAuth, decoded.xaiOAuthSet = credentialOAuth(object["oauth"])
	decoded.User, decoded.xaiUserSet = credentialUser(object["user"])
	*file = Credential(decoded)
	return nil
}

// ResolvedXAIOAuth 按 file.oauth ?? metadata.oauth ?? attributes.oauth 选择候选对象。
func (file Credential) ResolvedXAIOAuth() *CredentialOAuth {
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
func (file Credential) ResolvedXAIUser() *CredentialUser {
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
func (value *CredentialXAIUserIDValue) UnmarshalJSON(data []byte) error {
	parsed := credentialXAIUserIDValue(data)
	if parsed == nil {
		*value = ""
		return nil
	}
	*value = *parsed
	return nil
}

func credentialClaims(data json.RawMessage) *CredentialClaims {
	object := credentialObject(data)
	if object == nil {
		return nil
	}
	claims := &CredentialClaims{
		Sub:         credentialXAIUserIDValue(object["sub"]),
		Subject:     credentialXAIUserIDValue(object["subject"]),
		UserID:      credentialXAIUserIDValue(object["user_id"]),
		UserIDCamel: credentialXAIUserIDValue(object["userId"]),
	}
	claims.OAuth, claims.xaiOAuthSet = credentialOAuth(object["oauth"])
	claims.User, claims.xaiUserSet = credentialUser(object["user"])
	return claims
}

func credentialOAuth(data json.RawMessage) (*CredentialOAuth, bool) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, false
	}
	object := credentialObject(data)
	if object == nil {
		return nil, true
	}
	return &CredentialOAuth{
		Sub:     credentialXAIUserIDValue(object["sub"]),
		Subject: credentialXAIUserIDValue(object["subject"]),
	}, true
}

func credentialUser(data json.RawMessage) (*CredentialUser, bool) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, false
	}
	object := credentialObject(data)
	if object == nil {
		return nil, true
	}
	return &CredentialUser{
		Sub: credentialXAIUserIDValue(object["sub"]),
		ID:  credentialXAIUserIDValue(object["id"]),
	}, true
}

func credentialObject(data json.RawMessage) map[string]json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil
	}
	return object
}

func credentialXAIUserIDValue(data json.RawMessage) *CredentialXAIUserIDValue {
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
		value := CredentialXAIUserIDValue(candidate)
		return &value
	case json.Number:
		number, err := strconv.ParseFloat(candidate.String(), 64)
		if err == nil && !math.IsInf(number, 0) && !math.IsNaN(number) {
			value := CredentialXAIUserIDValue(formatJavaScriptNumber(number))
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

// CredentialIDToken 是 Codex credential 的 id_token 订阅元数据 DTO。
type CredentialIDToken struct {
	AccountID        *string    `json:"chatgpt_account_id,omitempty"`
	AccountIDCamel   *string    `json:"chatgptAccountId,omitempty"`
	ActiveStart      *time.Time `json:"chatgpt_subscription_active_start,omitempty"`
	ActiveStartCamel *time.Time `json:"chatgptSubscriptionActiveStart,omitempty"`
	ActiveUntil      *time.Time `json:"chatgpt_subscription_active_until,omitempty"`
	ActiveUntilCamel *time.Time `json:"chatgptSubscriptionActiveUntil,omitempty"`
	PlanType         *string    `json:"plan_type,omitempty"`
	PlanTypeCamel    *string    `json:"planType,omitempty"`
}
