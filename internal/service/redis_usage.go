package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

var (
	reSKKey         = regexp.MustCompile(`sk-[A-Za-z0-9_-]{10,}`)
	reAuthorization = regexp.MustCompile(`(?i)Authorization\s*:\s*Bearer\s+\S+`)
	reCookie        = regexp.MustCompile(`(?i)(?:Set-Cookie|Cookie)\s*:\s*[^\r\n]+`)
	reTokenValue    = regexp.MustCompile(`(?i)((?:access_token|refresh_token)\s*[=:]\s*)"?[A-Za-z0-9._\-]+"?`)
	reURL           = regexp.MustCompile(`https?://[^\s"',}\]]+`)
	reMultiSpace    = regexp.MustCompile(`\s{2,}`)
	reJSONTokenField = regexp.MustCompile(`(?i)"(?:access_token|refresh_token|api_key|authorization|token|cookie|set-cookie)"\s*:\s*"[^"]*"`)
)

type RedisQueue interface {
	PopUsage(ctx context.Context) ([]string, error)
}

// DecodeRedisUsageMessage 将 redis_inboxes.raw_message 原样解码为 usage_events 入库实体。
// request_id 缺失时：failed=true 的事件使用 fallback key 入库，failed=false 仍 decode_failed。
func DecodeRedisUsageMessage(message string, fetchedAt time.Time) (entities.UsageEvent, json.RawMessage, error) {
	raw := json.RawMessage(message)
	var payload queuedUsageDetail
	if err := json.Unmarshal(raw, &payload); err != nil {
		return entities.UsageEvent{}, nil, fmt.Errorf("decode redis usage message: %w", err)
	}
	if strings.TrimSpace(payload.RequestID) == "" {
		if !payload.Failed {
			return entities.UsageEvent{}, raw, fmt.Errorf("decode redis usage message: request_id is required")
		}
		// F 类兜底：failed=true 但缺 request_id，生成稳定 fallback EventKey 允许入库。
		payload.RequestID = ""
		event := payload.toUsageEvent(fetchedAt)
		event.EventKey = buildFallbackUsageEventKey(raw)
		return event, raw, nil
	}
	return payload.toUsageEvent(fetchedAt), raw, nil
}

// buildFallbackUsageEventKey 为缺 request_id 的 failed 事件生成稳定 EventKey。
// 使用 sha256(raw_message) 前 8 字节（16 位十六进制），确保同一消息不会重复入库。
func buildFallbackUsageEventKey(raw json.RawMessage) string {
	h := sha256.Sum256(raw)
	return fmt.Sprintf("failed:%x", h[:8])
}

// failPayload 对应 CPA usage 消息中 fail 字段（CPA 实际使用的错误结构）。
// CPA plugin.go resolveFail() 始终填充 status_code，body 携带上游响应原文。
type failPayload struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

// queuedUsageDetail 对应 CPA Redis 队列中的单条 usage JSON payload。
type queuedUsageDetail struct {
	Timestamp       time.Time           `json:"timestamp"`
	LatencyMS       int64               `json:"latency_ms"`
	TTFTMS          *int64              `json:"ttft_ms"`
	Source          string              `json:"source"`
	AuthIndex       string              `json:"auth_index"`
	Tokens          dto.TokenStats      `json:"tokens"`
	Failed          bool                `json:"failed"`
	Fail            failPayload         `json:"fail"`
	Provider        string              `json:"provider"`
	Model           string              `json:"model"`
	Alias           *string             `json:"alias"`
	ReasoningEffort string              `json:"reasoning_effort"`
	ServiceTier     string              `json:"service_tier"`
	ExecutorType    string              `json:"executor_type"`
	Endpoint        string              `json:"endpoint"`
	AuthType        string              `json:"auth_type"`
	APIKey          string              `json:"api_key"`
	RequestID       string              `json:"request_id"`
	StatusCode      int                 `json:"status_code"`
	Error           *errorTelemetryBody `json:"error"`
}

