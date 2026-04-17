package poller

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Syncer interface {
	SyncOnce(ctx context.Context) error
}

type Poller struct {
	interval time.Duration
	syncer   Syncer
	ticker   func(time.Duration) ticker
	now      func() time.Time

	mu          sync.Mutex
	running     bool
	lastRunAt   time.Time
	lastError   string
	syncRunning bool
}

type Status struct {
	Running     bool
	LastRunAt   time.Time
	LastError   string
	SyncRunning bool
}

type ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct {
	inner *time.Ticker
}

func New(syncer Syncer, interval time.Duration) *Poller {
	return &Poller{
		interval: interval,
		syncer:   syncer,
		ticker: func(d time.Duration) ticker {
			return realTicker{inner: time.NewTicker(d)}
		},
		now: time.Now,
	}
}

func (t realTicker) Chan() <-chan time.Time { return t.inner.C }
func (t realTicker) Stop()                  { t.inner.Stop() }

func (p *Poller) Run(ctx context.Context) error {
	if p == nil {
		return fmt.Errorf("poller is nil")
	}
	if p.syncer == nil {
		return fmt.Errorf("poller syncer is nil")
	}
	if p.interval <= 0 {
		return fmt.Errorf("poll interval must be greater than zero")
	}
	if p.ticker == nil {
		p.ticker = func(d time.Duration) ticker { return realTicker{inner: time.NewTicker(d)} }
	}
	if p.now == nil {
		p.now = time.Now
	}

	p.setRunning(true)
	defer p.setRunning(false)

	p.runSync(ctx)

	t := p.ticker(p.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.Chan():
			p.runSync(ctx)
		}
	}
}

func (p *Poller) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Status{
		Running:     p.running,
		LastRunAt:   p.lastRunAt,
		LastError:   p.lastError,
		SyncRunning: p.syncRunning,
	}
}

func (p *Poller) runSync(ctx context.Context) {
	p.mu.Lock()
	if p.syncRunning {
		p.mu.Unlock()
		return
	}
	p.syncRunning = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.syncRunning = false
		p.mu.Unlock()
	}()

	err := p.syncer.SyncOnce(ctx)

	p.mu.Lock()
	p.lastRunAt = p.now().UTC()
	if err != nil {
		p.lastError = err.Error()
	} else {
		p.lastError = ""
	}
	p.mu.Unlock()
}

func (p *Poller) setRunning(running bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = running
}
