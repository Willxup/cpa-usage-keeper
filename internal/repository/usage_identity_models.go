package repository

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// UsageIdentityModelUsage 是身份在自然日范围内一个模型的已提交汇总。
type UsageIdentityModelUsage struct {
	Model                string
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

type usageIdentityModelProjection struct {
	Model                string
	RequestCount         int64
	InputTokens          int64
	OutputTokens         int64
	ReasoningTokens      int64
	CacheReadTokens      int64
	CacheCreationTokens  int64
	TotalTokens          int64
	CostUSD              *float64
	UnavailableCostCount *int64
	MissingCostCount     int64
}

const usageIdentityModelColumns = `model,
	SUM(request_count) AS request_count,
	SUM(input_tokens) AS input_tokens,
	SUM(output_tokens) AS output_tokens,
	SUM(reasoning_tokens) AS reasoning_tokens,
	SUM(cache_read_tokens) AS cache_read_tokens,
	SUM(cache_creation_tokens) AS cache_creation_tokens,
	SUM(total_tokens) AS total_tokens,
	SUM(cost_usd) AS cost_usd,
	SUM(unavailable_cost_count) AS unavailable_cost_count,
	SUM(CASE WHEN cost_usd IS NULL OR unavailable_cost_count IS NULL THEN 1 ELSE 0 END) AS missing_cost_count`

// LoadUsageIdentityModels 在同一只读快照中解析身份并按模型汇总日桶。
func LoadUsageIdentityModels(ctx context.Context, db *gorm.DB, id int64, start, end time.Time) ([]UsageIdentityModelUsage, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	models := make([]UsageIdentityModelUsage, 0)
	err := db.Clauses(dbresolver.Read).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity entities.UsageIdentity
		if err := tx.Select("id, identity").Where("id = ?", id).First(&identity).Error; err != nil {
			return fmt.Errorf("find usage identity by id: %w", err)
		}
		authIndex := strings.TrimSpace(identity.Identity)
		if authIndex == "" {
			return nil
		}

		// 仅在日桶中按稳定 auth_index 和自然日过滤；不读取明细或筛选下游 Key。
		query := tx.Model(&entities.UsageOverviewDailyStat{}).Where("auth_index = ?", authIndex)
		query = overviewBucketRead(start, end, "daily").query(query).
			Select(usageIdentityModelColumns).Group("model").Order("model ASC")
		var rows []usageIdentityModelProjection
		if err := query.Scan(&rows).Error; err != nil {
			return fmt.Errorf("load usage identity model daily stats: %w", err)
		}
		for _, row := range rows {
			if row.MissingCostCount != 0 || row.CostUSD == nil || row.UnavailableCostCount == nil {
				return fmt.Errorf("usage identity model %q has unbackfilled cost", row.Model)
			}
			if math.IsNaN(*row.CostUSD) || math.IsInf(*row.CostUSD, 0) {
				return fmt.Errorf("usage identity model %q has non-finite cost", row.Model)
			}
			models = append(models, UsageIdentityModelUsage{
				Model: row.Model, RequestCount: row.RequestCount,
				InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
				CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens,
				TotalTokens: row.TotalTokens, CostUSD: *row.CostUSD, UnavailableCostCount: *row.UnavailableCostCount,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return models, nil
}
