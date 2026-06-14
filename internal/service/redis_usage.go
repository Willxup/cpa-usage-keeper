package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

var (
	reSKKey         = regexp.MustCompile(`sk-[A-Za-z0-9_-]{10,}`)
	reAuthorization = regexp.MustCompile(`(?i)(?:Authorization|authorization)\s*:\s*Bearer\s+\S+`)
	reCookie        = regexp.MustCompile(`(?i)(?:Set-Cookie|Cookie)\s*:\s*[^\r\n]+`)
	reTokenValue    = regexp.MustCompile(`(?i)((?:access_token|refresh_token)\s*[=:]\s*)"?[A-Za-z0-9._\-]+"?`)
	reURL           = regexp.MustCompile(`https?://[^\s"',}\]]+`)
	reMultiSpace    = regexp.MustCompile(`\s{2,}`)
)

type RedisQueue interface {
	PopUsage(ctx context.Context) ([]string, error)
}

// DecodeRedisUsageMessage 将 redis_inboxes.raw_message 原样解码为 usage_events 入库实体。
func DecodeRedisUsageMessage(message string, fetchedAt time.Time) (entities.UsageEvent, json.RawMessage, error) {
	raw := json.RawMessage(message)
	var payload queuedUsageDetail
	if err := json.Unmarshal(raw, &payload); err != nil {
		return entities.UsageEvent{}, nil, fmt.Errorf("decode redis usage message: %w", err)
	}
	if strings.TrimSpace(payload.RequestID) == "" {
		return entities.UsageEvent{}, raw, fmt.Errorf("decode redis usage message: request_id is required")
	}
	return payload.toUsageEvent(fetchedAt), raw, nil
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
	Error           json.RawMessage `json:"error"` // 嵌套 error，例如 error.error.type
	ResetsAt        *time.Time      `json:"resets_at"`
	ResetsInSeconds *int64          `json:"resets_in_seconds"`
	Message         string          `json:"message"`
	RawBody         *string         `json:"response_body,omitempty"`
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
// 支持多种嵌套 error 结构：error.type、error.error.type、response.error.type。
// 不触发 cooldown 的场景（非 codex、非 429、非 usage_limit_reached）返回 nil。
func ExtractCodex429Telemetry(rawMsg json.RawMessage) *Codex429Telemetry {
	if len(rawMsg) == 0 {
		return nil
	}
	var detail queuedUsageDetail
	if err := json.Unmarshal(rawMsg, &detail); err != nil {
		return nil
	}
	// 只有 codex provider 才检查 429
	if strings.ToLower(strings.TrimSpace(detail.Provider)) != "codex" {
		return nil
	}
	if detail.StatusCode != 429 {
		return nil
	}
	if !detail.Failed {
		return nil
	}
	if detail.Error == nil {
		return nil
	}
	errorType := detail.Error.Type
	// 尝试查找嵌套 error 路径：error.error.type / response.error.type
	if errorType == "" && len(detail.Error.Error) > 0 {
		var nested struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(detail.Error.Error, &nested); err == nil && nested.Type != "" {
			errorType = nested.Type
		}
	}
	if errorType != "usage_limit_reached" {
		return nil
	}
	telemetry := &Codex429Telemetry{
		StatusCode:  detail.StatusCode,
		ErrorType:   errorType,
		RequestID:   strings.TrimSpace(detail.RequestID),
		AuthIndex:   strings.TrimSpace(detail.AuthIndex),
		Provider:    strings.TrimSpace(detail.Provider),
		ResetsAt:    detail.Error.ResetsAt,
		ResetsInSec: detail.Error.ResetsInSeconds,
	}
	if detail.Error.RawBody != nil {
		telemetry.RawErrorBody = *detail.Error.RawBody
	}
	return telemetry
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
	statusCode, failCode, failMsg, failBody := extractFailureFields(d.Failed, d.StatusCode, d.Error)
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
		FailureStatusCode:   statusCode,
		FailureCode:         failCode,
		FailureMessage:      failMsg,
		FailureBody:         failBody,
	}
}

// extractFailureFields 从 Redis usage payload 中提取失败详情字段。
// failed==false 时返回空值。
func extractFailureFields(failed bool, statusCode int, errBody *errorTelemetryBody) (sc *int, code string, message string, body string) {
	if !failed {
		return nil, "", "", ""
	}
	if statusCode > 0 {
		sc = &statusCode
	}
	if errBody == nil {
		return sc, "", "", ""
	}
	// 构建 raw body 用于脱敏和存储
	rawBody := ""
	if errBody.RawBody != nil {
		rawBody = *errBody.RawBody
	} else {
		// 尝试序列化 error 结构体作为 body
		if b, err := json.Marshal(errBody); err == nil {
			rawBody = string(b)
		}
	}
	body = sanitizeFailureBody(rawBody)
	code, message = extractFailureCodeAndMessage(body, errBody)
	if message == "" && len(body) > 0 {
		message = truncateFailureText(body, 300)
	}
	return sc, code, message, body
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

// extractFailureCodeAndMessage 从脱敏后的 body 和结构化 error 中提取 code 和 message。
func extractFailureCodeAndMessage(body string, errBody *errorTelemetryBody) (code string, message string) {
	if errBody == nil {
		return tryExtractCodeFromText(body), ""
	}
	// 先尝试从结构化字段获取
	message = strings.TrimSpace(errBody.Message)
	code = strings.TrimSpace(errBody.Type)
	// 尝试从嵌套 error 获取
	if len(errBody.Error) > 0 {
		var nested struct {
			Code    json.RawMessage `json:"code"`
			Type    string          `json:"type"`
			Message string          `json:"message"`
		}
		if err := json.Unmarshal(errBody.Error, &nested); err == nil {
			if nested.Message != "" && message == "" {
				message = strings.TrimSpace(nested.Message)
			}
			if c := rawMessageToString(nested.Code); c != "" && code == "" {
				code = c
			}
			if nested.Type != "" && code == "" {
				code = strings.TrimSpace(nested.Type)
			}
		}
	}
	// 尝试从 body JSON 解析
	if code == "" || message == "" {
		var parsed struct {
			Error struct {
				Code    json.RawMessage `json:"code"`
				Type    string          `json:"type"`
				Message string          `json:"message"`
			} `json:"error"`
			Code    json.RawMessage `json:"code"`
			Message string          `json:"message"`
		}
		if json.Unmarshal([]byte(body), &parsed) == nil {
			if parsed.Error.Message != "" && message == "" {
				message = strings.TrimSpace(parsed.Error.Message)
			}
			if c := rawMessageToString(parsed.Error.Code); c != "" && code == "" {
				code = c
			}
			if parsed.Error.Type != "" && code == "" {
				code = strings.TrimSpace(parsed.Error.Type)
			}
			if parsed.Message != "" && message == "" {
				message = strings.TrimSpace(parsed.Message)
			}
			if c := rawMessageToString(parsed.Code); c != "" && code == "" {
				code = c
			}
		}
	}
	if code == "" {
		code = tryExtractCodeFromText(body)
	}
	return code, message
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
	"invalid_request",
	"not_implemented",
	"unsupported_parameter",
	"model_not_found",
	"insufficient_quota",
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
	// 替换 access_token / refresh_token 值
	body = reTokenValue.ReplaceAllString(body, "${1}[redacted_token]")
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
