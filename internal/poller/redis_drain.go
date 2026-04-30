package poller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/service"
	"github.com/sirupsen/logrus"
)

const redisInboxProcessInterval = 5 * time.Second // Redis inbox 处理频率固定在这里，后续如需配置优先改这里。

type RedisBatchSyncer interface {
	PullRedisUsageInbox(ctx context.Context) (*service.RedisInboxPullResult, error)
	ProcessRedisUsageInbox(ctx context.Context, syncMetadata bool) (*service.RedisBatchSyncResult, error)
	SyncMetadata(ctx context.Context) error
}

type RedisDrainConfig struct {
	IdleInterval     time.Duration
	ErrorBackoff     time.Duration
	MetadataInterval time.Duration
}

type RedisDrain struct {
	syncer RedisBatchSyncer
	config RedisDrainConfig
	now    func() time.Time
	sleep  func(context.Context, time.Duration) bool

	mu                 sync.Mutex
	running            bool
	lastRunAt          time.Time
	lastError          string
	lastWarning        string
	lastStatus         string
	pullRunning        bool
	processRunning     bool
	lastMetadataSyncAt time.Time
}

func NewRedisDrain(syncer RedisBatchSyncer, cfg RedisDrainConfig) *RedisDrain {
	return &RedisDrain{
		syncer: syncer,
		config: cfg,
		now:    time.Now,
		sleep:  sleepContext,
	}
}

func (d *RedisDrain) Run(ctx context.Context) error {
	if err := d.validate(); err != nil {
		return err
	}
	d.setRunning(true)
	defer d.setRunning(false)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d.runPullLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		d.runProcessLoop(ctx)
	}()
	<-ctx.Done()
	wg.Wait()
	return nil
}

func (d *RedisDrain) runPullLoop(ctx context.Context) {
	logrus.WithField("idle_interval", d.config.IdleInterval.String()).Info("redis inbox pull task started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		result, err := d.runRedisPull(ctx)
		if err != nil {
			if shouldLogSyncError(err) {
				logrus.WithError(err).Error("redis drain pull failed")
			}
			if !d.sleep(ctx, d.config.ErrorBackoff) {
				return
			}
			continue
		}
		if result != nil && result.Empty {
			if !d.sleep(ctx, d.config.IdleInterval) {
				return
			}
		}
	}
}

func (d *RedisDrain) runProcessLoop(ctx context.Context) {
	logrus.WithField("interval", redisInboxProcessInterval.String()).Info("redis inbox process task started")
	for {
		if !d.sleep(ctx, redisInboxProcessInterval) {
			return
		}
		syncMetadata := d.shouldSyncMetadata()
		result, err := d.runRedisProcess(ctx, syncMetadata)
		if err != nil && !errors.Is(err, ErrSyncCompletedWithWarnings) {
			if shouldLogSyncError(err) {
				d.logBatchFailure(result, syncMetadata, err)
			}
			continue
		}
		if syncMetadata && result != nil && (!result.Empty || errors.Is(err, ErrSyncCompletedWithWarnings)) {
			d.setLastMetadataSyncAt(d.now().UTC())
		}
	}
}

func (d *RedisDrain) logBatchFailure(result *service.RedisBatchSyncResult, syncMetadata bool, err error) {
	fields := logrus.Fields{
		"sync_metadata":   syncMetadata,
		"auth_error":      errors.Is(err, cpa.ErrRedisQueueAuth),
		"status":          "",
		"empty":           false,
		"inserted_events": 0,
		"deduped_events":  0,
	}
	if result != nil {
		fields["status"] = result.Status
		fields["empty"] = result.Empty
		fields["inserted_events"] = result.InsertedEvents
		fields["deduped_events"] = result.DedupedEvents
	}
	logrus.WithError(err).WithFields(fields).Error("redis drain batch failed")
}

func (d *RedisDrain) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return Status{
		Running:     d.running,
		LastRunAt:   d.lastRunAt,
		LastError:   d.lastError,
		LastWarning: d.lastWarning,
		LastStatus:  d.lastStatus,
		SyncRunning: d.pullRunning || d.processRunning,
	}
}

