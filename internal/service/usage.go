package service

import (
	"context"

	"cpa-usage-keeper/internal/cpa"
)

type UsageProvider interface {
	GetUsage(context.Context) (*cpa.StatisticsSnapshot, error)
}
