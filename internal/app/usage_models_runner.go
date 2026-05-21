package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cpa-usage-keeper/internal/repository"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const usageModelAggregationInterval = time.Minute

type UsageModelAggregationRunner struct {
	db       *gorm.DB
	interval time.Duration
	now      func() time.Time
	sleep    func(context.Context, time.Duration) bool

	mu      sync.Mutex
	running bool
}

func NewUsageModelAggregationRunner(db *gorm.DB) *UsageModelAggregationRunner {
	return &UsageModelAggregationRunner{
		db:       db,
		interval: usageModelAggregationInterval,
		now:      time.Now,
		sleep:    maintenanceSleepContext,
	}
}

// Run 定时把 usage_events 聚合到 usage_models，支撑定价页面的提供商/模型候选项。
func (r *UsageModelAggregationRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	logrus.WithField("interval", r.interval.String()).Info("usage model aggregation task started")
	r.setRunning(true)
	defer r.setRunning(false)

	delay := time.Duration(0)
	for {
		if !r.sleep(ctx, delay) {
			return nil
		}
		pending, err := repository.HasPendingUsageModelAggregation(ctx, r.db)
		if err != nil {
			logrus.WithError(err).Error("usage model aggregation pending check failed")
			delay = r.interval
			continue
		}
		if pending {
			if err := repository.AggregateUsageModels(ctx, r.db, r.now()); err != nil {
				logrus.WithError(err).Error("usage model aggregation failed")
			}
		}
		delay = r.interval
	}
}

func (r *UsageModelAggregationRunner) validate() error {
	if r == nil {
		return fmt.Errorf("usage model aggregation runner is nil")
	}
	if r.db == nil {
		return fmt.Errorf("usage model aggregation database is nil")
	}
	if r.interval <= 0 {
		return fmt.Errorf("usage model aggregation interval must be positive")
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.sleep == nil {
		r.sleep = maintenanceSleepContext
	}
	return nil
}

func (r *UsageModelAggregationRunner) setRunning(running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
}