func (d *RedisDrain) SyncNow(ctx context.Context) error {
	if err := d.validate(); err != nil {
		return err
	}
	if _, err := d.runRedisPull(ctx); err != nil {
		return err
	}
	_, err := d.runRedisProcess(ctx, true)
	return err
}

func (d *RedisDrain) runRedisPull(ctx context.Context) (*service.RedisInboxPullResult, error) {
	d.mu.Lock()
	if d.pullRunning {
		d.mu.Unlock()
		return nil, ErrSyncAlreadyRunning
	}
	d.pullRunning = true
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.pullRunning = false
		d.mu.Unlock()
	}()

	result, err := d.syncer.PullRedisUsageInbox(ctx)
	d.recordPullResult(result, err)
	return result, err
}

func (d *RedisDrain) runRedisProcess(ctx context.Context, syncMetadata bool) (*service.RedisBatchSyncResult, error) {
	d.mu.Lock()
	if d.processRunning {
		d.mu.Unlock()
		return nil, ErrSyncAlreadyRunning
	}
	d.processRunning = true
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.processRunning = false
		d.mu.Unlock()
	}()

	result, err := d.syncer.ProcessRedisUsageInbox(ctx, syncMetadata)
	returnErr := err
	if err != nil && result != nil && result.Status != "" && result.Status != "failed" {
		returnErr = fmt.Errorf("%w: %v", ErrSyncCompletedWithWarnings, err)
	}
	d.recordResult(result, err)
	if err == nil && result != nil && result.Empty && syncMetadata {
		metadataErr := d.syncer.SyncMetadata(ctx)
		if metadataErr != nil {
			d.recordMetadataWarning(metadataErr)
			return result, fmt.Errorf("%w: %v", ErrSyncCompletedWithWarnings, metadataErr)
		}
		d.setLastMetadataSyncAt(d.now().UTC())
	}
	return result, returnErr
}

func (d *RedisDrain) recordPullResult(result *service.RedisInboxPullResult, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastRunAt = d.now().UTC()
	status := ""
	if result != nil {
		status = result.Status
	}
	if status == "" && err == nil {
		status = "completed"
	}
	d.lastStatus = status
	d.lastError = ""
	d.lastWarning = ""
	if err != nil {
		d.lastError = err.Error()
	}
}

func (d *RedisDrain) recordMetadataWarning(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastWarning = err.Error()
	d.lastError = ""
}

func (d *RedisDrain) recordResult(result *service.RedisBatchSyncResult, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastRunAt = d.now().UTC()
	status := ""
	if result != nil {
		status = result.Status
	}
	if status == "" && err == nil {
		status = "completed"
	}
	d.lastStatus = status
	d.lastError = ""
	d.lastWarning = ""
	if err != nil {
		if result != nil && result.Status != "" && result.Status != "failed" {
			d.lastWarning = err.Error()
		} else {
			d.lastError = err.Error()
		}
	}
}

func (d *RedisDrain) shouldSyncMetadata() bool {
	d.mu.Lock()
	last := d.lastMetadataSyncAt
	d.mu.Unlock()
	return last.IsZero() || d.now().UTC().Sub(last.UTC()) >= d.config.MetadataInterval
}

func (d *RedisDrain) setLastMetadataSyncAt(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastMetadataSyncAt = t.UTC()
}

func (d *RedisDrain) setRunning(running bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running = running
}

func (d *RedisDrain) validate() error {
	if d == nil {
		return fmt.Errorf("redis drain is nil")
	}
	if d.syncer == nil {
		return fmt.Errorf("redis drain syncer is nil")
	}
	if d.config.IdleInterval <= 0 {
		return fmt.Errorf("redis drain idle interval must be greater than zero")
	}
	if d.config.ErrorBackoff <= 0 {
		return fmt.Errorf("redis drain error backoff must be greater than zero")
	}
	if d.config.MetadataInterval <= 0 {
		return fmt.Errorf("redis drain metadata interval must be greater than zero")
	}
	if d.now == nil {
		d.now = time.Now
	}
	if d.sleep == nil {
		d.sleep = sleepContext
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
