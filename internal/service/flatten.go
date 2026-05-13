package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func BuildEventKey(apiGroupKey, model string, timestamp time.Time, source, authIndex string, failed bool, tokens dto.TokenStats) string {
	normalized := normalizeTokens(tokens)
	payload := fmt.Sprintf(
		"%s|%s|%s|%s|%s|%t|%d|%d|%d|%d|%d",
		strings.TrimSpace(apiGroupKey),
		strings.TrimSpace(model),
		timeutil.FormatStorageTime(timestamp),
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

func normalizeTokens(tokens dto.TokenStats) dto.TokenStats {
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
