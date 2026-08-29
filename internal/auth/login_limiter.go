package auth

import (
	"sync"
	"time"
)

const (
	defaultLoginAttemptWindow         = time.Minute
	defaultLoginAttemptPerSourceLimit = 5
	defaultLoginAttemptGlobalLimit    = 60
	defaultLoginAttemptMaxSources     = 4096
)

type LoginAttemptLimiterOptions struct {
	Window         time.Duration
	PerSourceLimit int
	GlobalLimit    int
	MaxSources     int
	Now            func() time.Time
}

type loginAttemptWindow struct {
	StartedAt time.Time
	LastSeen  time.Time
	Attempts  int
}

type LoginAttemptLimiter struct {
	mu sync.Mutex

	window         time.Duration
	perSourceLimit int
	globalLimit    int
	maxSources     int
	now            func() time.Time

	sources map[string]loginAttemptWindow
	global  loginAttemptWindow
}

func NewLoginAttemptLimiter(options LoginAttemptLimiterOptions) *LoginAttemptLimiter {
	if options.Window <= 0 {
		options.Window = defaultLoginAttemptWindow
	}
	if options.PerSourceLimit <= 0 {
		options.PerSourceLimit = defaultLoginAttemptPerSourceLimit
	}
	if options.GlobalLimit <= 0 {
		options.GlobalLimit = defaultLoginAttemptGlobalLimit
	}
	if options.MaxSources <= 0 {
		options.MaxSources = defaultLoginAttemptMaxSources
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &LoginAttemptLimiter{
		window:         options.Window,
		perSourceLimit: options.PerSourceLimit,
		globalLimit:    options.GlobalLimit,
		maxSources:     options.MaxSources,
		now:            options.Now,
		sources:        make(map[string]loginAttemptWindow),
	}
}

func (l *LoginAttemptLimiter) Allow(source string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	if source == "" {
		source = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.resetGlobalWindowLocked(now)
	l.removeExpiredSourcesLocked(now)

	if l.global.Attempts >= l.globalLimit {
		return false, retryAfter(now, l.global.StartedAt, l.window)
	}

	state, exists := l.sources[source]
	if !exists {
		l.makeSourceCapacityLocked()
		state = loginAttemptWindow{StartedAt: now}
	}
	if state.Attempts >= l.perSourceLimit {
		state.LastSeen = now
		l.sources[source] = state
		return false, retryAfter(now, state.StartedAt, l.window)
	}

	state.Attempts++
	state.LastSeen = now
	l.sources[source] = state
	l.global.Attempts++
	l.global.LastSeen = now
	return true, 0
}

func (l *LoginAttemptLimiter) Reset(source string) {
	if l == nil || source == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sources, source)
}

func (l *LoginAttemptLimiter) resetGlobalWindowLocked(now time.Time) {
	if l.global.StartedAt.IsZero() || now.Sub(l.global.StartedAt) >= l.window {
		l.global = loginAttemptWindow{StartedAt: now, LastSeen: now}
	}
}

func (l *LoginAttemptLimiter) removeExpiredSourcesLocked(now time.Time) {
	for source, state := range l.sources {
		if now.Sub(state.StartedAt) >= l.window {
			delete(l.sources, source)
		}
	}
}

func (l *LoginAttemptLimiter) makeSourceCapacityLocked() {
	if len(l.sources) < l.maxSources {
		return
	}
	// 来源表达到硬上限时淘汰最久未访问项，避免公网来源轮换导致状态无界增长。
	var oldestSource string
	var oldestSeen time.Time
	for source, state := range l.sources {
		if oldestSource == "" || state.LastSeen.Before(oldestSeen) {
			oldestSource = source
			oldestSeen = state.LastSeen
		}
	}
	delete(l.sources, oldestSource)
}

func retryAfter(now, startedAt time.Time, window time.Duration) time.Duration {
	retry := startedAt.Add(window).Sub(now)
	if retry <= 0 {
		return time.Second
	}
	return retry
}
