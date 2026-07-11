package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"cpa-usage-keeper/internal/entities"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

const (
	pricingSyncMetadataSource = "Models.dev"
	pricingSyncAPIURL         = "https://models.dev/api.json"
)

var pricingSyncHTTPClient = &http.Client{Timeout: 12 * time.Second}

type modelsDevProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Family      string        `json:"family"`
	LastUpdated string        `json:"last_updated"`
	Status      string        `json:"status"`
	Cost        modelsDevCost `json:"cost"`
}

type modelsDevCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type pricingCatalogEntry struct {
	providerID   string
	providerName string
	model        modelsDevModel
}

type pricingCatalogIndex struct {
	exact      map[string][]pricingCatalogEntry
	normalized map[string][]pricingCatalogEntry
}

type pricingSyncCandidate struct {
	entry     pricingCatalogEntry
	matchType string
	score     int
}

func (s *pricingService) PreviewPricingSync(ctx context.Context) (servicedto.PricingSyncPreview, error) {
	models, err := s.effectiveModels(ctx)
	if err != nil {
		return servicedto.PricingSyncPreview{}, err
	}
	catalog, err := fetchModelsDevCatalog(ctx, pricingSyncAPIURL)
	if err != nil {
		return servicedto.PricingSyncPreview{}, err
	}
	return buildPricingSyncPreviewFromCatalog(models, catalog, pricingSyncAPIURL)
}

func fetchModelsDevCatalog(ctx context.Context, catalogURL string) (map[string]modelsDevProvider, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build pricing catalog request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := pricingSyncHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch pricing catalog: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch pricing catalog: unexpected status %d", response.StatusCode)
	}

	var catalog map[string]modelsDevProvider
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<20))
	if err := decoder.Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode pricing catalog: %w", err)
	}
	if catalog == nil {
		catalog = map[string]modelsDevProvider{}
	}
	return catalog, nil
}

func buildPricingSyncPreviewFromCatalog(
	models []string,
	catalog map[string]modelsDevProvider,
	sourceURL string,
) (servicedto.PricingSyncPreview, error) {
	entries := flattenModelsDevCatalog(catalog)
	index := buildPricingCatalogIndex(entries)
	matches := make([]servicedto.PricingSyncMatch, 0, len(models))
	unmatched := make([]string, 0)
	seenModels := make(map[string]struct{}, len(models))

	for _, rawModel := range models {
		model := strings.TrimSpace(rawModel)
		if model == "" {
			continue
		}
		if _, ok := seenModels[model]; ok {
			continue
		}
		seenModels[model] = struct{}{}

		candidates := matchPricingCatalogCandidates(model, index)
		if len(candidates) == 0 {
			unmatched = append(unmatched, model)
			continue
		}

		match, ok := buildPricingSyncMatchFromCandidates(model, candidates)
		if !ok {
			unmatched = append(unmatched, model)
			continue
		}
		matches = append(matches, match)
	}

	sort.Slice(matches, func(left, right int) bool {
		return matches[left].Model < matches[right].Model
	})
	sort.Strings(unmatched)

	return servicedto.PricingSyncPreview{
		Source:          pricingSyncMetadataSource,
		SourceURL:       sourceURL,
		MetadataModels:  len(entries),
		Matches:         matches,
		UnmatchedModels: unmatched,
	}, nil
}

func flattenModelsDevCatalog(catalog map[string]modelsDevProvider) []pricingCatalogEntry {
	providerIDs := make([]string, 0, len(catalog))
	for providerID := range catalog {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)

	entries := make([]pricingCatalogEntry, 0)
	for _, providerKey := range providerIDs {
		provider := catalog[providerKey]
		providerID := strings.TrimSpace(provider.ID)
		if providerID == "" {
			providerID = strings.TrimSpace(providerKey)
		}
		if providerID == "" {
			continue
		}
		providerName := strings.TrimSpace(provider.Name)
		if providerName == "" {
			providerName = providerID
		}

		modelIDs := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)

		for _, modelKey := range modelIDs {
			model := provider.Models[modelKey]
			if strings.TrimSpace(model.ID) == "" {
				model.ID = strings.TrimSpace(modelKey)
			}
			if strings.TrimSpace(model.ID) == "" && strings.TrimSpace(model.Name) == "" {
				continue
			}
			entries = append(entries, pricingCatalogEntry{
				providerID:   providerID,
				providerName: providerName,
				model:        model,
			})
		}
	}

	return entries
}

