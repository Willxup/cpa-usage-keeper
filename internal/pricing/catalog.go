package pricing

import "sync/atomic"

var sharedEmptySnapshot = &Snapshot{
	modelsByName: map[string]compiledModel{},
	modelConfigs: []ModelConfig{},
}

// EmptySnapshot 返回可显式注入的只读空价格快照。
func EmptySnapshot() *Snapshot {
	return sharedEmptySnapshot
}

// Catalog 原子保存当前价格快照，本身不执行数据库或网络 I/O。
type Catalog struct {
	current atomic.Pointer[Snapshot]
}

func NewCatalog(snapshot *Snapshot) *Catalog {
	catalog := &Catalog{}
	catalog.Replace(snapshot)
	return catalog
}

func (c *Catalog) Snapshot() *Snapshot {
	if c == nil {
		return sharedEmptySnapshot
	}
	snapshot := c.current.Load()
	if snapshot == nil {
		return sharedEmptySnapshot
	}
	return snapshot
}

// Replace 只发布已经完整编译的候选快照。
func (c *Catalog) Replace(snapshot *Snapshot) {
	if c == nil {
		return
	}
	if snapshot == nil {
		snapshot = sharedEmptySnapshot
	}
	c.current.Store(snapshot)
}

func (c *Catalog) NewResolver() Resolver {
	return Resolver{snapshot: c.Snapshot()}
}
