package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

type analysisResponse struct {
	Granularity           string                    `json:"granularity"`
	Timezone              string                    `json:"timezone"`
	RangeStart            *time.Time                `json:"range_start,omitempty"`
	RangeEnd              *time.Time                `json:"range_end,omitempty"`
	TokenUsage            []analysisTokenUsage      `json:"token_usage"`
	APIKeyComposition     []analysisCompositionItem `json:"api_key_composition"`
	ModelComposition      []analysisCompositionItem `json:"model_composition"`
	AuthFilesComposition  []analysisCompositionItem `json:"auth_files_composition"`
	AIProviderComposition []analysisCompositionItem `json:"ai_provider_composition"`
	Heatmap               analysisHeatmap           `json:"heatmap"`
	CostBreakdown         analysisCostBreakdown     `json:"cost_breakdown"`
	ModelEfficiency       []analysisModelEfficiency `json:"model_efficiency"`
}

type analysisTokenUsage struct {
	Bucket          time.Time `json:"bucket"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	Requests        int64     `json:"requests"`
	CostUSD         float64   `json:"cost_usd"`
	CostAvailable   bool      `json:"cost_available"`
}

type analysisCompositionItem struct {
	Key             string  `json:"key"`
	Label           string  `json:"label"`
	TotalTokens     int64   `json:"total_tokens"`
	Requests        int64   `json:"requests"`
	Percent         float64 `json:"percent"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CachedTokens    int64   `json:"cached_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	CostAvailable   bool    `json:"cost_available"`
}

type analysisHeatmap struct {
	APIKeys      []string              `json:"api_keys"`
	APIKeyLabels map[string]string     `json:"api_key_labels"`
	Models       []string              `json:"models"`
	Cells        []analysisHeatmapCell `json:"cells"`
}

type analysisHeatmapCell struct {
	APIKey          string  `json:"api_key"`
	Model           string  `json:"model"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CachedTokens    int64   `json:"cached_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
	TotalTokens     int64   `json:"total_tokens"`
	Requests        int64   `json:"requests"`
	CostUSD         float64 `json:"cost_usd"`
	CostAvailable   bool    `json:"cost_available"`
	Intensity       float64 `json:"intensity"`
}

type analysisCostBreakdown struct {
	InputCostUSD  float64 `json:"input_cost_usd"`
	OutputCostUSD float64 `json:"output_cost_usd"`
	CachedCostUSD float64 `json:"cached_cost_usd"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
	CostAvailable bool    `json:"cost_available"`
}

type analysisModelEfficiency struct {
	Model                  string  `json:"model"`
	Requests               int64   `json:"requests"`
	InputTokens            int64   `json:"input_tokens"`
	OutputTokens           int64   `json:"output_tokens"`
	CachedTokens           int64   `json:"cached_tokens"`
	ReasoningTokens        int64   `json:"reasoning_tokens"`
	TotalTokens            int64   `json:"total_tokens"`
	CostUSD                float64 `json:"cost_usd"`
	CostAvailable          bool    `json:"cost_available"`
	CostPerRequestUSD      float64 `json:"cost_per_request_usd"`
	OutputTokensPerRequest float64 `json:"output_tokens_per_request"`
	CacheRate              float64 `json:"cache_rate"`
}

type analysisAPIKeyInfo struct {
	ID    string
	Label string
}

func registerUsageAnalysisRoute(router gin.IRoutes, usageProvider service.UsageProvider, cpaAPIKeyProvider service.CPAAPIKeyProvider) {
	router.GET("/usage/analysis", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, emptyAnalysisResponse())
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		analysis, err := usageProvider.GetAnalysis(c.Request.Context(), filter)
		if err != nil {
			writeInternalError(c, "get analysis failed", err)
			return
		}
		apiKeyInfos, err := loadCPAAPIKeyInfos(c, cpaAPIKeyProvider)
		if err != nil {
			return
		}

		c.JSON(http.StatusOK, buildAnalysisPayload(analysis, apiKeyInfos))
	})
}

func emptyAnalysisResponse() analysisResponse {
	return analysisResponse{
		Granularity:           string(servicedto.AnalysisGranularityHourly),
		Timezone:              time.Local.String(),
		TokenUsage:            []analysisTokenUsage{},
		APIKeyComposition:     []analysisCompositionItem{},
		ModelComposition:      []analysisCompositionItem{},
		AuthFilesComposition:  []analysisCompositionItem{},
		AIProviderComposition: []analysisCompositionItem{},
		Heatmap:               analysisHeatmap{APIKeys: []string{}, APIKeyLabels: map[string]string{}, Models: []string{}, Cells: []analysisHeatmapCell{}},
		CostBreakdown:         analysisCostBreakdown{CostAvailable: true},
		ModelEfficiency:       []analysisModelEfficiency{},
	}
}

