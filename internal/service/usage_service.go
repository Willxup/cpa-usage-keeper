package service

import (
	"context"

	"cpa-usage-keeper/internal/cpa"
)

type UsageProvider interface {
	GetUsageWithFilter(context.Context, UsageFilter) (*cpa.StatisticsSnapshot, error)
	GetUsageOverview(context.Context, UsageFilter) (*UsageOverviewSnapshot, error)
	ListUsageEvents(context.Context, UsageFilter) ([]UsageEventRecord, error)
	ListUsageCredentialStats(context.Context, UsageFilter) ([]UsageCredentialStat, error)
	GetUsageAnalysis(context.Context, UsageFilter) (*UsageAnalysisSnapshot, error)
}
