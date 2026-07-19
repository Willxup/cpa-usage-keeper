package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

// DecodeRedisUsageMessage 将 redis_inboxes.raw_message 原样解码为 usage_events 入库实体。
func DecodeRedisUsageMessage(message string, fetchedAt time.Time) (entities.UsageEvent, json.RawMessage, error) {
	event, raw, _, err := DecodeRedisUsageMessageWithHeaders(message, fetchedAt)
	return event, raw, err
}

// DecodeRedisUsageMessageWithHeaders 解码 usage event，并在 OAuth 响应携带 headers 时抽取 quota cache 更新快照。
func DecodeRedisUsageMessageWithHeaders(message string, fetchedAt time.Time) (entities.UsageEvent, json.RawMessage, *quota.UsageHeaderSnapshot, error) {
	raw := json.RawMessage(message)
	var payload queuedUsageDetail
	if err := json.Unmarshal(raw, &payload); err != nil {
		return entities.UsageEvent{}, nil, nil, fmt.Errorf("decode redis usage message: %w", err)
	}
	if strings.TrimSpace(payload.RequestID) == "" {
		return entities.UsageEvent{}, raw, nil, fmt.Errorf("decode redis usage message: request_id is required")
	}
	event := payload.toUsageEvent(fetchedAt)
	return event, raw, payload.toUsageHeaderSnapshot(event), nil
}

// queuedUsageDetail 对应 CPA Redis 队列中的单条 usage JSON payload。
type queuedUsageDetail struct {
	Timestamp           time.Time       `json:"timestamp"`
	LatencyMS           int64           `json:"latency_ms"`
	TTFTMS              *int64          `json:"ttft_ms"`
	Source              string          `json:"source"`
	AuthIndex           string          `json:"auth_index"`
	Tokens              dto.TokenStats  `json:"tokens"`
	Failed              bool            `json:"failed"`
	Generate            *bool           `json:"generate"`
	Provider            string          `json:"provider"`
	Model               string          `json:"model"`
	Alias               *string         `json:"alias"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	ServiceTier         string          `json:"service_tier"`
	ResponseServiceTier string          `json:"response_service_tier"`
	ExecutorType        string          `json:"executor_type"`
	Endpoint            string          `json:"endpoint"`
	AuthType            string          `json:"auth_type"`
	APIKey              string          `json:"api_key"`
	RequestID           string          `json:"request_id"`
	ResponseHeaders     json.RawMessage `json:"response_headers"`
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

func normalizeRedisGenerate(value *bool, failed bool, executorType string, tokens dto.TokenStats) *bool {
	if value != nil {
		return value
	}
	generate := true
	if !failed && strings.TrimSpace(executorType) == "CodexWebsocketsExecutor" && redisTokenStatsAllZero(tokens) {
		// 旧版 CPA 没有 generate；只把成功的 WebSocket 全零 token 消息兼容为预热。
		generate = false
	}
	return &generate
}

func redisTokenStatsAllZero(tokens dto.TokenStats) bool {
	return tokens.InputTokens == 0 &&
		tokens.OutputTokens == 0 &&
		tokens.ReasoningTokens == 0 &&
		tokens.CachedTokens == 0 &&
		tokens.CacheReadTokens == 0 &&
		tokens.CacheCreationTokens == 0 &&
		tokens.TotalTokens == 0
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
		ResponseServiceTier: strings.TrimSpace(d.ResponseServiceTier),
		ExecutorType:        strings.TrimSpace(d.ExecutorType),
		Timestamp:           timestamp,
		Source:              source,
		AuthIndex:           authIndex,
		Failed:              d.Failed,
		Generate:            normalizeRedisGenerate(d.Generate, d.Failed, d.ExecutorType, d.Tokens),
		LatencyMS:           max(d.LatencyMS, 0),
		TTFTMS:              d.TTFTMS,
		InputTokens:         d.Tokens.InputTokens,
		OutputTokens:        d.Tokens.OutputTokens,
		ReasoningTokens:     d.Tokens.ReasoningTokens,
		CachedTokens:        d.Tokens.CachedTokens,
		CacheReadTokens:     d.Tokens.CacheReadTokens,
		CacheReadPresent:    d.Tokens.CacheReadPresent,
		CacheCreationTokens: d.Tokens.CacheCreationTokens,
		TotalTokens:         d.Tokens.TotalTokens,
	}
}

func (d queuedUsageDetail) toUsageHeaderSnapshot(event entities.UsageEvent) *quota.UsageHeaderSnapshot {
	headers, ok := decodeRedisUsageResponseHeaders(d.ResponseHeaders)
	if !ok {
		return nil
	}
	snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{
		AuthType:   event.AuthType,
		AuthIndex:  event.AuthIndex,
		Provider:   event.Provider,
		ObservedAt: event.Timestamp,
		Headers:    headers,
	})
	if !ok {
		return nil
	}
	return snapshot
}

func decodeRedisUsageResponseHeaders(raw json.RawMessage) (http.Header, bool) {
	if rawJSONMessageIsEmptyOrNull(raw) {
		return nil, false
	}
	var headers http.Header
	if err := json.Unmarshal(raw, &headers); err == nil && len(headers) > 0 {
		return headers, true
	}
	var rawHeaders map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawHeaders); err != nil || len(rawHeaders) == 0 {
		return nil, false
	}
	headers = make(http.Header, len(rawHeaders))
	for key, rawValue := range rawHeaders {
		for _, value := range decodeRedisUsageHeaderValues(rawValue) {
			headers.Add(key, value)
		}
	}
	if len(headers) == 0 {
		return nil, false
	}
	return headers, true
}

func rawJSONMessageIsEmptyOrNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || (len(trimmed) == 4 && trimmed[0] == 'n' && trimmed[1] == 'u' && trimmed[2] == 'l' && trimmed[3] == 'l')
}

func decodeRedisUsageHeaderValues(raw json.RawMessage) []string {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return []string{value}
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return values
	}
	var rawValues []json.RawMessage
	if err := json.Unmarshal(raw, &rawValues); err != nil {
		return nil
	}
	values = make([]string, 0, len(rawValues))
	for _, rawValue := range rawValues {
		if err := json.Unmarshal(rawValue, &value); err == nil {
			values = append(values, value)
		}
	}
	return values
}
