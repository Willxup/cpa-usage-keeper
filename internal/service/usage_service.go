package service

import (
	"context"

	"cpa-usage-keeper/internal/cpa"
)

type UsageProvider interface {
	GetUsage(context.Context) (*cpa.StatisticsSnapshot, error)
	GetUsageWithFilter(context.Context, UsageFilter) (*cpa.StatisticsSnapshot, error)
	ListUsageEvents(context.Context, UsageFilter) ([]UsageEventRecord, error)
}
