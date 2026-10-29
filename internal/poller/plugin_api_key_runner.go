package poller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	pluginAPIKeyDebounceInterval    = 5 * time.Second
	pluginAPIKeyBatchSize           = 1000
	pluginAPIKeyPendingLimit        = 10000
	pluginAPIKeyPendingByteLimit    = 8 << 20
	pluginAPIKeyInboxRetryInterval  = 100 * time.Millisecond
	pluginAPIKeyErrorBackoffInitial = 200 * time.Millisecond
	pluginAPIKeyErrorBackoffMax     = 5 * time.Second
	pluginAPIKeyRegistrationTimeout = 5 * time.Second
)

// PluginAPIKeyRunnerOptions 只供内部验证控制窗口和内存界限，不增加用户配置。
type PluginAPIKeyRunnerOptions struct {
	DebounceInterval     time.Duration
	MaxPendingIdentities int
	MaxPendingBytes      int
}

// PluginAPIKeyRunner 纯内存合并已提交身份；空闲和重启均不扫描事件或恢复水位。
type PluginAPIKeyRunner struct {
	db            *gorm.DB
	wake          chan struct{}
	debounce      time.Duration
	maxPending    int
	maxBytes      int
	mu            sync.Mutex
	pending       map[string]entities.PluginAPIKeyIdentity
	accepted      map[string]struct{}
	pendingBytes  int
	windowStarted time.Time
	dropped       int
}

func NewPluginAPIKeyRunner(db *gorm.DB) *PluginAPIKeyRunner {
	return NewPluginAPIKeyRunnerWithOptions(db, PluginAPIKeyRunnerOptions{})
}

func NewPluginAPIKeyRunnerWithOptions(db *gorm.DB, options PluginAPIKeyRunnerOptions) *PluginAPIKeyRunner {
	if options.DebounceInterval <= 0 {
		options.DebounceInterval = pluginAPIKeyDebounceInterval
	}
	if options.MaxPendingIdentities <= 0 {
		options.MaxPendingIdentities = pluginAPIKeyPendingLimit
	}
	if options.MaxPendingBytes <= 0 {
		options.MaxPendingBytes = pluginAPIKeyPendingByteLimit
	}
	return &PluginAPIKeyRunner{
		db: db, wake: make(chan struct{}, 1), debounce: options.DebounceInterval,
		maxPending: options.MaxPendingIdentities, maxBytes: options.MaxPendingBytes,
		pending: make(map[string]entities.PluginAPIKeyIdentity), accepted: make(map[string]struct{}),
	}
}

// NotifyPluginAPIKeys 只合并轻量身份，拒收超限部分不阻塞已经提交的用量。
func (r *PluginAPIKeyRunner) NotifyPluginAPIKeys(identities []entities.PluginAPIKeyIdentity) {
	if r == nil || len(identities) == 0 {
		return
	}
	r.mu.Lock()
	for _, identity := range identities {
		if _, exists := r.accepted[identity.APIGroupKey]; exists {
			continue
		}
		if !helper.IsPluginAccessProvider(identity.AccessProvider) || !helper.IsPluginAPIKeyGroupKey(identity.APIGroupKey) {
			continue
		}
		size := len(identity.APIGroupKey) + len(identity.AccessProvider)
		if len(r.accepted) >= r.maxPending || size > r.maxBytes-r.pendingBytes {
			r.dropped++
			continue
		}
		// 复制字符串所有权，不能留住原消息或调用方可复用的大缓冲。
		identity.APIGroupKey = strings.Clone(identity.APIGroupKey)
		identity.AccessProvider = strings.Clone(identity.AccessProvider)
		r.pending[identity.APIGroupKey] = identity
		r.accepted[identity.APIGroupKey] = struct{}{}
		r.pendingBytes += size
	}
	if r.windowStarted.IsZero() && (len(r.pending) > 0 || r.dropped > 0) {
		r.windowStarted = time.Now()
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// PendingStats 包含在途批次，便于诊断内存上限；完成前失败身份仍占原有配额。
func (r *PluginAPIKeyRunner) PendingStats() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.accepted), r.pendingBytes
}

func (r *PluginAPIKeyRunner) Run(ctx context.Context) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("plugin API key runner database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var active []entities.PluginAPIKeyIdentity
	backoff := pluginAPIKeyErrorBackoffInitial
	draining := false
	for ctx.Err() == nil {
		if len(active) == 0 {
			r.mu.Lock()
			started := r.windowStarted
			hasPending := len(r.pending) > 0 || r.dropped > 0
			r.mu.Unlock()
			if !hasPending {
				draining = false
				select {
				case <-ctx.Done():
					return nil
				case <-r.wake:
					continue
				}
			}
			if !draining {
				// 第一次通知固定窗口；后续通知只合并数据，不重置到期时间。
				if !waitPluginAPIKeyRetry(ctx, max(time.Until(started.Add(r.debounce)), 0)) {
					return nil
				}
				draining = true
			}
			r.mu.Lock()
			dropped := r.dropped
			r.dropped = 0
			active = make([]entities.PluginAPIKeyIdentity, 0, min(len(r.pending), pluginAPIKeyBatchSize))
			for key, identity := range r.pending {
				active = append(active, identity)
				delete(r.pending, key)
				if len(active) == pluginAPIKeyBatchSize {
					break
				}
			}
			// 只有真的闲下来才清除窗口，新到身份不会被本轮完成操作删除。
			if len(r.pending) == 0 {
				r.windowStarted = time.Time{}
			}
			r.mu.Unlock()
			if dropped > 0 {
				logrus.WithField("rejected_identity_count", dropped).Warn("plugin API key registration memory limit reached; later usage may retry")
			}
			if len(active) == 0 {
				continue
			}
		}

		// 同时限制 reader、inbox 检查、等待 writer 连接和短事务，退出不会无限等待。
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, pluginAPIKeyRegistrationTimeout)
		result, err := repository.RegisterPluginAPIKeys(attemptCtx, r.db, active)
		cancelAttempt()
		remaining := make(map[string]struct{}, len(result.Remaining))
		for _, identity := range result.Remaining {
			remaining[identity.APIGroupKey] = struct{}{}
		}
		r.mu.Lock()
		for _, identity := range active {
			if _, retry := remaining[identity.APIGroupKey]; retry {
				continue
			}
			delete(r.accepted, identity.APIGroupKey)
			r.pendingBytes -= len(identity.APIGroupKey) + len(identity.AccessProvider)
		}
		r.mu.Unlock()
		active = result.Remaining
		if result.Conflicts > 0 {
			logrus.WithField("conflict_count", result.Conflicts).Error("plugin API key source conflict; existing records preserved")
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logrus.WithError(err).Error("plugin API key registration failed")
			if !waitPluginAPIKeyRetry(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, pluginAPIKeyErrorBackoffMax)
			continue
		}
		backoff = pluginAPIKeyErrorBackoffInitial
		if result.DeferredForInbox && !waitPluginAPIKeyRetry(ctx, pluginAPIKeyInboxRetryInterval) {
			return nil
		}
	}
	return nil
}

func waitPluginAPIKeyRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
