package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// 三个分类分别聚合，避免模型、Key、身份和计费规则的组合膨胀；与汇总共用调用方的事务快照。
func loadUsageOverviewComparisonTokenSeries(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, byDay bool, comparisons *dto.UsageOverviewComparisonsRecord, identities analysisIdentityLookup) error {
	table := "usage_overview_hourly_stats"
	if grain == "daily" {
		table = "usage_overview_daily_stats"
	}
	for _, dimension := range []string{"model", "api_group_key", "auth_index"} {
		query := db.Table(table).
			Select("bucket_start, "+dimension+", COALESCE(SUM(total_tokens), 0)").
			Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
			Group("bucket_start, " + dimension)
		if key := strings.TrimSpace(filter.APIGroupKey); key != "" {
			query = query.Where("api_group_key = ?", key)
		}
		if err := scanUsageOverviewComparisonTokenSeries(query, dimension, byDay, comparisons, identities); err != nil {
			return fmt.Errorf("load usage overview %s %s token series: %w", grain, dimension, err)
		}
	}
	return nil
}

func scanUsageOverviewComparisonTokenSeries(query *gorm.DB, dimension string, byDay bool, comparisons *dto.UsageOverviewComparisonsRecord, identities analysisIdentityLookup) error {
	rows, err := query.Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	// 固定列读取并直接累计，只保留最终序列；同一时间桶复用格式化结果。
	var previous time.Time
	var bucket string
	for rows.Next() {
		var timestamp time.Time
		var key sql.NullString
		var tokens int64
		if err := rows.Scan(&timestamp, &key, &tokens); err != nil {
			return err
		}
		if bucket == "" || !timestamp.Equal(previous) {
			bucket, _ = usageOverviewBucket(timeutil.NormalizeStorageTime(timestamp), byDay)
			previous = timestamp
		}
		switch dimension {
		case "model":
			addUsageOverviewComparisonTokens(comparisons.Models, normalizeUsageOverviewDimension(key.String), bucket, tokens)
		case "api_group_key":
			addUsageOverviewComparisonTokens(comparisons.APIKeys, normalizeUsageOverviewDimension(key.String), bucket, tokens)
		case "auth_index":
			for _, target := range []struct {
				kind  entities.UsageIdentityAuthType
				items map[string]*dto.UsageComparisonItemRecord
			}{
				{entities.UsageIdentityAuthTypeAuthFile, comparisons.AuthFiles},
				{entities.UsageIdentityAuthTypeAIProvider, comparisons.AIProviders},
			} {
				if identity, ok := identities.find(target.kind, strings.TrimSpace(key.String)); ok {
					addUsageOverviewComparisonTokens(target.items, identity.identity, bucket, tokens)
				}
			}
		}
	}
	return rows.Err()
}

func addUsageOverviewComparisonTokens(items map[string]*dto.UsageComparisonItemRecord, key, bucket string, tokens int64) {
	// 汇总已在同一快照内创建所有项目；这里只填充趋势，不能重复增加总量和费用。
	if item := items[key]; item != nil {
		item.TokenBuckets[bucket] += tokens
	}
}
