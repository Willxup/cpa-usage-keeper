package api

import (
	"crypto/sha256"
	"fmt"
	"sort"

	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type usageOverviewComparisonItem struct {
	Key                 string   `json:"key"`
	Label               string   `json:"label"`
	Requests            int64    `json:"requests"`
	Failures            int64    `json:"failures"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	ReasoningTokens     int64    `json:"reasoning_tokens"`
	TotalTokens         int64    `json:"total_tokens"`
	Cost                *float64 `json:"cost"`
}

type usageOverviewComparisons struct {
	Models  []usageOverviewComparisonItem `json:"models"`
	APIKeys []usageOverviewComparisonItem `json:"api_keys,omitempty"`
}

func buildUsageOverviewComparisons(overview *servicedto.UsageOverviewSnapshot, infos map[string]analysisAPIKeyInfo) *usageOverviewComparisons {
	result := &usageOverviewComparisons{Models: []usageOverviewComparisonItem{}, APIKeys: []usageOverviewComparisonItem{}}
	if overview == nil || overview.Comparisons == nil {
		return result
	}
	result.Models = mapUsageOverviewComparison(overview.Comparisons.Models, nil, false)
	result.APIKeys = mapUsageOverviewComparison(overview.Comparisons.APIKeys, infos, true)
	return result
}

func mapUsageOverviewComparison(items map[string]*repodto.UsageComparisonItemRecord, infos map[string]analysisAPIKeyInfo, apiKeys bool) []usageOverviewComparisonItem {
	result := make([]usageOverviewComparisonItem, 0, len(items))
	for _, item := range items {
		key, label := item.Key, item.Key
		if apiKeys {
			label = analysisAPIKeyLabel(item.Key, infos)
			if info, ok := infos[item.Key]; ok && info.ID != "" {
				key = info.ID
			} else {
				// 已删除的 Key 仍需独立标识；不能以可能相同的脱敏文本合并不同历史 Key。
				key = fmt.Sprintf("legacy:%x", sha256.Sum256([]byte(item.Key)))
			}
		}
		var cost *float64
		if item.CostAvailable {
			value := item.CostUSD
			cost = &value
		}
		result = append(result, usageOverviewComparisonItem{
			Key: key, Label: label, Requests: item.Requests, Failures: item.Failures,
			InputTokens: item.InputTokens, OutputTokens: item.OutputTokens,
			CacheReadTokens: item.CacheReadTokens, CacheCreationTokens: item.CacheCreationTokens,
			ReasoningTokens: item.ReasoningTokens, TotalTokens: item.TotalTokens, Cost: cost,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TotalTokens == result[j].TotalTokens {
			return result[i].Key < result[j].Key
		}
		return result[i].TotalTokens > result[j].TotalTokens
	})
	return result
}