// errorTelemetryBody 是 CPA usage 消息中 error 字段的 flexible DTO，支持多种嵌套路径。
type errorTelemetryBody struct {
	Type            string          `json:"type"`
	Code            json.RawMessage `json:"code"`
	Error           json.RawMessage `json:"error"`
	Status          json.RawMessage `json:"status"`
	Message         string          `json:"message"`
	RawBody         *string         `json:"response_body,omitempty"`
	Body            *string         `json:"body,omitempty"`
	BodyText        *string         `json:"body_text,omitempty"`
	BodyTextCamel   *string         `json:"bodyText,omitempty"`
	ResetsAt        *time.Time      `json:"resets_at"`
	ResetsInSeconds *int64          `json:"resets_in_seconds"`
}

// usageFailureTelemetry 是从 usage 消息中提取的归一化失败遥测数据。
type usageFailureTelemetry struct {
	StatusCode *int
	Code       string
	Message    string
	Body       string
}

// Codex429Telemetry 是从 usage 消息中提取的 codex 429 错误遥测数据，供 cooldown 处理使用。
type Codex429Telemetry struct {
	StatusCode   int
	ErrorType    string
	ResetsAt     *time.Time
	ResetsInSec  *int64
	RawErrorBody string
	RequestID    string
	AuthIndex    string
	Provider     string
}

// ExtractCodex429Telemetry 从解码后的 raw message 中提取 codex 429 usage_limit_reached 遥测。
// 复用 extractUsageFailureTelemetry 底层解析，确保与通用失败详情保持一致。
// 不触发 cooldown 的场景（非 codex、非 429、非 usage_limit_reached）返回 nil。
func ExtractCodex429Telemetry(rawMsg json.RawMessage) *Codex429Telemetry {
	if len(rawMsg) == 0 {
		return nil
	}
	var detail queuedUsageDetail
	if err := json.Unmarshal(rawMsg, &detail); err != nil {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(detail.Provider)) != "codex" {
		return nil
	}
	effectiveStatusCode := detail.StatusCode
	if effectiveStatusCode == 0 && detail.Fail.StatusCode > 0 {
		effectiveStatusCode = detail.Fail.StatusCode
	}
	if effectiveStatusCode != 429 || !detail.Failed || detail.Error == nil {
		return nil
	}
	failureTel := extractUsageFailureTelemetry(detail.Failed, detail.StatusCode, detail.Error, &detail.Fail)
	if failureTel.Code != "usage_limit_reached" {
		return nil
	}
	tel := &Codex429Telemetry{
		StatusCode:   effectiveStatusCode,
		ErrorType:    failureTel.Code,
		RequestID:    strings.TrimSpace(detail.RequestID),
		AuthIndex:    strings.TrimSpace(detail.AuthIndex),
		Provider:     strings.TrimSpace(detail.Provider),
		ResetsAt:     detail.Error.ResetsAt,
		ResetsInSec:  detail.Error.ResetsInSeconds,
		RawErrorBody: resolveFailureRawBody(detail.Error),
	}
	return tel
}

// RawErrorBodyTruncated 返回截断后的原始错误 body（最长 1024 字节），避免暴露敏感数据。
func (t *Codex429Telemetry) RawErrorBodyTruncated() string {
	if t == nil || t.RawErrorBody == "" {
		return ""
	}
	if len(t.RawErrorBody) > 1024 {
		return t.RawErrorBody[:1024]
	}
	return t.RawErrorBody
}

func normalizeRedisAuthType(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "api_key" {
		return "apikey"
	}
	return trimmed
}

func trimRedisOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// toUsageEvent 保持 Redis payload 的 model/request_id 语义，缺失时间才用本地拉取时间兜底。
func (d queuedUsageDetail) toUsageEvent(fetchedAt time.Time) entities.UsageEvent {
	apiGroupKey := firstNonEmpty(d.APIKey, d.Provider, d.Endpoint, "unknown")
	model := firstNonEmpty(d.Model, "unknown")
	timestamp := timeutil.NormalizeStorageTime(d.Timestamp)
	if timestamp.IsZero() {
		timestamp = timeutil.NormalizeStorageTime(fetchedAt)
	}
	source := strings.TrimSpace(d.Source)
	authIndex := strings.TrimSpace(d.AuthIndex)
	eventKey := strings.TrimSpace(d.RequestID)
	ftel := extractUsageFailureTelemetry(d.Failed, d.StatusCode, d.Error, &d.Fail)
	return entities.UsageEvent{
		EventKey:            eventKey,
		APIGroupKey:         apiGroupKey,
		Provider:            strings.TrimSpace(d.Provider),
		Endpoint:            strings.TrimSpace(d.Endpoint),
		AuthType:            normalizeRedisAuthType(d.AuthType),
		RequestID:           strings.TrimSpace(d.RequestID),
		Model:               model,
		ModelAlias:          trimRedisOptionalString(d.Alias),
		ReasoningEffort:     strings.TrimSpace(d.ReasoningEffort),
		ServiceTier:         strings.TrimSpace(d.ServiceTier),
		ExecutorType:        strings.TrimSpace(d.ExecutorType),
		Timestamp:           timestamp,
		Source:              source,
		AuthIndex:           authIndex,
		Failed:              d.Failed,
		LatencyMS:           max(d.LatencyMS, 0),
		TTFTMS:              d.TTFTMS,
		InputTokens:         d.Tokens.InputTokens,
		OutputTokens:        d.Tokens.OutputTokens,
		ReasoningTokens:     d.Tokens.ReasoningTokens,
		CachedTokens:        d.Tokens.CachedTokens,
		CacheReadTokens:     d.Tokens.CacheReadTokens,
		CacheCreationTokens: d.Tokens.CacheCreationTokens,
		TotalTokens:         d.Tokens.TotalTokens,
		FailureStatusCode:   ftel.StatusCode,
		FailureCode:         ftel.Code,
		FailureMessage:      ftel.Message,
		FailureBody:         ftel.Body,
	}
}

// extractUsageFailureTelemetry 从 usage payload 中提取归一化的失败遥测数据。
// 优先使用 CPA 实际发送的 fail 字段（fail.status_code / fail.body），
// 同时兼容 error 字段（OpenAI/Codex、Anthropic、Gemini/Google、OpenRouter 包装结构）。
func extractUsageFailureTelemetry(failed bool, statusCode int, errBody *errorTelemetryBody, fail *failPayload) usageFailureTelemetry {
	if !failed {
		return usageFailureTelemetry{}
	}
	tel := usageFailureTelemetry{}
	// 优先从 CPA fail.status_code 获取 HTTP 状态码
	if fail != nil && fail.StatusCode > 0 {
		sc := fail.StatusCode
		tel.StatusCode = &sc
	} else if statusCode > 0 {
		tel.StatusCode = &statusCode
	}
	// 优先从 CPA fail.body 获取上游响应 body
	failBody := ""
	if fail != nil {
		failBody = strings.TrimSpace(fail.Body)
	}
	// error 字段解析（兼容多种上游包装结构）
	if errBody != nil {
		code, isWrapper, nested := resolveFailureCodeInfo(errBody)
		if isWrapper && nested != nil {
			if nested.Type != "" {
				code = strings.TrimSpace(nested.Type)
			}
			if nested.Message != "" && tel.Message == "" {
				tel.Message = strings.TrimSpace(nested.Message)
			}
		}
		tel.Code = normalizeFailureCode(code)
		if tel.StatusCode == nil {
			if sc := normalizeFailureStatusCode(errBody, nested); sc != nil {
				tel.StatusCode = sc
			}
		}
		if tel.Message == "" {
			tel.Message = normalizeFailureMessage(errBody, nested)
		}
		body := sanitizeFailureBody(resolveFailureRawBody(errBody))
		if body == "" {
			body = sanitizeFailureBody(marshalErrorObject(errBody))
		}
		tel.Body = body
		if tel.Code == "" || tel.Message == "" {
			fillFromSanitizedBody(body, &tel)
		}
		if tel.Code == "" {
			tel.Code = tryExtractCodeFromText(body)
		}
		if tel.Message == "" && len(body) > 0 {
			tel.Message = truncateFailureText(body, 300)
		}
	}
	// fail.body 作为 body 的优先来源（CPA 直接传递的上游响应）
	if tel.Body == "" && failBody != "" {
		tel.Body = sanitizeFailureBody(failBody)
		// 从 fail.body 中尝试补全缺失的 code 和 message
		if tel.Code == "" || tel.Message == "" {
			fillFromSanitizedBody(tel.Body, &tel)
		}
		if tel.Code == "" {
			tel.Code = tryExtractCodeFromText(tel.Body)
		}
		if tel.Message == "" && len(tel.Body) > 0 {
			tel.Message = truncateFailureText(tel.Body, 300)
		}
	}
	return tel
}

