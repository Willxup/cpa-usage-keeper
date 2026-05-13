package service

import "cpa-usage-keeper/internal/repository/dto"

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
