package service

import "sync"

// CredentialMetadataCoordinator 在同一进程的同步与凭据修改服务间共享；零值可用。
// 只锁住版本检查和必要本地写入，不跨远端请求、聚合或通知持锁。
type CredentialMetadataCoordinator struct {
	mu      sync.Mutex
	version uint64
}

func (c *CredentialMetadataCoordinator) snapshotVersion() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

func (c *CredentialMetadataCoordinator) persistSnapshot(version uint64, persist func() error) (bool, error) {
	if c == nil {
		return true, persist()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version != version {
		return false, nil
	}
	return true, persist()
}

// completeMutation 仅在远端成功后调用；即使本地写入失败，也必须先使旧快照失效。
func (c *CredentialMetadataCoordinator) completeMutation(persist func() error) error {
	if c != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.version++
	}
	if persist != nil {
		return persist()
	}
	return nil
}