// nestedErrorInfo 携带嵌套 error 解析结果和 Anthropic wrapper 标记。
type nestedErrorInfo struct {
	Code    string
	Type    string
	Message string
	Status  json.RawMessage
}

// resolveFailureCodeInfo 从 errorTelemetryBody 提取初始 code，并解析嵌套 error 路径。
// 返回初始 code、是否为 Anthropic wrapper、嵌套 error 信息。
func resolveFailureCodeInfo(errBody *errorTelemetryBody) (string, bool, *nestedErrorInfo) {
	initialCode := strings.TrimSpace(errBody.Type)
	isWrapper := initialCode == "error"
	var nested *nestedErrorInfo
	if len(errBody.Error) > 0 {
		var n struct {
			Code    json.RawMessage `json:"code"`
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Status  json.RawMessage `json:"status"`
		}
		if err := json.Unmarshal(errBody.Error, &n); err == nil {
			nested = &nestedErrorInfo{
				Type:    n.Type,
				Message: n.Message,
				Status:  n.Status,
			}
			if s := rawMessageToString(n.Code); s != "" {
				nested.Code = s
			}
			// 非 wrapper 时，嵌套 code/type 可补全初始 code
			if !isWrapper && initialCode == "" {
				if nested.Code != "" {
					initialCode = nested.Code
				} else if nested.Type != "" {
					initialCode = nested.Type
				}
			}
		}
	}
	// error.code / error.status 作为初始 code 候选
	if !isWrapper && initialCode == "" {
		if s := rawMessageToString(errBody.Code); s != "" {
			initialCode = s
		}
	}
	// Gemini error.status 是非数字 code（如 RESOURCE_EXHAUSTED），
	// 当 initialCode 为纯数字时用作回退，避免 code 被清空。
	if !isWrapper && isNumericString(initialCode) {
		if s := rawMessageToString(errBody.Status); s != "" && !isNumericString(s) {
			initialCode = s
		}
	}
	return initialCode, isWrapper, nested
}

// normalizeFailureStatusCode 从 error 字段推断 HTTP status code。
// Gemini error.code 如果是数字则作为 status code。
func normalizeFailureStatusCode(errBody *errorTelemetryBody, nested *nestedErrorInfo) *int {
	if errBody == nil {
		return nil
	}
	if sc := parseNumericStatusCode(errBody.Code); sc != nil {
		return sc
	}
	if sc := parseNumericStatusCode(errBody.Status); sc != nil {
		return sc
	}
	if nested != nil {
		if sc := parseNumericStatusCode(nested.Status); sc != nil {
			return sc
		}
	}
	return nil
}

// normalizeFailureCode 清洗错误码，纯数字不适合作为 code。
func normalizeFailureCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if isNumericString(code) {
		return ""
	}
	return strings.ToLower(code)
}

// normalizeFailureMessage 从 error 结构中提取消息。
func normalizeFailureMessage(errBody *errorTelemetryBody, nested *nestedErrorInfo) string {
	if msg := strings.TrimSpace(errBody.Message); msg != "" {
		return msg
	}
	if nested != nil {
		if msg := strings.TrimSpace(nested.Message); msg != "" {
			return msg
		}
	}
	return ""
}

// resolveFailureRawBody 从 error 结构中选取原始 body 字段。
func resolveFailureRawBody(errBody *errorTelemetryBody) string {
	switch {
	case errBody.RawBody != nil:
		return *errBody.RawBody
	case errBody.Body != nil:
		return *errBody.Body
	case errBody.BodyText != nil:
		return *errBody.BodyText
	case errBody.BodyTextCamel != nil:
		return *errBody.BodyTextCamel
	}
	return ""
}

// marshalErrorObject 将 error 结构体序列化为 JSON 字符串作为 body 兜底。
func marshalErrorObject(errBody *errorTelemetryBody) string {
	if b, err := json.Marshal(errBody); err == nil {
		return string(b)
	}
	return ""
}

