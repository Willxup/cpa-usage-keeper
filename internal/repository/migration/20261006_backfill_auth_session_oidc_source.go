package migration

import (
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// backfillAuthSessionOIDCSourceMigration 归一 auth_sessions.source 的取值。
// OIDC 登录引入 source=oidc；本迁移不新增列（source 列自 20260701 已存在），
// 仅把历史遗留的空值/异常值兜底为 standard，保证读取路径 NormalizeSessionSource 的数据一致性。
func backfillAuthSessionOIDCSourceMigration(tx *gorm.DB) error {
	if err := tx.Model(&entities.AuthSession{}).
		Where("source IS NULL OR TRIM(source) = '' OR source NOT IN ?", []string{
			string(auth.SessionSourceStandard),
			string(auth.SessionSourceEmbed),
			string(auth.SessionSourceOIDC),
		}).
		Update("source", string(auth.SessionSourceStandard)).Error; err != nil {
		return err
	}
	return nil
}
