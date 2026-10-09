package service

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricingmetadata"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func (s *pricingService) PreviewPricingSync(ctx context.Context, sourceID string) (servicedto.PricingSyncPreview, error) {
	if _, err := pricingmetadata.SourceByID(sourceID); err != nil {
		return servicedto.PricingSyncPreview{}, err
	}
	models, err := s.effectiveModels(ctx)
	if err != nil {
		return servicedto.PricingSyncPreview{}, err
	}
	catalog, err := s.metadataClient.Fetch(ctx, sourceID)
	if err != nil {
		return servicedto.PricingSyncPreview{}, err
	}
	return buildPricingSyncPreviewFromCatalog(models, catalog)
}

func validMetadataPrice(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

type pricingCatalogIndex struct {
	exact      map[string][]pricingmetadata.Entry
	normalized map[string][]pricingmetadata.Entry
}

type pricingSyncCandidate struct {
	entry         pricingmetadata.Entry
	matchType     string
	score         int
	idMatchLength int
}

func buildPricingSyncPreviewFromCatalog(
	models []string,
	catalog pricingmetadata.Catalog,
) (servicedto.PricingSyncPreview, error) {
	entries := catalog.Entries
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
		SourceID:        catalog.Source.ID,
		Source:          catalog.Source.Name,
		SourceURL:       catalog.Source.URL,
		MetadataModels:  len(entries),
		Matches:         matches,
		UnmatchedModels: unmatched,
	}, nil
}

func buildPricingSyncMatchFromCandidates(model string, candidates []pricingSyncCandidate) (servicedto.PricingSyncMatch, bool) {
	for _, candidate := range candidates {
		match, ok := buildPricingSyncMatch(
			model,
			candidate.entry.Model,
			candidate.matchType,
			candidate.entry.ProviderID,
			candidate.entry.ProviderName,
		)
		if ok {
			return match, true
		}
	}
	return servicedto.PricingSyncMatch{}, false
}

func buildPricingCatalogIndex(entries []pricingmetadata.Entry) pricingCatalogIndex {
	index := pricingCatalogIndex{
		exact:      make(map[string][]pricingmetadata.Entry, len(entries)*3),
		normalized: make(map[string][]pricingmetadata.Entry, len(entries)*3),
	}
	for _, entry := range entries {
		model := entry.Model
		if strings.TrimSpace(model.ID) == "" && strings.TrimSpace(model.Name) == "" {
			continue
		}
		registerPricingCatalogIndexValues(index.exact, entry,
			model.ID, model.Name, stripPricingModelPrefix(model.ID), stripPricingModelPrefix(model.Name))
		registerPricingCatalogIndexValues(index.normalized, entry,
			normalizePricingModelKey(model.ID), normalizePricingModelKey(model.Name),
			normalizePricingModelKey(stripPricingModelPrefix(model.ID)), normalizePricingModelKey(stripPricingModelPrefix(model.Name)))
	}
	return index
}

func registerPricingCatalogIndexValues(target map[string][]pricingmetadata.Entry, entry pricingmetadata.Entry, values ...string) {
	// 同一模型的 ID、名称与去前缀形式常常相同，只注册一次，避免重复候选。
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		target[key] = append(target[key], entry)
	}
}

