package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"cpa-usage-keeper/internal/redact"
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
}

type analysisTokenUsage struct {
	Bucket          time.Time `json:"bucket"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	Requests        int64     `json:"requests"`
}

type analysisCompositionItem struct {
	Key         string  `json:"key"`
	Label       string  `json:"label"`
	TotalTokens int64   `json:"total_tokens"`
	Requests    int64   `json:"requests"`
	Percent     float64 `json:"percent"`
}

type analysisHeatmap struct {
	APIKeys []string              `json:"api_keys"`
	Models  []string              `json:"models"`
	Cells   []analysisHeatmapCell `json:"cells"`
}

type analysisHeatmapCell struct {
	APIKey      string  `json:"api_key"`
	Model       string  `json:"model"`
	TotalTokens int64   `json:"total_tokens"`
	Requests    int64   `json:"requests"`
	Intensity   float64 `json:"intensity"`
}

type analysisAPIKeyInfo struct {
	ID    int64
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
		Heatmap:               analysisHeatmap{APIKeys: []string{}, Models: []string{}, Cells: []analysisHeatmapCell{}},
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
		infos[row.APIKey] = analysisAPIKeyInfo{ID: row.ID, Label: cpaAPIKeyDisplayLabel(row)}
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
	}
}

func buildAnalysisCompositionPayload(items []servicedto.AnalysisCompositionItem, apiKeyInfos map[string]analysisAPIKeyInfo) []analysisCompositionItem {
	total := int64(0)
	for _, item := range items {
		total += item.TotalTokens
	}
	payload := make([]analysisCompositionItem, 0, len(items))
	for _, item := range items {
		key := analysisAPIKeyResponseKey(item.Key, apiKeyInfos)
		label := item.Key
		if apiKeyInfos != nil {
			label = analysisAPIKeyLabel(item.Key, apiKeyInfos)
		} else if item.Label != "" {
			label = item.Label
		}
		percent := 0.0
		if total > 0 {
			percent = (float64(item.TotalTokens) / float64(total)) * 100
		}
		payload = append(payload, analysisCompositionItem{Key: key, Label: label, TotalTokens: item.TotalTokens, Requests: item.Requests, Percent: percent})
	}
	return payload
}

func analysisAPIKeyResponseKey(apiKey string, apiKeyInfos map[string]analysisAPIKeyInfo) string {
	if info, ok := apiKeyInfos[apiKey]; ok && info.ID > 0 {
		return strconv.FormatInt(info.ID, 10)
	}
	return redact.APIKeyDisplayName(apiKey)
}

func analysisAPIKeyLabel(apiKey string, apiKeyInfos map[string]analysisAPIKeyInfo) string {
	if info, ok := apiKeyInfos[apiKey]; ok && info.Label != "" {
		return info.Label
	}
	return redact.APIKeyDisplayName(apiKey)
}

func buildAnalysisHeatmapPayload(cells []servicedto.AnalysisHeatmapCell, apiKeyInfos map[string]analysisAPIKeyInfo) analysisHeatmap {
	apiRequests := map[string]int64{}
	modelRequests := map[string]int64{}
	maxTokens := int64(0)
	for _, cell := range cells {
		apiKey := analysisAPIKeyLabel(cell.APIKey, apiKeyInfos)
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
		payloadCells = append(payloadCells, analysisHeatmapCell{
			APIKey:      analysisAPIKeyLabel(cell.APIKey, apiKeyInfos),
			Model:       cell.Model,
			TotalTokens: cell.TotalTokens,
			Requests:    cell.Requests,
			Intensity:   intensity,
		})
	}
	return analysisHeatmap{APIKeys: apiKeys, Models: models, Cells: payloadCells}
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