func loadCPAAPIKeyInfos(c *gin.Context, provider service.CPAAPIKeyProvider) (map[string]analysisAPIKeyInfo, error) {
	if provider == nil {
		return map[string]analysisAPIKeyInfo{}, nil
	}
	rows, err := provider.ListCPAAPIKeys(c.Request.Context())
	if err != nil {
		writeInternalError(c, "list api key options failed", err)
		return nil, err
	}
	infos := make(map[string]analysisAPIKeyInfo, len(rows))
	for _, row := range rows {
		infos[row.APIKey] = analysisAPIKeyInfo{
			ID:    strconv.FormatInt(row.ID, 10),
			Label: helper.CPAAPIKeyDisplayName(row),
		}
	}
	return infos, nil
}

func buildAnalysisPayload(snapshot *servicedto.AnalysisSnapshot, apiKeyInfos map[string]analysisAPIKeyInfo) analysisResponse {
	if snapshot == nil {
		return emptyAnalysisResponse()
	}
	tokenUsage := make([]analysisTokenUsage, 0, len(snapshot.TokenUsage))
	for _, bucket := range snapshot.TokenUsage {
		tokenUsage = append(tokenUsage, analysisTokenUsage{
			Bucket:          bucket.Bucket,
			InputTokens:     bucket.InputTokens,
			OutputTokens:    bucket.OutputTokens,
			CachedTokens:    bucket.CachedTokens,
			ReasoningTokens: bucket.ReasoningTokens,
			TotalTokens:     bucket.TotalTokens,
			Requests:        bucket.Requests,
			CostUSD:         bucket.CostUSD,
			CostAvailable:   bucket.CostAvailable,
		})
	}
	apiComposition := buildAnalysisCompositionPayload(snapshot.APIKeyComposition, apiKeyInfos)
	modelComposition := buildAnalysisCompositionPayload(snapshot.ModelComposition, nil)
	authFilesComposition := buildAnalysisCompositionPayload(snapshot.AuthFilesComposition, nil)
	aiProviderComposition := buildAnalysisCompositionPayload(snapshot.AIProviderComposition, nil)
	return analysisResponse{
		Granularity:           string(snapshot.Granularity),
		Timezone:              time.Local.String(),
		RangeStart:            snapshot.RangeStart,
		RangeEnd:              snapshot.RangeEnd,
		TokenUsage:            tokenUsage,
		APIKeyComposition:     apiComposition,
		ModelComposition:      modelComposition,
		AuthFilesComposition:  authFilesComposition,
		AIProviderComposition: aiProviderComposition,
		Heatmap:               buildAnalysisHeatmapPayload(snapshot.Heatmap, apiKeyInfos),
		CostBreakdown: analysisCostBreakdown{
			InputCostUSD:  snapshot.CostBreakdown.InputCostUSD,
			OutputCostUSD: snapshot.CostBreakdown.OutputCostUSD,
			CachedCostUSD: snapshot.CostBreakdown.CachedCostUSD,
			TotalCostUSD:  snapshot.CostBreakdown.TotalCostUSD,
			CostAvailable: snapshot.CostBreakdown.CostAvailable,
		},
		ModelEfficiency: buildAnalysisModelEfficiencyPayload(snapshot.ModelEfficiency),
	}
}

func buildAnalysisCompositionPayload(items []servicedto.AnalysisCompositionItem, apiKeyInfos map[string]analysisAPIKeyInfo) []analysisCompositionItem {
	total := int64(0)
	for _, item := range items {
		total += item.TotalTokens
	}
	payload := make([]analysisCompositionItem, 0, len(items))
	for _, item := range items {
		key := helper.RedactSensitiveValue(item.Key)
		label := item.Key
		if apiKeyInfos != nil {
			key = analysisAPIKeyResponseKey(item.Key, apiKeyInfos)
			label = analysisAPIKeyLabel(item.Key, apiKeyInfos)
		} else if item.Label != "" {
			label = item.Label
		}
		percent := 0.0
		if total > 0 {
			percent = (float64(item.TotalTokens) / float64(total)) * 100
		}
		payload = append(payload, analysisCompositionItem{
			Key:             key,
			Label:           label,
			TotalTokens:     item.TotalTokens,
			Requests:        item.Requests,
			Percent:         percent,
			InputTokens:     item.InputTokens,
			OutputTokens:    item.OutputTokens,
			CachedTokens:    item.CachedTokens,
			ReasoningTokens: item.ReasoningTokens,
			CostUSD:         item.CostUSD,
			CostAvailable:   item.CostAvailable,
		})
	}
	return payload
}

