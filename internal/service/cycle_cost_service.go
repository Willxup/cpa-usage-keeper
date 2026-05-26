package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

// CodexWeeklyWindowSeconds 是 Codex 上游对周限额窗口的固定长度 (7 days).
const CodexWeeklyWindowSeconds int64 = 604800

// ProviderAll 是 "全部 provider" 哨兵.
const ProviderAll = ""

// CycleCostProvider 暴露按 Codex 周限额 cycle 聚合的账号成本视图.
type CycleCostProvider interface {
	GetCurrentCycles(ctx context.Context, provider string) ([]CycleSummary, error)
	GetHistoricalCycles(ctx context.Context, provider, authIndex string, limit int) ([]CycleSummary, error)
	GetCycleBreakdown(ctx context.Context, provider, authIndex string, cycleEnd time.Time) (*CycleBreakdown, error)
	ListProviders(ctx context.Context) ([]CycleProviderSummary, error)
}

// CycleProviderSummary 是 provider 过滤栏每一条数据.
type CycleProviderSummary struct {
	Provider string `json:"provider"`
	Count    int64  `json:"count"`
}

// CycleSummary 是一个 (provider, auth_index, cycle window) 维度的聚合行.
type CycleSummary struct {
	Provider       string    `json:"provider"`
	AuthIndex      string    `json:"authIndex"`
	IdentityName   string    `json:"identityName"`
	IdentityType   string    `json:"identityType,omitempty"`
	PlanType       string    `json:"planType,omitempty"`
	Disabled       bool      `json:"disabled,omitempty"`
	WindowSeconds  int64     `json:"windowSeconds"`
	WindowLabel    string    `json:"windowLabel,omitempty"`
	CycleStart     time.Time `json:"cycleStart"`
	CycleEnd       time.Time `json:"cycleEnd"`
	UsedPercent    float64   `json:"usedPercent"`
	Sealed         bool      `json:"sealed"`
	HasSnapshot    bool      `json:"hasSnapshot"`
	TotalUSD       float64   `json:"totalUsd"`
	TotalTokens    int64     `json:"totalTokens"`
	RequestCount   int64     `json:"requestCount"`
	LastCapturedAt time.Time `json:"lastCapturedAt"`
	PricingMissing bool      `json:"pricingMissing"`
}

// CycleBreakdown 是 CycleSummary 加上按 model 拆分的明细.
type CycleBreakdown struct {
	CycleSummary
	Models []CycleModelCost `json:"models"`
}

// CycleModelCost 是 cycle 内单个 model 的 token / 成本明细.
type CycleModelCost struct {
	Model               string  `json:"model"`
	ModelAlias          string  `json:"modelAlias,omitempty"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	ReasoningTokens     int64   `json:"reasoningTokens"`
	CachedTokens        int64   `json:"cachedTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	TotalTokens         int64   `json:"totalTokens"`
	RequestCount        int64   `json:"requestCount"`
	USDCost             float64 `json:"usdCost"`
	PricingMissing      bool    `json:"pricingMissing"`
}

type cycleCostService struct {
	db *gorm.DB
}

func NewCycleCostService(db *gorm.DB) CycleCostProvider {
	return &cycleCostService{db: db}
}