func buildPricingSyncMatchFromCandidates(model string, candidates []pricingSyncCandidate) (servicedto.PricingSyncMatch, bool) {
	for _, candidate := range candidates {
		match, ok := buildPricingSyncMatch(
			model,
			candidate.entry.model,
			candidate.matchType,
			candidate.entry.providerID,
			candidate.entry.providerName,
		)
		if ok {
			return match, true
		}
	}
	return servicedto.PricingSyncMatch{}, false
}

func buildPricingCatalogIndex(entries []pricingCatalogEntry) pricingCatalogIndex {
	index := pricingCatalogIndex{
		exact:      make(map[string][]pricingCatalogEntry, len(entries)*3),
		normalized: make(map[string][]pricingCatalogEntry, len(entries)*3),
	}
	for _, entry := range entries {
		model := entry.model
		if strings.TrimSpace(model.ID) == "" && strings.TrimSpace(model.Name) == "" {
			continue
		}
		registerPricingCatalogIndexValue(index.exact, model.ID, entry)
		registerPricingCatalogIndexValue(index.exact, model.Name, entry)
		registerPricingCatalogIndexValue(index.exact, stripPricingModelPrefix(model.ID), entry)
		registerPricingCatalogIndexValue(index.exact, stripPricingModelPrefix(model.Name), entry)
		registerPricingCatalogIndexValue(index.normalized, normalizePricingModelKey(model.ID), entry)
		registerPricingCatalogIndexValue(index.normalized, normalizePricingModelKey(model.Name), entry)
		registerPricingCatalogIndexValue(index.normalized, normalizePricingModelKey(stripPricingModelPrefix(model.ID)), entry)
		registerPricingCatalogIndexValue(index.normalized, normalizePricingModelKey(stripPricingModelPrefix(model.Name)), entry)
	}
	return index
}

func registerPricingCatalogIndexValue(target map[string][]pricingCatalogEntry, value string, entry pricingCatalogEntry) {
	key := strings.ToLower(strings.TrimSpace(value))
	if key == "" {
		return
	}
	target[key] = append(target[key], entry)
}

func matchPricingCatalogCandidates(model string, index pricingCatalogIndex) []pricingSyncCandidate {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}

	var candidates []pricingSyncCandidate
	add := func(entries []pricingCatalogEntry, matchType string, score int) {
		for _, entry := range entries {
			candidates = append(candidates, pricingSyncCandidate{
				entry:     entry,
				matchType: matchType,
				score:     score,
			})
		}
	}

	suffix := stripPricingModelPrefix(model)
	if suffix != model {
		// CPA 前缀可由用户自由配置，价格匹配先使用去前缀后的真实模型 ID；
		// 完整 ID 仅作为低优先级兜底，不能用于推断 Models.dev 供应商。
		add(index.exact[strings.ToLower(suffix)], "index_suffix", 100)
		add(index.normalized[normalizePricingModelKey(suffix)], "index_normalized_suffix", 96)
		add(index.exact[strings.ToLower(model)], "index_exact", 92)
		add(index.normalized[normalizePricingModelKey(model)], "index_normalized", 90)
	} else {
		add(index.exact[strings.ToLower(model)], "index_exact", 100)
		add(index.normalized[normalizePricingModelKey(model)], "index_normalized", 92)
	}

	return sortedUniquePricingCandidates(model, candidates)
}

func sortedUniquePricingCandidates(model string, candidates []pricingSyncCandidate) []pricingSyncCandidate {
	unique := make([]pricingSyncCandidate, 0, len(candidates))
	bestByKey := make(map[string]pricingSyncCandidate, len(candidates))
	for _, candidate := range candidates {
		key := strings.TrimSpace(candidate.entry.providerID) + "\x00" + strings.TrimSpace(candidate.entry.model.ID)
		if key == "\x00" {
			continue
		}
		if existing, ok := bestByKey[key]; !ok || pricingCandidateLess(model, candidate, existing) {
			bestByKey[key] = candidate
		}
	}
	for _, candidate := range bestByKey {
		unique = append(unique, candidate)
	}
	sort.Slice(unique, func(left, right int) bool {
		return pricingCandidateLess(model, unique[left], unique[right])
	})
	return unique
}

