package service

import (
	"context"
	"errors"
	"sync"
)

var ErrCostReadGateAlreadyBlocked = errors.New("cost reads are already blocked")

// CostReadGate 只协调费用 HTTP 读取与重算写入；非费用统计及 CPA 接收不占用此许可。
type CostReadGate struct {
	mu      sync.Mutex
	blocked bool
	active  int
	drained chan struct{}
}

// NewCostReadGate 创建单个 App 内共享的费用读取许可。
func NewCostReadGate() *CostReadGate {
	return &CostReadGate{}
}

// Acquire 非阻塞取得一次费用读取许可。调用方须在完整响应或流式导出结束后释放；释放函数可重复调用。
// nil gate 表示独立路由没有接入重算协调。
func (g *CostReadGate) Acquire() (release func(), ok bool) {
	if g == nil {
		return func() {}, true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked {
		return nil, false
	}
	g.active++
	return sync.OnceFunc(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.active--
		if g.active == 0 && g.drained != nil {
			close(g.drained)
			g.drained = nil
		}
	}), true
}

// BlockAndDrain 立即拒绝新费用请求，再等已获许可的完整 HTTP 响应结束。
// 成功时保持阻断并返回幂等恢复函数；取消时自动恢复，避免协调器失败后永久封读。
func (g *CostReadGate) BlockAndDrain(ctx context.Context) (resume func(), err error) {
	if g == nil {
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.blocked {
		g.mu.Unlock()
		return nil, ErrCostReadGateAlreadyBlocked
	}
	g.blocked = true
	active := g.active
	var drained chan struct{}
	if active > 0 {
		g.drained = make(chan struct{})
		drained = g.drained
	}
	g.mu.Unlock()

	resume = sync.OnceFunc(func() {
		g.mu.Lock()
		g.blocked = false
		g.mu.Unlock()
	})
	if active > 0 {
		select {
		case <-drained:
		case <-ctx.Done():
			resume()
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		resume()
		return nil, err
	}
	return resume, nil
}
