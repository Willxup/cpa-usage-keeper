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
