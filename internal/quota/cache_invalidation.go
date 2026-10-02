package quota

import "time"

// InvalidateStoredCostCache 在费用重算结束后清除整份额度结果，并让旧异步工作失去发布资格。
// 仅改变内存额度状态；历史 periods/segments、上游刷新时间和 Header history 均不受影响。
func (s *Service) InvalidateStoredCostCache() {
	if s == nil {
		return
	}
	s.refreshMu.Lock()
	s.cacheGeneration++
	s.refreshTasks = make(map[string]*RefreshTaskRecord)
	s.nextRefreshTaskCleanupAt = time.Time{}
	// 巡检轮次与任务在同一锁内失效；GetInspectionStatus 不能把旧轮次补报完成。
	s.inspectionRoundActive = false
	s.inspectionRoundAuthIndexSet = nil
	// 锁顺序固定为 refreshMu → usageHeaderMu，防止清理与 Header 入队互相交错。
	s.usageHeaderMu.Lock()
	s.usageHeaderPending = make(map[string]*UsageHeaderSnapshot, usageHeaderPendingIdentityLimit)
	s.usageHeaderMu.Unlock()
	s.refreshMu.Unlock()
}

// quotaCacheGeneration 为异步刷新和 Header 工作固定启动代数；读取必须受 refreshMu 保护。
func (s *Service) quotaCacheGeneration() uint64 {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.cacheGeneration
}
