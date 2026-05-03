package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type MetadataSyncer interface {
	SyncMetadata(ctx context.Context) error
}

type MetadataSyncRunner struct {
	syncer   MetadataSyncer
	interval time.Duration
	sleep    func(context.Context, time.Duration) bool

	mu      sync.Mutex
	running bool
}

func NewMetadataSyncRunner(syncer MetadataSyncer, interval time.Duration) *MetadataSyncRunner {
	return &MetadataSyncRunner{
		syncer:   syncer,
		interval: interval,
		sleep:    maintenanceSleepContext,
	}
}

// Run 启动独立 metadata 同步任务：启动后立即执行一次，之后按固定间隔刷新 auth files 和 provider metadata。
func (r *MetadataSyncRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	logrus.Info("metadata sync task started")
	r.setRunning(true)
	defer r.setRunning(false)

	delay := time.Duration(0)
	for {
		if !r.sleep(ctx, delay) {
			return nil
		}
		if err := r.syncer.SyncMetadata(ctx); err != nil {
			logrus.WithError(err).Error("metadata sync failed")
		}
		delay = r.interval
	}
}

func (r *MetadataSyncRunner) validate() error {
	if r == nil {
		return fmt.Errorf("metadata sync runner is nil")
	}
	if r.syncer == nil {
		return fmt.Errorf("metadata syncer is nil")
	}
	if r.interval <= 0 {
		return fmt.Errorf("metadata sync interval must be positive")
	}
	if r.sleep == nil {
		r.sleep = maintenanceSleepContext
	}
	return nil
}

func (r *MetadataSyncRunner) setRunning(running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
}
