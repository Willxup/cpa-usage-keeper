package poller

import (
	"context"
	"fmt"
	"sync"

	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// aggregationWorkGate 等普通聚合整页结束，并让单次受控 Overview 追平独占暂停期。
type aggregationWorkGate struct {
	mu         sync.Mutex
	changed    chan struct{}
	paused     bool
	ready      bool
	active     int
	controlled bool
}

// enter 在 Reader 预读前获取普通 turn 许可，直到写入和 runner 内存状态更新才释放。
func (g *aggregationWorkGate) enter(ctx context.Context) (func(), error) {
	for {
		g.mu.Lock()
		if err := ctx.Err(); err != nil {
			g.mu.Unlock()
			return nil, err
		}
		if !g.paused {
			g.active++
			g.mu.Unlock()
			return g.leave, nil
		}
		changed := g.changeChannelLocked()
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// leave 在整页提交和内存状态更新后释放普通许可，最后一页完成时唤醒停稳等待。
func (g *aggregationWorkGate) leave() {
	g.mu.Lock()
	g.active--
	if g.active == 0 {
		g.notifyLocked()
	}
	g.mu.Unlock()
}

// pause 禁止新普通 turn，等待已预读或正在提交的 turn 及其状态更新全部结束。
func (g *aggregationWorkGate) pause(ctx context.Context) (func(), error) {
	g.mu.Lock()
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	if g.paused {
		g.mu.Unlock()
		return nil, fmt.Errorf("usage aggregation already paused")
	}
	g.paused = true
	g.mu.Unlock()
	resume := sync.OnceFunc(func() {
		g.mu.Lock()
		g.ready = false
		for g.controlled {
			changed := g.changeChannelLocked()
			g.mu.Unlock()
			<-changed
			g.mu.Lock()
		}
		g.paused = false
		g.notifyLocked()
		g.mu.Unlock()
	})
	for {
		g.mu.Lock()
		if err := ctx.Err(); err != nil {
			g.mu.Unlock()
			resume()
			return nil, err
		}
		if g.active == 0 {
			g.ready = true
			g.mu.Unlock()
			return resume, nil
		}
		changed := g.changeChannelLocked()
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			resume()
			return nil, ctx.Err()
		}
	}
}

// beginControlled 只允许停稳后的一个受控追平，防止两个调用读取同一旧 checkpoint。
func (g *aggregationWorkGate) beginControlled(ctx context.Context) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !g.paused || !g.ready {
		return nil, fmt.Errorf("usage aggregation is not paused and settled")
	}
	if g.controlled {
		return nil, fmt.Errorf("controlled overview catch-up already running")
	}
	g.controlled = true
	return func() {
		g.mu.Lock()
		g.controlled = false
		g.notifyLocked()
		g.mu.Unlock()
	}, nil
}

// changeChannelLocked 只为真正等待停稳或恢复的调用创建一次广播通道。
func (g *aggregationWorkGate) changeChannelLocked() chan struct{} {
	if g.changed == nil {
		g.changed = make(chan struct{})
	}
	return g.changed
}

// notifyLocked 唤醒现有等待者，日常聚合没有等待者时不分配通道。
func (g *aggregationWorkGate) notifyLocked() {
	if g.changed != nil {
		close(g.changed)
		g.changed = nil
	}
}

// Pause 停稳 Run 和 RunOnce 共用的普通聚合 turn；返回函数在追平结束或失败后恢复调度。
// 取消等待会自动恢复许可，恢复函数会等待已经开始的受控追平完成。
func (r *UsageAggregationRunner) Pause(ctx context.Context) (func(), error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("usage aggregation runner database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return r.workGate.pause(ctx)
}

// CatchUpOverviewTo 在停稳期绕过 inbox 让路，按固定 H 逐页推进已存事件的 Overview 费用与原统计。
// 每页独立持有短写事务；已超 H 的 checkpoint 原样保留，Activity、Latency、Identity 均不触及。
func (r *UsageAggregationRunner) CatchUpOverviewTo(ctx context.Context, targetEventID int64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("usage aggregation runner database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if targetEventID < 0 {
		return fmt.Errorf("controlled overview target must be non-negative: %d", targetEventID)
	}
	finish, err := r.workGate.beginControlled(ctx)
	if err != nil {
		return err
	}
	defer finish()
	writeDB := r.db.Clauses(dbresolver.Write).Session(&gorm.Session{Context: ctx})
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot, err := repository.LoadUsageAggregationCheckpointSnapshot(ctx, r.db)
		if err != nil {
			return err
		}
		if snapshot.OverviewCursor >= targetEventID {
			return nil
		}
		if err := r.applyControlledOverviewPage(ctx, writeDB, snapshot.OverviewCursor, targetEventID); err != nil {
			return err
		}
	}
}

// applyControlledOverviewPage 以固定 H 从 Reader 取一页，纯计算后在一个事务提交小时、日和水位。
func (r *UsageAggregationRunner) applyControlledOverviewPage(ctx context.Context, writeDB *gorm.DB, cursor, targetEventID int64) error {
	pageCtx, cancel := context.WithTimeout(ctx, usageAggregationTransactionTimeout)
	defer cancel()
	events, err := repository.LoadUsageAggregationEventPage(pageCtx, r.db, cursor, targetEventID, usageAggregationEventPageSize)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return fmt.Errorf("controlled overview page is empty before target %d from cursor %d", targetEventID, cursor)
	}
	hourly, daily, nextCursor, err := overview.BuildRows(events)
	if err != nil {
		return err
	}
	return repository.ApplyUsageOverviewAggregationPage(pageCtx, writeDB, cursor, nextCursor, hourly, daily, r.now())
}
