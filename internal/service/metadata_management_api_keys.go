package service

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// syncManagementAPIKeys 同步 CPA 管理 API key 清单；原值只在本地保存，对外查询时再脱敏。
func syncManagementAPIKeys(db *gorm.DB, result *response.ManagementAPIKeysResult, fetchErr error, now time.Time) error {
	// fetch failure 没有完整新清单，必须保留本地 active keys。
	if fetchErr != nil {
		// 保持原有来源错误包装。
		return fmt.Errorf("fetch management api keys: %w", fetchErr)
	}
	// nil 数据库仍返回与旧实现一致的配置错误。
	if db == nil {
		// 不尝试任何持久化。
		return fmt.Errorf("database is nil")
	}
	// nil response 不是成功空列表，不能删除本地 keys。
	if result == nil {
		// 保持原有 nil response 错误文本。
		return fmt.Errorf("fetch management api keys: empty response")
	}
	// 成功响应交给原 repository，以本轮统一 now 执行独立事务替换。
	if err := repository.SyncCPAAPIKeys(db, result.Payload.APIKeys, now); err != nil {
		// repository failure 使用原 service 上下文包装。
		return fmt.Errorf("sync management api keys: %w", err)
	}
	// 成功空列表和非空列表都完成本轮同步。
	return nil
}