// fillFromSanitizedBody 从脱敏后的 body JSON 中补全缺失的 code 和 message。
func fillFromSanitizedBody(body string, tel *usageFailureTelemetry) {
	var parsed struct {
		Error struct {
			Code    json.RawMessage `json:"code"`
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Status  string          `json:"status"`
		} `json:"error"`
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Type    string          `json:"type"`
		Status  json.RawMessage `json:"status"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return
	}
	if tel.Message == "" {
		if parsed.Error.Message != "" {
			tel.Message = strings.TrimSpace(parsed.Error.Message)
		} else if parsed.Message != "" {
			tel.Message = strings.TrimSpace(parsed.Message)
		}
	}
	if tel.Code == "" {
		candidates := []string{
			rawMessageToString(parsed.Error.Code),
			parsed.Error.Type,
			parsed.Error.Status,
			rawMessageToString(parsed.Code),
			parsed.Type,
			rawMessageToString(parsed.Status),
		}
		for _, c := range candidates {
			if nc := normalizeFailureCode(c); nc != "" {
				tel.Code = nc
				break
			}
		}
	}
	if tel.StatusCode == nil {
		if sc := parseNumericStatusCode(parsed.Error.Code); sc != nil {
			tel.StatusCode = sc
		} else if sc := parseNumericStatusCode(parsed.Status); sc != nil {
			tel.StatusCode = sc
		}
	}
}

// parseNumericStatusCode 将 json.RawMessage 解析为数字 status code。
func parseNumericStatusCode(raw json.RawMessage) *int {
	if len(raw) == 0 {
		return nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil && n >= 100 && n <= 599 {
		v := int(n)
		return &v
	}
	if s := rawMessageToString(raw); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 100 && v <= 599 {
			return &v
		}
	}
	return nil
}

// isNumericString 判断字符串是否为纯数字。
func isNumericString(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// rawMessageToString 将 json.RawMessage 解析为字符串，解析失败返回空串。
func rawMessageToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// tryExtractCodeFromText 从非 JSON 文本中尝试提取 snake_case 错误码。
func tryExtractCodeFromText(text string) string {
	if text == "" {
		return ""
	}
	// 匹配 snake_case 风格的错误码
	for _, candidate := range knownErrorCodes {
		if strings.Contains(text, candidate) {
			return candidate
		}
	}
	return ""
}

var knownErrorCodes = []string{
	"usage_limit_reached",
	"rate_limit_exceeded",
	"auth_unavailable",
	"authentication_error",
	"invalid_request",
	"invalid_request_error",
	"insufficient_quota",
	"resource_exhausted",
	"permission_denied",
	"model_not_found",
	"not_implemented",
	"unsupported_parameter",
	"unknown_provider",
	"bad_gateway",
	"service_unavailable",
}

// sanitizeFailureBody 脱敏并截断 failure body。
func sanitizeFailureBody(body string) string {
	if body == "" {
		return ""
	}
	// 替换 sk- 密钥
	body = reSKKey.ReplaceAllString(body, "[redacted_key]")
	// 替换 Authorization 头（大小写不敏感）
	body = reAuthorization.ReplaceAllString(body, "[redacted_authorization]")
	// 替换 Cookie / Set-Cookie
	body = reCookie.ReplaceAllString(body, "[redacted_cookie]")
	// 替换 access_token / refresh_token 值（非 JSON 上下文）
	body = reTokenValue.ReplaceAllString(body, "${1}[redacted_token]")
	// 替换 JSON 敏感字段值："access_token": "xxx" 等
	body = reJSONTokenField.ReplaceAllStringFunc(body, func(match string) string {
		colonIdx := strings.Index(match, ":")
		if colonIdx < 0 {
			return match
		}
		return match[:colonIdx+1] + ` "[redacted]"`
	})
	// 替换 URL
	body = reURL.ReplaceAllString(body, "[redacted_url]")
	// 合并连续空白
	body = reMultiSpace.ReplaceAllString(body, " ")
	return truncateFailureText(body, 4000)
}

// truncateFailureText 按 rune 截断文本。
func truncateFailureText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
