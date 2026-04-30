package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
)

type UsageFetchResult struct {
	HTTPStatus int
	RawPayload []byte
	ExportedAt *time.Time
	Version    string
	Events     []models.UsageEvent
}

type RedisQueue interface {
	PopUsage(ctx context.Context) ([]string, error)
}

type redisUsageFetcher struct {
	queue RedisQueue
}

func newRedisUsageFetcher(queue RedisQueue) redisUsageFetcher {
	return redisUsageFetcher{queue: queue}
}

func (f redisUsageFetcher) FetchUsage(ctx context.Context, fetchedAt time.Time) (*UsageFetchResult, error) {
	if f.queue == nil {
		return nil, fmt.Errorf("redis usage queue is nil")
	}
	messages, err := f.queue.PopUsage(ctx)
	if err != nil {
		return nil, err
	}
	rawMessages := make([]json.RawMessage, 0, len(messages))
	events := make([]models.UsageEvent, 0, len(messages))
	for i, message := range messages {
		event, raw, err := DecodeRedisUsageMessage(message, fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("decode redis usage message %d: %w", i, err)
		}
		rawMessages = append(rawMessages, raw)
		events = append(events, event)
	}
	rawPayload, err := json.Marshal(rawMessages)
	if err != nil {
		return nil, fmt.Errorf("encode redis usage batch: %w", err)
	}
	return &UsageFetchResult{
		RawPayload: rawPayload,
		Events:     events,
	}, nil
}

func DecodeRedisUsageMessage(message string, fetchedAt time.Time) (models.UsageEvent, json.RawMessage, error) {
	raw := json.RawMessage(message)
	var payload queuedUsageDetail
	if err := json.Unmarshal(raw, &payload); err != nil {
		return models.UsageEvent{}, nil, fmt.Errorf("decode redis usage message: %w", err)
	}
	return payload.toUsageEvent(fetchedAt), raw, nil
}

type queuedUsageDetail struct {
	Timestamp time.Time      `json:"timestamp"`
	LatencyMS int64          `json:"latency_ms"`
	Source    string         `json:"source"`
	AuthIndex string         `json:"auth_index"`
	Tokens    cpa.TokenStats `json:"tokens"`
	Failed    bool           `json:"failed"`
	Provider  string         `json:"provider"`
	Model     string         `json:"model"`
	Endpoint  string         `json:"endpoint"`
	AuthType  string         `json:"auth_type"`
	APIKey    string         `json:"api_key"`
	RequestID string         `json:"request_id"`
}

func (d queuedUsageDetail) toUsageEvent(fetchedAt time.Time) models.UsageEvent {
	tokens := normalizeTokens(d.Tokens)
	apiGroupKey := firstNonEmpty(d.APIKey, d.Provider, d.Endpoint, "unknown")
	model := firstNonEmpty(d.Model, "unknown")
	timestamp := d.Timestamp.UTC()
	if timestamp.IsZero() {
		timestamp = fetchedAt.UTC()
	}
	source := strings.TrimSpace(d.Source)
	authIndex := strings.TrimSpace(d.AuthIndex)
	eventKey := strings.TrimSpace(d.RequestID)
	if eventKey == "" {
		eventKey = BuildEventKey(apiGroupKey, model, timestamp, source, authIndex, d.Failed, tokens)
	}
	return models.UsageEvent{
		EventKey:        eventKey,
		APIGroupKey:     apiGroupKey,
		Model:           model,
		Timestamp:       timestamp,
		Source:          source,
		AuthIndex:       authIndex,
		Failed:          d.Failed,
		LatencyMS:       max(d.LatencyMS, 0),
		InputTokens:     tokens.InputTokens,
		OutputTokens:    tokens.OutputTokens,
		ReasoningTokens: tokens.ReasoningTokens,
		CachedTokens:    tokens.CachedTokens,
		TotalTokens:     tokens.TotalTokens,
	}
}