func (s *cycleCostService) GetCurrentCycles(ctx context.Context, provider string) ([]CycleSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	provider = strings.TrimSpace(provider)
	// 1. 列出全部 active auth-file identity (按 provider 过滤或全部)
	identities, err := repository.ListActiveAuthFileIdentities(ctx, s.db, provider)
	if err != nil {
		return nil, err
	}
	if len(identities) == 0 {
		return []CycleSummary{}, nil
	}
	// 2. 加载 snapshot — 用 provider 过滤如果指定, 否则一次拿全
	snapshots, err := repository.LatestSnapshotForEachAuthIndex(ctx, s.db, provider, CodexWeeklyWindowSeconds)
	if err != nil {
		return nil, err
	}
	snapshotByKey := make(map[string]entities.QuotaCycleSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotByKey[snapshot.Provider+"::"+snapshot.AuthIndex] = snapshot
	}
	// 3. 加载 pricing
	pricing, err := repository.ListModelPriceSettings(s.db)
	if err != nil {
		return nil, fmt.Errorf("load pricing settings: %w", err)
	}
	pricingByModel := buildPricingIndex(pricing)
	// 4. 合并 — 每个 identity 一行, 有 snapshot 的填 cycle 数据, 没的 HasSnapshot=false
	summaries := make([]CycleSummary, 0, len(identities))
	for _, identity := range identities {
		identityProvider := strings.TrimSpace(identity.Provider)
		if identityProvider == "" {
			identityProvider = strings.TrimSpace(identity.Type)
		}
		summary := CycleSummary{
			Provider:     identityProvider,
			AuthIndex:    identity.Identity,
			IdentityName: identity.Name,
			IdentityType: identity.Type,
		}
		if identity.PlanType != nil {
			summary.PlanType = *identity.PlanType
		}
		if identity.Disabled != nil {
			summary.Disabled = *identity.Disabled
		}
		if snapshot, ok := snapshotByKey[identityProvider+"::"+identity.Identity]; ok {
			cycleEnd := snapshot.ResetAt
			cycleStart := cycleEnd.Add(-time.Duration(snapshot.WindowSeconds) * time.Second)
			aggregate, missing, err := s.aggregateCycleCost(ctx, identityProvider, identity.Identity, cycleStart, cycleEnd, pricingByModel)
			if err != nil {
				return nil, err
			}
			summary.HasSnapshot = true
			summary.WindowSeconds = snapshot.WindowSeconds
			summary.WindowLabel = snapshot.WindowLabel
			summary.CycleStart = cycleStart
			summary.CycleEnd = cycleEnd
			summary.UsedPercent = snapshot.UsedPercent
			summary.Sealed = time.Now().After(cycleEnd)
			summary.TotalUSD = aggregate.totalUSD
			summary.TotalTokens = aggregate.totalTokens
			summary.RequestCount = aggregate.requestCount
			summary.LastCapturedAt = snapshot.CapturedAt
			summary.PricingMissing = missing
		}
		summaries = append(summaries, summary)
	}
	// 5. 默认按 $ DESC 然后 name ASC; 没 snapshot 的归到最后. (前端可再排序)
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].HasSnapshot != summaries[j].HasSnapshot {
			return summaries[i].HasSnapshot
		}
		if summaries[i].TotalUSD != summaries[j].TotalUSD {
			return summaries[i].TotalUSD > summaries[j].TotalUSD
		}
		return summaries[i].IdentityName < summaries[j].IdentityName
	})
	return summaries, nil
}

func (s *cycleCostService) ListProviders(ctx context.Context) ([]CycleProviderSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	counts, err := repository.ListActiveAuthFileProviders(ctx, s.db)
	if err != nil {
		return nil, err
	}
	results := make([]CycleProviderSummary, 0, len(counts))
	for _, c := range counts {
		if strings.TrimSpace(c.Provider) == "" {
			continue
		}
		results = append(results, CycleProviderSummary{Provider: c.Provider, Count: c.Count})
	}
	return results, nil
}

