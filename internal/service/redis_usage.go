package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
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
	Timestamp       time.Time        `json:"timestamp"`
	LatencyMS       int64            `json:"latency_ms"`
	TTFTMS          int64            `json:"ttft_ms"`
	Source          string           `json:"source"`
	AuthIndex       string           `json:"auth_index"`
	Tokens          dto.TokenStats   `json:"tokens"`
	Failed          bool             `json:"failed"`
	Fail            queuedFailDetail `json:"fail"`
	Provider        string           `json:"provider"`
	Model           string           `json:"model"`
	Alias           *string          `json:"alias"`
	ReasoningEffort string           `json:"reasoning_effort"`
	ServiceTier     string           `json:"service_tier"`
	Endpoint        string           `json:"endpoint"`
	AuthType        string           `json:"auth_type"`
	APIKey          string           `json:"api_key"`
	RequestID       string           `json:"request_id"`
}

// queuedFailDetail 对应 CPA payload 中的 fail 块，keeper 仅取失败状态码。
type queuedFailDetail struct {
	StatusCode int `json:"status_code"`
}

func normalizeRedisAuthType(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "api_key" {
		return "apikey"
	}
	return trimmed
}

// normalizeFailStatusCode 防御负数 HTTP 状态码，缺失时归零。
func normalizeFailStatusCode(code int) int {
	if code < 0 {
		return 0
	}
	return code
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
	tokens := normalizeTokens(d.Tokens)
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
		Timestamp:           timestamp,
		Source:              source,
		AuthIndex:           authIndex,
		Failed:              d.Failed,
		FailStatusCode:      normalizeFailStatusCode(d.Fail.StatusCode),
		LatencyMS:           max(d.LatencyMS, 0),
		TTFTMS:              max(d.TTFTMS, 0),
		InputTokens:         tokens.InputTokens,
		OutputTokens:        tokens.OutputTokens,
		ReasoningTokens:     tokens.ReasoningTokens,
		CachedTokens:        tokens.CachedTokens,
		CacheReadTokens:     tokens.CacheReadTokens,
		CacheCreationTokens: tokens.CacheCreationTokens,
		TotalTokens:         tokens.TotalTokens,
	}
}
