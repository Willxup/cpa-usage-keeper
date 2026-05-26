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

// CycleCostProvider 暴露按 Codex 周限额 cycle 聚合的账号成本视图.
type CycleCostProvider interface {
	GetCurrentCycles(ctx context.Context, provider string) ([]CycleSummary, error)
	GetHistoricalCycles(ctx context.Context, provider, authIndex string, limit int) ([]CycleSummary, error)
	GetCycleBreakdown(ctx context.Context, provider, authIndex string, cycleEnd time.Time) (*CycleBreakdown, error)
}

// CycleSummary 是一个 (provider, auth_index, cycle window) 维度的聚合行.
type CycleSummary struct {
	Provider       string    `json:"provider"`
	AuthIndex      string    `json:"authIndex"`
	IdentityName   string    `json:"identityName"`
	IdentityType   string    `json:"identityType,omitempty"`
	WindowSeconds  int64     `json:"windowSeconds"`
	WindowLabel    string    `json:"windowLabel,omitempty"`
	CycleStart     time.Time `json:"cycleStart"`
	CycleEnd       time.Time `json:"cycleEnd"`
	UsedPercent    float64   `json:"usedPercent"`
	Sealed         bool      `json:"sealed"`
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
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, fmt.Errorf("provider is required")
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	snapshots, err := repository.LatestSnapshotForEachAuthIndex(ctx, s.db, provider, CodexWeeklyWindowSeconds)
	if err != nil {
		return nil, err
	}
	pricing, err := repository.ListModelPriceSettings(s.db)
	if err != nil {
		return nil, fmt.Errorf("load pricing settings: %w", err)
	}
	pricingByModel := buildPricingIndex(pricing)
	identities, err := s.loadIdentitiesByAuthIndex(ctx, snapshotAuthIndexes(snapshots))
	if err != nil {
		return nil, err
	}
	summaries := make([]CycleSummary, 0, len(snapshots))
	for _, snapshot := range snapshots {
		cycleEnd := snapshot.ResetAt
		cycleStart := cycleEnd.Add(-time.Duration(snapshot.WindowSeconds) * time.Second)
		aggregate, missing, err := s.aggregateCycleCost(ctx, provider, snapshot.AuthIndex, cycleStart, cycleEnd, pricingByModel)
		if err != nil {
			return nil, err
		}
		summary := CycleSummary{
			Provider:       provider,
			AuthIndex:      snapshot.AuthIndex,
			WindowSeconds:  snapshot.WindowSeconds,
			WindowLabel:    snapshot.WindowLabel,
			CycleStart:     cycleStart,
			CycleEnd:       cycleEnd,
			UsedPercent:    snapshot.UsedPercent,
			Sealed:         time.Now().After(cycleEnd),
			TotalUSD:       aggregate.totalUSD,
			TotalTokens:    aggregate.totalTokens,
			RequestCount:   aggregate.requestCount,
			LastCapturedAt: snapshot.CapturedAt,
			PricingMissing: missing,
		}
		if identity, ok := identities[snapshot.AuthIndex]; ok {
			summary.IdentityName = identity.Name
			summary.IdentityType = identity.Type
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].TotalUSD != summaries[j].TotalUSD {
			return summaries[i].TotalUSD > summaries[j].TotalUSD
		}
		return summaries[i].AuthIndex < summaries[j].AuthIndex
	})
	return summaries, nil
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
