package ranking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

const LocalRankingAggregationInterval = 5 * time.Minute

type LocalRankingAggregator interface {
	AggregateOnce(context.Context) error
}

// LocalRankingRunner 每五分钟独立触发一次本地排行完整日快照，不接入 Community 上报调度。
type LocalRankingRunner struct {
	aggregator LocalRankingAggregator
	interval   time.Duration
}

func NewLocalRankingRunner(aggregator LocalRankingAggregator) (*LocalRankingRunner, error) {
	return NewLocalRankingRunnerWithInterval(aggregator, LocalRankingAggregationInterval)
}

// NewLocalRankingRunnerWithInterval 仅供模块测试缩短 ticker。
func NewLocalRankingRunnerWithInterval(aggregator LocalRankingAggregator, interval time.Duration) (*LocalRankingRunner, error) {
	if aggregator == nil {
		return nil, fmt.Errorf("local ranking aggregator is required")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("local ranking interval must be positive")
	}
	return &LocalRankingRunner{aggregator: aggregator, interval: interval}, nil
}

func (r *LocalRankingRunner) Run(ctx context.Context) error {
	if r == nil || r.aggregator == nil || r.interval <= 0 {
		return fmt.Errorf("local ranking runner is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 首次聚合等待一个完整周期，让 usage inbox 与 metadata 启动追赶先行。
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *LocalRankingRunner) runOnce(ctx context.Context) {
	if err := r.aggregator.AggregateOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logrus.WithError(err).Error("local ranking aggregation failed")
	}
}
