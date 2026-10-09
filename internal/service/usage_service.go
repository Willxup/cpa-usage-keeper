package service

import (
	"context"

	servicedto "cpa-usage-keeper/internal/service/dto"
)

type UsageProvider interface {
	GetUsageOverview(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error)
	GetUsageActivity(context.Context, servicedto.UsageFilter) (*servicedto.UsageActivitySnapshot, error)
	GetUsageOverviewRealtime(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error)
	ListUsageEvents(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventsPage, error)
	StreamUsageEvents(context.Context, servicedto.UsageFilter, func(servicedto.UsageEventRecord) error) error
	ListUsageEventFilterOptions(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error)
	GetAnalysis(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error)
	GetAnalysisLatency(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisLatencyDiagnostics, error)
}

// UsageComparisonProvider 提供 Overview 下方维度比较数据；单独拆出接口避免基础 Overview 调用方被强制增加查询。
type UsageComparisonProvider interface {
	GetUsageOverviewComparisons(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error)
}
