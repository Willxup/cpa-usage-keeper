package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
)

var ErrInvalidUsageIdentityModelsRange = errors.New("invalid usage identity models range")

type UsageIdentityModelsReader interface {
	GetUsageIdentityModels(context.Context, int64, string) (UsageIdentityModels, error)
}

type UsageIdentityModelTotals struct {
	RequestCount         int64
	InputTokens          int64
	OutputTokens         int64
	ReasoningTokens      int64
	CacheReadTokens      int64
	CacheCreationTokens  int64
	TotalTokens          int64
	CostUSD              float64
	UnavailableCostCount int64
}

type UsageIdentityModels struct {
	Range      string
	Timezone   string
	RangeStart time.Time
	RangeEnd   time.Time
	Models     []repository.UsageIdentityModelUsage
	Summary    UsageIdentityModelTotals
}

func (s *usageIdentityService) GetUsageIdentityModels(ctx context.Context, id int64, rangeName string) (UsageIdentityModels, error) {
	if id <= 0 {
		return UsageIdentityModels{}, ErrInvalidID
	}
	rangeName = strings.TrimSpace(rangeName)
	if rangeName == "" {
		rangeName = "7d"
	}
	days := 0
	switch rangeName {
	case "today":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	default:
		return UsageIdentityModels{}, ErrInvalidUsageIdentityModelsRange
	}

	// 自然日按项目时区计数；先求真实日界，避免 DST 或午夜跳时改变范围。
	today := timeutil.DateOf(timeutil.NormalizeStorageTime(s.now()))
	todayBounds, ok := timeutil.DayBounds(today, time.Local)
	if !ok {
		return UsageIdentityModels{}, fmt.Errorf("current calendar date does not exist")
	}
	startDate := today.AddDays(1 - days)
	startBounds, ok := timeutil.DayBounds(startDate, time.Local)
	for !ok && startDate.DaysUntil(today) > 0 {
		startDate = startDate.AddDays(1)
		startBounds, ok = timeutil.DayBounds(startDate, time.Local)
	}
	if !ok {
		return UsageIdentityModels{}, fmt.Errorf("identity models range has no valid start date")
	}
	models, err := repository.LoadUsageIdentityModels(ctx, s.db, id, startBounds.Start, todayBounds.End)
	if err != nil {
		return UsageIdentityModels{}, err
	}
	result := UsageIdentityModels{
		Range: rangeName, Timezone: time.Local.String(), RangeStart: startBounds.Start,
		RangeEnd: todayBounds.End, Models: models,
	}
	for _, model := range models {
		result.Summary.RequestCount += model.RequestCount
		result.Summary.InputTokens += model.InputTokens
		result.Summary.OutputTokens += model.OutputTokens
		result.Summary.ReasoningTokens += model.ReasoningTokens
		result.Summary.CacheReadTokens += model.CacheReadTokens
		result.Summary.CacheCreationTokens += model.CacheCreationTokens
		result.Summary.TotalTokens += model.TotalTokens
		result.Summary.CostUSD += model.CostUSD
		result.Summary.UnavailableCostCount += model.UnavailableCostCount
	}
	if math.IsNaN(result.Summary.CostUSD) || math.IsInf(result.Summary.CostUSD, 0) {
		return UsageIdentityModels{}, fmt.Errorf("usage identity models total cost is not finite")
	}
	return result, nil
}
