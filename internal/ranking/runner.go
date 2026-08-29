package ranking

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	SyncInterval        = 30 * time.Minute
	MinimumSyncInterval = 15 * time.Minute
	SyncTimeout         = 10 * time.Second
)

type RunnerSyncer interface {
	RunOnce(context.Context) error
	ScheduleSeed(context.Context) (string, error)
}

type runnerIntervalProvider interface {
	SyncInterval(context.Context) (time.Duration, error)
}

type Runner struct {
	syncer   RunnerSyncer
	interval time.Duration
	timeout  time.Duration
	now      func() time.Time
}

func NewRunner(syncer RunnerSyncer) (*Runner, error) {
	return newRunner(syncer, SyncInterval, SyncTimeout)
}

// NewRunnerWithTiming 仅用于模块测试缩短等待；生产装配固定调用 NewRunner。
func NewRunnerWithTiming(syncer RunnerSyncer, interval, timeout time.Duration) (*Runner, error) {
	return newRunner(syncer, interval, timeout)
}

func newRunner(syncer RunnerSyncer, interval, timeout time.Duration) (*Runner, error) {
	if syncer == nil {
		return nil, fmt.Errorf("ranking syncer is required")
	}
	if interval <= 0 || timeout <= 0 {
		return nil, fmt.Errorf("ranking runner timing must be positive")
	}
	return &Runner{syncer: syncer, interval: interval, timeout: timeout, now: time.Now}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.syncer == nil || r.interval <= 0 || r.timeout <= 0 || r.now == nil {
		return fmt.Errorf("ranking runner is not configured")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		seed, interval := r.schedule(ctx)
		now := r.now()
		delay := NextScheduledRun(now, seed, interval).Sub(now)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
			currentSeed, currentInterval := r.schedule(ctx)
			// 身份或中心频率在等待期间变化时，旧时间槽只负责唤醒并重新排期。
			if currentSeed != seed || currentInterval != interval {
				continue
			}
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) schedule(ctx context.Context) (string, time.Duration) {
	seed, err := r.syncer.ScheduleSeed(ctx)
	if err != nil {
		logrus.WithError(err).Error("ranking schedule seed lookup failed")
		seed = "ranking-disabled"
	}
	interval := r.interval
	if provider, ok := r.syncer.(runnerIntervalProvider); ok {
		centerInterval, intervalErr := provider.SyncInterval(ctx)
		if intervalErr != nil {
			logrus.WithError(intervalErr).Error("ranking sync interval lookup failed")
		} else if centerInterval > 0 {
			interval = centerInterval
		}
	}
	return seed, interval
}

func (r *Runner) runOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	if err := r.syncer.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrSyncInProgress) {
		logrus.WithError(err).Error("ranking synchronization failed")
	}
}

// NextScheduledRun 将实例稳定散列到固定周期内，避免所有 Keeper 同时访问中心。
func NextScheduledRun(now time.Time, seed string, interval time.Duration) time.Time {
	if interval <= 0 {
		return now
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(seed))
	intervalNanos := uint64(interval)
	offset := time.Duration(hash.Sum64() % intervalNanos)
	utc := now.UTC()
	base := utc.Truncate(interval)
	candidate := base.Add(offset)
	if !candidate.After(utc) {
		candidate = candidate.Add(interval)
	}
	return candidate
}

func (s *Service) ScheduleSeed(ctx context.Context) (string, error) {
	state, err := s.store.Load(ctx)
	if err != nil {
		return "", err
	}
	if state.PublicKey != "" {
		return state.PublicKey, nil
	}
	return "ranking-disabled", nil
}

// SyncInterval 仅让 active 参与者读取中心频率；其他状态保持本地默认值且不访问中心。
func (s *Service) SyncInterval(ctx context.Context) (time.Duration, error) {
	state, err := s.store.Load(ctx)
	if err != nil {
		return SyncInterval, err
	}
	if state.Status != StatusActive {
		return SyncInterval, nil
	}
	metadata, err := s.LeaderboardMetadata(ctx)
	if err != nil {
		return SyncInterval, err
	}
	requested := time.Duration(metadata.SuggestedSyncIntervalSeconds) * time.Second
	if requested < MinimumSyncInterval {
		return MinimumSyncInterval, nil
	}
	return requested, nil
}
