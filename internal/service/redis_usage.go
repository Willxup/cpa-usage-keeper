package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

const (
	failureBodyMaxLength    = 4000
	failureMessageMaxLength = 300
)

var (
	failureAuthorizationPattern = regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+[^\s"']+`)
	failureKeyPattern           = regexp.MustCompile(`sk-[A-Za-z0-9_-]+`)
	failureURLPattern           = regexp.MustCompile(`(?i)https?://[^\s"']+`)
	failureWhitespacePattern    = regexp.MustCompile(`\s+`)
	failureCodeTokenPattern     = regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z0-9]+)+`)
)

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
	Fail            *queuedUsageFailure `json:"fail"`
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
	statusCode, code, message, body := extractFailureFields(d.Failed, d.Fail)
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
		FailureCode:         code,
		FailureMessage:      message,
		FailureBody:         body,
	}
}

type queuedUsageFailure struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

// extractFailureFields 从失败 payload 中提取状态码、错误码、错误信息与脱敏后的错误详情。
// 成功事件或缺失 fail payload 时全部返回空值，让上层与 API 的 omitempty 生效。
func extractFailureFields(failed bool, fail *queuedUsageFailure) (statusCode *int, code string, message string, body string) {
	if !failed || fail == nil {
		return nil, "", "", ""
	}
	if fail.StatusCode > 0 {
		code := fail.StatusCode
		statusCode = &code
	}
	body = sanitizeFailureBody(fail.Body)
	code, message = extractFailureCodeAndMessage(body)
	if message == "" && body != "" {
		message = truncateFailureText(body, failureMessageMaxLength)
	}
	return statusCode, code, message, body
}

// sanitizeFailureBody 在写库前移除密钥、Authorization 头与 URL 等敏感信息，并合并空白、限制长度。
func sanitizeFailureBody(body string) string {
	sanitized := strings.TrimSpace(body)
	if sanitized == "" {
		return ""
	}
	sanitized = failureAuthorizationPattern.ReplaceAllString(sanitized, "[redacted_authorization]")
	sanitized = failureKeyPattern.ReplaceAllString(sanitized, "[redacted_key]")
	sanitized = failureURLPattern.ReplaceAllString(sanitized, "[redacted_url]")
	sanitized = failureWhitespacePattern.ReplaceAllString(sanitized, " ")
	sanitized = strings.TrimSpace(sanitized)
	return truncateFailureText(sanitized, failureBodyMaxLength)
}

// truncateFailureText 以字符（rune）为单位截断，避免在多字节字符中间切断。
func truncateFailureText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// extractFailureCodeAndMessage 优先按 JSON 错误结构解析 code/message，失败则退化为文本里的错误码 token。
func extractFailureCodeAndMessage(body string) (code string, message string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", ""
	}
	if strings.HasPrefix(trimmed, "{") {
		var payload struct {
			Error *struct {
				Code    json.RawMessage `json:"code"`
				Message string          `json:"message"`
				Type    string          `json:"type"`
			} `json:"error"`
			Code    json.RawMessage `json:"code"`
			Message string          `json:"message"`
		}
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			if payload.Error != nil {
				code = rawJSONToString(payload.Error.Code)
				message = strings.TrimSpace(payload.Error.Message)
				if code == "" {
					code = strings.TrimSpace(payload.Error.Type)
				}
			}
			if code == "" {
				code = rawJSONToString(payload.Code)
			}
			if message == "" {
				message = strings.TrimSpace(payload.Message)
			}
			if code != "" || message != "" {
				return code, message
			}
		}
	}
	return failureCodeTokenPattern.FindString(trimmed), ""
}

// rawJSONToString 把 code 字段解释为字符串，兼容 JSON 字符串与数字两种类型。
func rawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(strings.Trim(string(raw), `"`))
}
