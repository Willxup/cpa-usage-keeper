package poller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Syncer interface {
	SyncOnce(ctx context.Context) error
}

type StatusSyncer interface {
	SyncStatus(ctx context.Context) (string, error)
}

var ErrSyncAlreadyRunning = errors.New("sync already running")
var ErrSyncCompletedWithWarnings = errors.New("sync completed with warnings")

type Poller struct {
	interval time.Duration
	syncer   Syncer
	ticker   func(time.Duration) ticker
	now      func() time.Time

	mu          sync.Mutex
	running     bool
	lastRunAt   time.Time
	lastError   string
	lastWarning string
	lastStatus  string
	syncRunning bool
}

type Status struct {
	Running     bool
	LastRunAt   time.Time
	LastError   string
	LastWarning string
	LastStatus  string
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

	p.runBackgroundSync(ctx)

	t := p.ticker(p.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.Chan():
			p.runBackgroundSync(ctx)
		}
	}
}

func (p *Poller) runBackgroundSync(ctx context.Context) {
	if err := p.runSync(ctx); shouldLogSyncError(err) {
		logrus.WithError(err).Error("poller sync failed")
	}
}

func shouldLogSyncError(err error) bool {
	return err != nil && !errors.Is(err, ErrSyncCompletedWithWarnings) && !errors.Is(err, ErrSyncAlreadyRunning) && !errors.Is(err, context.Canceled)
}

func (p *Poller) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Status{
		Running:     p.running,
		LastRunAt:   p.lastRunAt,
		LastError:   p.lastError,
		LastWarning: p.lastWarning,
		LastStatus:  p.lastStatus,
		SyncRunning: p.syncRunning,
	}
}

func (p *Poller) SyncNow(ctx context.Context) error {
	return p.runSync(ctx)
}

func (p *Poller) runSync(ctx context.Context) error {
	p.mu.Lock()
	if p.syncRunning {
		p.mu.Unlock()
		return ErrSyncAlreadyRunning
	}
	p.syncRunning = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.syncRunning = false
		p.mu.Unlock()
	}()

	lastStatus := ""
	var err error
	if statusSyncer, ok := p.syncer.(StatusSyncer); ok {
		lastStatus, err = statusSyncer.SyncStatus(ctx)
	} else {
		err = p.syncer.SyncOnce(ctx)
	}

	warningResult := err != nil && lastStatus != "" && lastStatus != "failed"
	p.mu.Lock()
	p.lastRunAt = p.now().UTC()
	p.lastStatus = lastStatus
	p.lastWarning = ""
	if warningResult {
		p.lastWarning = err.Error()
		p.lastError = ""
	} else if err != nil {
		p.lastError = err.Error()
	} else {
		p.lastError = ""
	}
	p.mu.Unlock()
	if warningResult {
		return fmt.Errorf("%w: %v", ErrSyncCompletedWithWarnings, err)
	}
	return err
}

func (p *Poller) setRunning(running bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = running
}
