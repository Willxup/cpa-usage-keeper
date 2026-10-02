package service

import (
	"context"
	"fmt"
	"sync"
)

// usageWorkGate 统一保护 inbox 处理和存储维护；暂停期间新的调用等待，已入场调用自然完成。
type usageWorkGate struct {
	mu      sync.Mutex
	changed chan struct{}
	paused  bool
	active  int
}

// enter 在任何仓储读写或重试状态变更前取得许可，释放时包括提交后的通知阶段。
func (g *usageWorkGate) enter(ctx context.Context) (func(), error) {
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

// leave 在整个批次或维护结束后释放许可，最后一个调用结束才唤醒停稳等待者。
func (g *usageWorkGate) leave() {
	g.mu.Lock()
	g.active--
	if g.active == 0 {
		g.notifyLocked()
	}
	g.mu.Unlock()
}

// pause 阻止新处理和维护入场，等已入场调用完成；取消等待会立即恢复入场许可。
func (g *usageWorkGate) pause(ctx context.Context) (func(), error) {
	g.mu.Lock()
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	if g.paused {
		g.mu.Unlock()
		return nil, fmt.Errorf("usage work already paused")
	}
	g.paused = true
	g.mu.Unlock()
	resume := sync.OnceFunc(func() {
		g.mu.Lock()
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

// changeChannelLocked 仅在确有等待者时创建一次广播通道。
func (g *usageWorkGate) changeChannelLocked() chan struct{} {
	if g.changed == nil {
		g.changed = make(chan struct{})
	}
	return g.changed
}

// notifyLocked 只唤醒已注册的等待者，不为日常事件批次分配新通道。
func (g *usageWorkGate) notifyLocked() {
	if g.changed != nil {
		close(g.changed)
		g.changed = nil
	}
}