func analysisAPIKeyResponseKey(apiKey string, apiKeyInfos map[string]analysisAPIKeyInfo) string {
	// Analysis 的结构标识使用 CPA API Key id，展示文案独立走别名/脱敏 key，避免脱敏值碰撞。
	if info, ok := apiKeyInfos[apiKey]; ok && info.ID != "" {
		return info.ID
	}
	return helper.RedactSensitiveValue(apiKey)
}

func analysisAPIKeyLabel(apiKey string, apiKeyInfos map[string]analysisAPIKeyInfo) string {
	if info, ok := apiKeyInfos[apiKey]; ok && info.Label != "" {
		return info.Label
	}
	return helper.RedactSensitiveValue(apiKey)
}

func buildAnalysisHeatmapPayload(cells []servicedto.AnalysisHeatmapCell, apiKeyInfos map[string]analysisAPIKeyInfo) analysisHeatmap {
	apiRequests := map[string]int64{}
	apiKeyLabels := map[string]string{}
	modelRequests := map[string]int64{}
	maxTokens := int64(0)
	for _, cell := range cells {
		apiKey := analysisAPIKeyResponseKey(cell.APIKey, apiKeyInfos)
		apiKeyLabels[apiKey] = analysisAPIKeyLabel(cell.APIKey, apiKeyInfos)
		apiRequests[apiKey] += cell.Requests
		modelRequests[cell.Model] += cell.Requests
		if cell.TotalTokens > maxTokens {
			maxTokens = cell.TotalTokens
		}
	}
	apiKeys := sortedHeatmapKeysByRequests(apiRequests)
	models := sortedHeatmapKeysByRequests(modelRequests)
	payloadCells := make([]analysisHeatmapCell, 0, len(cells))
	for _, cell := range cells {
		intensity := 0.0
		if maxTokens > 0 {
			intensity = float64(cell.TotalTokens) / float64(maxTokens)
		}
		apiKey := analysisAPIKeyResponseKey(cell.APIKey, apiKeyInfos)
		payloadCells = append(payloadCells, analysisHeatmapCell{
			APIKey:          apiKey,
			Model:           cell.Model,
			InputTokens:     cell.InputTokens,
			OutputTokens:    cell.OutputTokens,
			CachedTokens:    cell.CachedTokens,
			ReasoningTokens: cell.ReasoningTokens,
			TotalTokens:     cell.TotalTokens,
			Requests:        cell.Requests,
			CostUSD:         cell.CostUSD,
			CostAvailable:   cell.CostAvailable,
			Intensity:       intensity,
		})
	}
	return analysisHeatmap{APIKeys: apiKeys, APIKeyLabels: apiKeyLabels, Models: models, Cells: payloadCells}
}

func buildAnalysisModelEfficiencyPayload(items []servicedto.AnalysisModelEfficiencyItem) []analysisModelEfficiency {
	payload := make([]analysisModelEfficiency, 0, len(items))
	for _, item := range items {
		payload = append(payload, analysisModelEfficiency{
			Model:                  item.Model,
			Requests:               item.Requests,
			InputTokens:            item.InputTokens,
			OutputTokens:           item.OutputTokens,
			CachedTokens:           item.CachedTokens,
			ReasoningTokens:        item.ReasoningTokens,
			TotalTokens:            item.TotalTokens,
			CostUSD:                item.CostUSD,
			CostAvailable:          item.CostAvailable,
			CostPerRequestUSD:      item.CostPerRequestUSD,
			OutputTokensPerRequest: item.OutputTokensPerRequest,
			CacheRate:              item.CacheRate,
		})
	}
	return payload
}

func sortedHeatmapKeysByRequests(requestsByKey map[string]int64) []string {
	keys := make([]string, 0, len(requestsByKey))
	for key := range requestsByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if requestsByKey[keys[i]] == requestsByKey[keys[j]] {
			return keys[i] < keys[j]
		}
		return requestsByKey[keys[i]] > requestsByKey[keys[j]]
	})
	return keys
}
