package service

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// FinishPricingRecalculation 在重算成功或失败后，按已提交费用统一处理缓存并释放本次暂停许可。
// 调用前事件处理、普通聚合已停稳且 recent 追加已排空；本函数不计价、不回滚费用，也不清理健康桶。
// recent 重载失败时该缓存自行回退 DB；仍清空额度缓存并执行 release，返回原错误供任务报告失败。
// release 由协调器提供，恢复已取得的聚合、处理／维护及费用读取许可；调用返回后才可发布任务结束状态。
// 未启用的缓存可传 nil；不创建后台任务或重试，不使用新的数据库事务。
func FinishPricingRecalculation(ctx context.Context, db *gorm.DB, recent *repository.UsageRecentEventCache, quotas *quota.Service, release func()) error {
	if release != nil {
		defer release()
	}
	var reloadErr error
	if recent != nil {
		if err := recent.ReloadStoredCostEvents(ctx, db); err != nil {
			reloadErr = fmt.Errorf("reload recent stored costs: %w", err)
		}
	}
	// 额度失效是纯内存操作；即使 DB 读取失败或生命周期已取消，也不能留下旧费用结果。
	if quotas != nil {
		quotas.InvalidateStoredCostCache()
	}
	return reloadErr
}
