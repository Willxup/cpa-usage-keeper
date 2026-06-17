package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
)

type PricingSyncer interface {
	SyncPricing(context.Context, servicedto.SyncPricingInput) (servicedto.PricingSyncResult, error)
}

type PricingSyncRunner struct {
	syncer PricingSyncer
	now    func() time.Time
	sleep  func(context.Context, time.Duration) bool

	mu      sync.Mutex
	running bool
}

func NewPricingSyncRunner(syncer PricingSyncer) *PricingSyncRunner {
	return &PricingSyncRunner{
		syncer: syncer,
		now:    time.Now,
		sleep:  maintenanceSleepContext,
	}
}

// Run starts one LiteLLM pricing sync on startup, then retries daily in the project local timezone.
func (r *PricingSyncRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	logrus.Info("pricing sync task started")
	r.setRunning(true)
	defer r.setRunning(false)

	delay := time.Duration(0)
	for {
		if !r.sleep(ctx, delay) {
			return nil
		}
		if _, err := r.syncer.SyncPricing(ctx, servicedto.SyncPricingInput{}); err != nil {
			logrus.WithError(err).Error("pricing sync failed")
		}
		now := r.now()
		delay = nextDailyPricingSyncAt(now).Sub(now)
		if delay < 0 {
			delay = 0
		}
	}
}

func nextDailyPricingSyncAt(now time.Time) time.Time {
	localNow := now.In(time.Local)
	syncAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 2, 30, 0, 0, time.Local)
	if !localNow.Before(syncAt) {
		syncAt = syncAt.AddDate(0, 0, 1)
	}
	return syncAt
}

func (r *PricingSyncRunner) validate() error {
	if r == nil {
		return fmt.Errorf("pricing sync runner is nil")
	}
	if r.syncer == nil {
		return fmt.Errorf("pricing syncer is nil")
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.sleep == nil {
		r.sleep = maintenanceSleepContext
	}
	return nil
}

func (r *PricingSyncRunner) setRunning(running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
}