func pricingCandidateLess(model string, left, right pricingSyncCandidate) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	leftPlanZero := isPlanZeroPricingCandidate(left)
	rightPlanZero := isPlanZeroPricingCandidate(right)
	if leftPlanZero != rightPlanZero {
		return !leftPlanZero
	}
	leftRank := pricingProviderRankForModel(model, left.entry.providerID)
	rightRank := pricingProviderRankForModel(model, right.entry.providerID)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	leftDeprecated := isDeprecatedPricingModel(left.entry.model)
	rightDeprecated := isDeprecatedPricingModel(right.entry.model)
	if leftDeprecated != rightDeprecated {
		return !leftDeprecated
	}
	if left.entry.model.LastUpdated != right.entry.model.LastUpdated {
		return left.entry.model.LastUpdated > right.entry.model.LastUpdated
	}
	if left.entry.providerID != right.entry.providerID {
		return left.entry.providerID < right.entry.providerID
	}
	return left.entry.model.ID < right.entry.model.ID
}

func isDeprecatedPricingModel(model modelsDevModel) bool {
	return strings.EqualFold(strings.TrimSpace(model.Status), "deprecated")
}

func isPlanZeroPricingCandidate(candidate pricingSyncCandidate) bool {
	if !isPlanPricingProvider(candidate.entry.providerID) {
		return false
	}
	input := candidate.entry.model.Cost.Input
	output := candidate.entry.model.Cost.Output
	return input != nil && output != nil && *input == 0 && *output == 0
}

func isPlanPricingProvider(providerID string) bool {
	provider := strings.ToLower(strings.TrimSpace(providerID))
	return strings.Contains(provider, "coding-plan") || strings.Contains(provider, "token-plan")
}

func pricingProviderRankForModel(model string, providerID string) int {
	family := pricingModelFamily(model)
	provider := strings.ToLower(strings.TrimSpace(providerID))
	if family != "" {
		for index, officialProvider := range officialPricingProvidersByFamily(family) {
			if provider == officialProvider {
				return index
			}
		}
	}
	return 100 + pricingProviderRank(provider)
}

func pricingModelFamily(model string) string {
	normalized := normalizePricingModelKey(stripPricingModelPrefix(model))
	switch {
	case strings.HasPrefix(normalized, "gpt") || strings.HasPrefix(normalized, "chatgpt") || strings.HasPrefix(normalized, "o1") || strings.HasPrefix(normalized, "o3") || strings.HasPrefix(normalized, "o4"):
		return "openai"
	case strings.HasPrefix(normalized, "claude"):
		return "anthropic"
	case strings.HasPrefix(normalized, "deepseek"):
		return "deepseek"
	case strings.HasPrefix(normalized, "glm"):
		return "glm"
	case strings.HasPrefix(normalized, "qwen"):
		return "qwen"
	case strings.HasPrefix(normalized, "gemini"):
		return "google"
	case strings.HasPrefix(normalized, "grok"):
		return "xai"
	case strings.HasPrefix(normalized, "minimax"):
		return "minimax"
	case strings.HasPrefix(normalized, "moonshot") || strings.HasPrefix(normalized, "kimi"):
		return "moonshot"
	case strings.HasPrefix(normalized, "doubao"):
		return "doubao"
	case strings.HasPrefix(normalized, "mistral"):
		return "mistral"
	case strings.HasPrefix(normalized, "llama"):
		return "llama"
	case strings.HasPrefix(normalized, "xiaomi"):
		return "xiaomi"
	default:
		return ""
	}
}

