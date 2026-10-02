package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cpa-usage-keeper/internal/poller"
)

// UsageIngestBridge 在同一个接收 runner 上移交 inbox 写入和 metadata 通知。
// 切换等待正在进行的 Insert 完成；远端已取出的批次仍会沿原生命周期写入同一个 inbox。
type UsageIngestBridge struct {
	mu       sync.RWMutex
	writer   poller.RedisInboxWriter
	observer poller.RedisControlMessageObserver
}

// NewUsageIngestBridge 用引导期旧列 writer 建立唯一接收 runner 的写入边界。
func NewUsageIngestBridge(writer poller.RedisInboxWriter) *UsageIngestBridge {
	return &UsageIngestBridge{writer: writer}
}

// Insert 持有读锁到原始消息落盘结束，防止写入半批时替换目标列合同。
func (b *UsageIngestBridge) Insert(ctx context.Context, source string, messages []string, receivedAt time.Time) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.writer == nil {
		return 0, fmt.Errorf("usage inbox writer is missing")
	}
	return b.writer.Insert(ctx, source, messages, receivedAt)
}

// ActivateNormalWriter 等旧批提交后改用标准 source 列；控制消息此后才触发业务 metadata。
func (b *UsageIngestBridge) ActivateNormalWriter(writer poller.RedisInboxWriter, observer poller.RedisControlMessageObserver) error {
	if writer == nil || observer == nil {
		return fmt.Errorf("normal usage ingest dependencies are missing")
	}
	b.mu.Lock()
	b.writer, b.observer = writer, observer
	b.mu.Unlock()
	observer.NotifyIngestConnected()
	return nil
}

// NotifyIngestConnected 仅在标准业务阶段转发连接通知，引导接收不触发 metadata。
func (b *UsageIngestBridge) NotifyIngestConnected() {
	b.withObserver(func(observer poller.RedisControlMessageObserver) { observer.NotifyIngestConnected() })
}

// MarkRefreshSupported 只在业务阶段传播 CPA 的刷新能力。
func (b *UsageIngestBridge) MarkRefreshSupported() {
	b.withObserver(func(observer poller.RedisControlMessageObserver) { observer.MarkRefreshSupported() })
}

// RequestMetadataRefresh 避免旧库迁移期间读取尚未准备的 metadata 表。
func (b *UsageIngestBridge) RequestMetadataRefresh() {
	b.withObserver(func(observer poller.RedisControlMessageObserver) { observer.RequestMetadataRefresh() })
}

// MarkRefreshPollingRequired 在业务阶段保留原有的刷新回退通知。
func (b *UsageIngestBridge) MarkRefreshPollingRequired(reason string) {
	b.withObserver(func(observer poller.RedisControlMessageObserver) { observer.MarkRefreshPollingRequired(reason) })
}

// withObserver 让控制通知与写入目标同一切换边界；引导阶段静默过滤，不触发旧表业务读取。
func (b *UsageIngestBridge) withObserver(call func(poller.RedisControlMessageObserver)) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.observer != nil {
		call(b.observer)
	}
}