func matchPricingCatalogCandidates(model string, index pricingCatalogIndex) []pricingSyncCandidate {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}

	var candidates []pricingSyncCandidate
	add := func(entries []pricingmetadata.Entry, matchType string, score int) {
		for _, entry := range entries {
			candidates = append(candidates, pricingSyncCandidate{
				entry:         entry,
				matchType:     matchType,
				score:         score,
				idMatchLength: pricingModelIDMatchLength(model, entry.Model.ID),
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

func pricingModelIDMatchLength(model, id string) int {
	model = strings.ToLower(strings.TrimSpace(model))
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return 0
	}
	// CPA 前缀之外仍保留目录完整 ID；明确地区 ID 比裸模型别名更具体。
	if model == id || strings.HasSuffix(model, "/"+id) || strings.HasSuffix(model, ":"+id) ||
		normalizePricingModelKey(stripPricingModelPrefix(model)) == normalizePricingModelKey(id) {
		return len(id)
	}
	return 0
}

func sortedUniquePricingCandidates(model string, candidates []pricingSyncCandidate) []pricingSyncCandidate {
	unique := make([]pricingSyncCandidate, 0, len(candidates))
	bestByKey := make(map[string]pricingSyncCandidate, len(candidates))
	for _, candidate := range candidates {
		key := strings.TrimSpace(candidate.entry.ProviderID) + "\x00" + strings.TrimSpace(candidate.entry.Model.ID)
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
	leftPlanZero := isPlanZeroPricingCandidate(left)
	rightPlanZero := isPlanZeroPricingCandidate(right)
	if leftPlanZero != rightPlanZero {
		return !leftPlanZero
	}
	leftRank := pricingProviderRankForModel(model, left.entry.ProviderID)
	rightRank := pricingProviderRankForModel(model, right.entry.ProviderID)
	// 已匹配到模型时，官方及既有首选供应商优先于名称格式分数；套餐零价仍后置。
	// 没有可用首选价格时，第三方候选继续按匹配精度和既有顺序兜底。
	if leftRank != rightRank && (leftRank < 100 || rightRank < 100) {
		return leftRank < rightRank
	}
	if left.score != right.score {
		return left.score > right.score
	}
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	// 同等级候选先选完整 ID；只有别名可用时，较少命名空间的通用条目优先于地区变体。
	if left.idMatchLength != right.idMatchLength {
		return left.idMatchLength > right.idMatchLength
	}
	leftNamespaces := strings.Count(left.entry.Model.ID, "/")
	rightNamespaces := strings.Count(right.entry.Model.ID, "/")
	if leftNamespaces != rightNamespaces {
		return leftNamespaces < rightNamespaces
	}
	leftDeprecated := isDeprecatedPricingModel(left.entry.Model)
	rightDeprecated := isDeprecatedPricingModel(right.entry.Model)
	if leftDeprecated != rightDeprecated {
		return !leftDeprecated
	}
	if left.entry.Model.LastUpdated != right.entry.Model.LastUpdated {
		return left.entry.Model.LastUpdated > right.entry.Model.LastUpdated
	}
	if left.entry.ProviderID != right.entry.ProviderID {
		return left.entry.ProviderID < right.entry.ProviderID
	}
	return left.entry.Model.ID < right.entry.Model.ID
}

func isDeprecatedPricingModel(model pricingmetadata.Model) bool {
	return strings.EqualFold(strings.TrimSpace(model.Status), "deprecated")
}

func isPlanZeroPricingCandidate(candidate pricingSyncCandidate) bool {
	if !isPlanPricingProvider(candidate.entry.ProviderID) {
		return false
	}
	input := candidate.entry.Model.Cost.Input
	output := candidate.entry.Model.Cost.Output
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
	identity := strings.ToLower(stripPricingModelPrefix(model))
	normalized := normalizePricingModelKey(strings.TrimPrefix(identity, "ft:"))
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
	// Mistral 的子系列不都以厂商品牌开头，open/labs 仍属于模型身份。
	case strings.HasPrefix(normalized, "mistral"), strings.HasPrefix(normalized, "devstral"),
		strings.HasPrefix(normalized, "codestral"), strings.HasPrefix(normalized, "magistral"),
		strings.HasPrefix(normalized, "ministral"), strings.HasPrefix(normalized, "mixtral"),
		strings.HasPrefix(normalized, "pixtral"), strings.HasPrefix(normalized, "voxtral"),
		strings.HasPrefix(normalized, "openmistral"), strings.HasPrefix(normalized, "openmixtral"),
		strings.HasPrefix(normalized, "opencodestral"), strings.HasPrefix(normalized, "labsdevstral"),
		strings.HasPrefix(normalized, "labsleanstral"):
		return "mistral"
	case strings.HasPrefix(normalized, "command"):
		return "cohere"
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
	case "cohere":
		return []string{"cohere"}
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
	// ft: 是计费身份而非 CPA 前缀；包括完整微调 ID，避免再截成普通模型或组织后缀。
	// 只在原字符串上查找标记，避免 Unicode 小写化改变 UTF-8 长度后错用字节偏移。
	for index := 0; index+len("ft:") <= len(trimmed); index++ {
		if (index == 0 || trimmed[index-1] == '/' || trimmed[index-1] == ':') &&
			strings.EqualFold(trimmed[index:index+len("ft:")], "ft:") {
			return trimmed[index:]
		}
	}
	// Bedrock 等模型以 :0 标记版本；跳过数字版本后缀，再寻找 CPA 自定义前缀。
	for end := len(trimmed); end > 0; {
		index := strings.LastIndexAny(trimmed[:end], "/:")
		if index < 0 || index == len(trimmed)-1 {
			return trimmed
		}
		if trimmed[index] == ':' && isPricingModelVersion(trimmed[index+1:end]) {
			end = index
			continue
		}
		return strings.TrimSpace(trimmed[index+1:])
	}
	return trimmed
}

func isPricingModelVersion(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
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

func buildPricingSyncMatch(model string, metadataModel pricingmetadata.Model, matchType string, providerID string, providerName string) (servicedto.PricingSyncMatch, bool) {
	if metadataModel.Cost.Input == nil || metadataModel.Cost.Output == nil {
		return servicedto.PricingSyncMatch{}, false
	}
	input := *metadataModel.Cost.Input
	output := *metadataModel.Cost.Output
	if !validMetadataPrice(input) || !validMetadataPrice(output) {
		return servicedto.PricingSyncMatch{}, false
	}

	pricingStyle := pricingStyleForMetadataModel(metadataModel)
	cacheRead := 0.0
	if metadataModel.Cost.CacheRead != nil {
		cacheRead = *metadataModel.Cost.CacheRead
	}
	cacheWrite := 0.0
	// Keeper 当前保存单组基础价格；这里只映射 来源提供的基础缓存写入价格，长上下文 tiers 留待独立价格模型支持。
	if metadataModel.Cost.CacheWrite != nil {
		cacheWrite = *metadataModel.Cost.CacheWrite
	}
	if !validMetadataPrice(cacheRead) || !validMetadataPrice(cacheWrite) {
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

func pricingStyleForMetadataModel(model pricingmetadata.Model) string {
	if strings.Contains(strings.ToLower(model.ID), "claude") ||
		strings.Contains(strings.ToLower(model.Name), "claude") ||
		strings.Contains(strings.ToLower(model.Family), "claude") {
		return entities.ModelPricingStyleClaude
	}
	return entities.ModelPricingStyleOpenAI
}