func officialPricingProvidersByFamily(family string) []string {
	switch family {
	case "openai":
		return []string{"openai", "azure", "azure-cognitive-services"}
	case "anthropic":
		return []string{"anthropic", "google-vertex-anthropic"}
	case "deepseek":
		return []string{"deepseek", "siliconflow-cn", "siliconflow"}
	case "glm":
		return []string{"zai", "zhipuai", "zai-coding-plan", "zhipuai-coding-plan"}
	case "qwen":
		return []string{"alibaba-cn", "alibaba", "aliyun-bailian"}
	case "google":
		return []string{"google", "google-vertex"}
	case "xai":
		return []string{"xai"}
	case "minimax":
		return []string{"minimax-cn", "minimax", "minimax-cn-coding-plan", "minimax-coding-plan"}
	case "moonshot":
		return []string{"moonshotai-cn", "moonshotai", "kimi-for-coding"}
	case "doubao":
		return []string{"doubao"}
	case "mistral":
		return []string{"mistral"}
	case "llama":
		return []string{"llama"}
	case "xiaomi":
		return []string{"xiaomi-token-plan-cn", "xiaomi", "xiaomi-token-plan-sgp", "xiaomi-token-plan-ams"}
	default:
		return nil
	}
}

func pricingProviderRank(providerID string) int {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "openai":
		return 0
	case "anthropic":
		return 1
	case "deepseek":
		return 2
	case "google":
		return 3
	case "alibaba-cn", "alibaba":
		return 4
	case "zai", "zhipuai":
		return 5
	case "xai":
		return 6
	case "minimax-cn", "minimax", "minimax-cn-coding-plan", "minimax-coding-plan":
		return 7
	case "xiaomi-token-plan-cn", "xiaomi-token-plan-sgp", "xiaomi-token-plan-ams", "xiaomi":
		return 8
	case "302ai":
		return 9
	case "openrouter":
		return 10
	case "vercel":
		return 11
	default:
		return 100
	}
}

func stripPricingModelPrefix(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}
	index := strings.LastIndexAny(trimmed, "/:")
	if index < 0 || index == len(trimmed)-1 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[index+1:])
}

func normalizePricingModelKey(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func buildPricingSyncMatch(model string, metadataModel modelsDevModel, matchType string, providerID string, providerName string) (servicedto.PricingSyncMatch, bool) {
	if metadataModel.Cost.Input == nil || metadataModel.Cost.Output == nil {
		return servicedto.PricingSyncMatch{}, false
	}
	input := *metadataModel.Cost.Input
	output := *metadataModel.Cost.Output
	if input < 0 || output < 0 {
		return servicedto.PricingSyncMatch{}, false
	}

	pricingStyle := pricingStyleForModelsDevModel(metadataModel)
	cacheRead := 0.0
	if metadataModel.Cost.CacheRead != nil {
		cacheRead = *metadataModel.Cost.CacheRead
	}
	cacheWrite := 0.0
	// Keeper 当前保存单组基础价格；这里只映射 Models.dev 顶层 cache_write，长上下文 tiers 留待独立价格模型支持。
	if metadataModel.Cost.CacheWrite != nil {
		cacheWrite = *metadataModel.Cost.CacheWrite
	}
	if cacheRead < 0 || cacheWrite < 0 {
		return servicedto.PricingSyncMatch{}, false
	}

	matchedModel := strings.TrimSpace(metadataModel.ID)
	if matchedModel == "" {
		matchedModel = strings.TrimSpace(metadataModel.Name)
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		providerName = strings.TrimSpace(providerID)
	}
	return servicedto.PricingSyncMatch{
		Model:                model,
		MatchedModel:         matchedModel,
		MatchType:            matchType,
		SourceProviderID:     strings.TrimSpace(providerID),
		SourceProviderName:   providerName,
		PricingStyle:         pricingStyle,
		PromptPricePer1M:     input,
		CompletionPricePer1M: output,
		CacheReadPricePer1M:  cacheRead,
		CacheWritePricePer1M: cacheWrite,
	}, true
}

func pricingStyleForModelsDevModel(model modelsDevModel) string {
	if strings.Contains(strings.ToLower(model.ID), "claude") ||
		strings.Contains(strings.ToLower(model.Name), "claude") ||
		strings.Contains(strings.ToLower(model.Family), "claude") {
		return entities.ModelPricingStyleClaude
	}
	return entities.ModelPricingStyleOpenAI
}