func (s *cycleCostService) GetHistoricalCycles(ctx context.Context, provider, authIndex string, limit int) ([]CycleSummary, error) {
	provider = strings.TrimSpace(provider)
	authIndex = strings.TrimSpace(authIndex)
	if provider == "" || authIndex == "" {
		return nil, fmt.Errorf("provider and auth_index are required")
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if limit <= 0 {
		limit = 12
	}
	resetAts, err := repository.ListDistinctResetAt(ctx, s.db, provider, authIndex, CodexWeeklyWindowSeconds, limit)
	if err != nil {
		return nil, err
	}
	if len(resetAts) == 0 {
		return []CycleSummary{}, nil
	}
	pricing, err := repository.ListModelPriceSettings(s.db)
	if err != nil {
		return nil, fmt.Errorf("load pricing settings: %w", err)
	}
	pricingByModel := buildPricingIndex(pricing)
	identities, err := s.loadIdentitiesByAuthIndex(ctx, []string{authIndex})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	summaries := make([]CycleSummary, 0, len(resetAts))
	for _, resetAt := range resetAts {
		cycleEnd := resetAt
		cycleStart := cycleEnd.Add(-time.Duration(CodexWeeklyWindowSeconds) * time.Second)
		aggregate, missing, err := s.aggregateCycleCost(ctx, provider, authIndex, cycleStart, cycleEnd, pricingByModel)
		if err != nil {
			return nil, err
		}
		summary := CycleSummary{
			Provider:       provider,
			AuthIndex:      authIndex,
			WindowSeconds:  CodexWeeklyWindowSeconds,
			CycleStart:     cycleStart,
			CycleEnd:       cycleEnd,
			Sealed:         now.After(cycleEnd),
			TotalUSD:       aggregate.totalUSD,
			TotalTokens:    aggregate.totalTokens,
			RequestCount:   aggregate.requestCount,
			PricingMissing: missing,
		}
		if identity, ok := identities[authIndex]; ok {
			summary.IdentityName = identity.Name
			summary.IdentityType = identity.Type
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (s *cycleCostService) GetCycleBreakdown(ctx context.Context, provider, authIndex string, cycleEnd time.Time) (*CycleBreakdown, error) {
	provider = strings.TrimSpace(provider)
	authIndex = strings.TrimSpace(authIndex)
	if provider == "" || authIndex == "" {
		return nil, fmt.Errorf("provider and auth_index are required")
	}
	if cycleEnd.IsZero() {
		return nil, fmt.Errorf("cycleEnd is required")
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	pricing, err := repository.ListModelPriceSettings(s.db)
	if err != nil {
		return nil, fmt.Errorf("load pricing settings: %w", err)
	}
	pricingByModel := buildPricingIndex(pricing)
	identities, err := s.loadIdentitiesByAuthIndex(ctx, []string{authIndex})
	if err != nil {
		return nil, err
	}
	cycleStart := cycleEnd.Add(-time.Duration(CodexWeeklyWindowSeconds) * time.Second)
	aggregates, err := repository.SumUsageTokensByModelForAuthIndex(ctx, s.db, provider, authIndex, cycleStart, cycleEnd)
	if err != nil {
		return nil, err
	}
	models := make([]CycleModelCost, 0, len(aggregates))
	totalUSD := 0.0
	totalTokens := int64(0)
	requestCount := int64(0)
	anyMissing := false
	for _, a := range aggregates {
		cost, missing := computeCycleModelCost(a, pricingByModel)
		if missing {
			anyMissing = true
		}
		totalUSD += cost
		totalTokens += a.TotalTokens
		requestCount += a.RequestCount
		models = append(models, CycleModelCost{
			Model:               a.Model,
			ModelAlias:          a.ModelAlias,
			InputTokens:         a.InputTokens,
			OutputTokens:        a.OutputTokens,
			ReasoningTokens:     a.ReasoningTokens,
			CachedTokens:        a.CachedTokens,
			CacheReadTokens:     a.CacheReadTokens,
			CacheCreationTokens: a.CacheCreationTokens,
			TotalTokens:         a.TotalTokens,
			RequestCount:        a.RequestCount,
			USDCost:             cost,
			PricingMissing:      missing,
		})
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].USDCost != models[j].USDCost {
			return models[i].USDCost > models[j].USDCost
		}
		return models[i].Model < models[j].Model
	})
	summary := CycleSummary{
		Provider:       provider,
		AuthIndex:      authIndex,
		WindowSeconds:  CodexWeeklyWindowSeconds,
		CycleStart:     cycleStart,
		CycleEnd:       cycleEnd,
		Sealed:         time.Now().After(cycleEnd),
		TotalUSD:       totalUSD,
		TotalTokens:    totalTokens,
		RequestCount:   requestCount,
		PricingMissing: anyMissing,
	}
	if identity, ok := identities[authIndex]; ok {
		summary.IdentityName = identity.Name
		summary.IdentityType = identity.Type
	}
	// latest snapshot 给出窗口标签 / used%
	if snapshot, err := repository.LatestQuotaCycleSnapshot(ctx, s.db, provider, authIndex, CodexWeeklyWindowSeconds); err == nil {
		if snapshot.ResetAt.Equal(cycleEnd) {
			summary.UsedPercent = snapshot.UsedPercent
			summary.WindowLabel = snapshot.WindowLabel
			summary.LastCapturedAt = snapshot.CapturedAt
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return &CycleBreakdown{CycleSummary: summary, Models: models}, nil
}

type cycleAggregate struct {
	totalUSD     float64
	totalTokens  int64
	requestCount int64
}

func (s *cycleCostService) aggregateCycleCost(ctx context.Context, provider, authIndex string, cycleStart, cycleEnd time.Time, pricingByModel map[string]entities.ModelPriceSetting) (cycleAggregate, bool, error) {
	aggregates, err := repository.SumUsageTokensByModelForAuthIndex(ctx, s.db, provider, authIndex, cycleStart, cycleEnd)
	if err != nil {
		return cycleAggregate{}, false, err
	}
	total := cycleAggregate{}
	missing := false
	for _, a := range aggregates {
		cost, missingPrice := computeCycleModelCost(a, pricingByModel)
		total.totalUSD += cost
		total.totalTokens += a.TotalTokens
		total.requestCount += a.RequestCount
		if missingPrice {
			missing = true
		}
	}
	return total, missing, nil
}

func (s *cycleCostService) loadIdentitiesByAuthIndex(ctx context.Context, authIndexes []string) (map[string]entities.UsageIdentity, error) {
	result := make(map[string]entities.UsageIdentity, len(authIndexes))
	if len(authIndexes) == 0 {
		return result, nil
	}
	var identities []entities.UsageIdentity
	if err := s.db.WithContext(ctx).
		Where("identity IN (?) AND auth_type = ?", authIndexes, entities.UsageIdentityAuthTypeAuthFile).
		Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("load identities: %w", err)
	}
	for _, identity := range identities {
		result[identity.Identity] = identity
	}
	return result, nil
}

func snapshotAuthIndexes(snapshots []entities.QuotaCycleSnapshot) []string {
	result := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, snapshot.AuthIndex)
	}
	return result
}

func buildPricingIndex(settings []entities.ModelPriceSetting) map[string]entities.ModelPriceSetting {
	index := make(map[string]entities.ModelPriceSetting, len(settings))
	for _, setting := range settings {
		index[setting.Model] = setting
	}
	return index
}

// computeCycleModelCost 按现有 calculateUsageOverviewStatCost 的口径计算成本:
//
//	prompt_tokens = input_tokens - cached_tokens (非缓存输入)
//	$ = prompt_tokens × prompt_price + output_tokens × completion_price + cached_tokens × cache_price
//
// 缓存价目同时覆盖 cached_tokens + cache_read_tokens (两者都是缓存命中的输入).
// cache_creation_tokens 当前 keeper 没有专门计价, 暂按 prompt 价目计入.
func computeCycleModelCost(a repository.AuthIndexTokenAggregate, pricingByModel map[string]entities.ModelPriceSetting) (float64, bool) {
	pricing, ok := pricingByModel[a.Model]
	if !ok {
		return 0, true
	}
	cachedTotal := a.CachedTokens + a.CacheReadTokens
	if cachedTotal < 0 {
		cachedTotal = 0
	}
	promptTokens := a.InputTokens + a.CacheCreationTokens - cachedTotal
	if promptTokens < 0 {
		promptTokens = 0
	}
	return (float64(promptTokens)/1_000_000.0)*pricing.PromptPricePer1M +
		(float64(a.OutputTokens)/1_000_000.0)*pricing.CompletionPricePer1M +
		(float64(cachedTotal)/1_000_000.0)*pricing.CachePricePer1M, false
}
