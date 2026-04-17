package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
)

func FlattenUsageExport(snapshotRunID uint, export cpa.UsageExport) []models.UsageEvent {
	if len(export.Usage.APIs) == 0 {
		return nil
	}

	events := make([]models.UsageEvent, 0)
	for apiGroupKey, apiSnapshot := range export.Usage.APIs {
		apiGroupKey = strings.TrimSpace(apiGroupKey)
		if apiGroupKey == "" {
			continue
		}

		for modelName, modelSnapshot := range apiSnapshot.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				modelName = "unknown"
			}

			for _, detail := range modelSnapshot.Details {
				tokens := normalizeTokens(detail.Tokens)
				timestamp := detail.Timestamp.UTC()
				if timestamp.IsZero() {
					timestamp = export.ExportedAt.UTC()
				}

				events = append(events, models.UsageEvent{
					SnapshotRunID:   snapshotRunID,
					EventKey:        BuildEventKey(apiGroupKey, modelName, timestamp, detail.Source, detail.AuthIndex, detail.Failed, tokens),
					APIGroupKey:     apiGroupKey,
					Model:           modelName,
					Timestamp:       timestamp,
					Source:          strings.TrimSpace(detail.Source),
					AuthIndex:       strings.TrimSpace(detail.AuthIndex),
					Failed:          detail.Failed,
					LatencyMS:       max(detail.LatencyMS, 0),
					InputTokens:     tokens.InputTokens,
					OutputTokens:    tokens.OutputTokens,
					ReasoningTokens: tokens.ReasoningTokens,
					CachedTokens:    tokens.CachedTokens,
					TotalTokens:     tokens.TotalTokens,
				})
			}
		}
	}

	return events
}

func BuildEventKey(apiGroupKey, model string, timestamp time.Time, source, authIndex string, failed bool, tokens cpa.TokenStats) string {
	normalized := normalizeTokens(tokens)
	payload := fmt.Sprintf(
		"%s|%s|%s|%s|%s|%t|%d|%d|%d|%d|%d",
		strings.TrimSpace(apiGroupKey),
		strings.TrimSpace(model),
		timestamp.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(source),
		strings.TrimSpace(authIndex),
		failed,
		normalized.InputTokens,
		normalized.OutputTokens,
		normalized.ReasoningTokens,
		normalized.CachedTokens,
		normalized.TotalTokens,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func normalizeTokens(tokens cpa.TokenStats) cpa.TokenStats {
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CachedTokens
	}
	return tokens
}

func max(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}
