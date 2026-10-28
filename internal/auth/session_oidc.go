package auth

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const oidcSessionDeleteBatchSize = 200

// RevokeInvalidOIDCSessions 在启动构造会话缓存前执行；同一事务内完成撤销，失败不得进入业务就绪。
func (s *GormSessionStore) RevokeInvalidOIDCSessions(policy OIDCSessionPolicy) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("auth session store is not configured")
	}
	var deleted int64
	var identityInvalid, subjectDenied int64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 来源索引限定 OIDC；基础身份不匹配时直接批量删除，不加载会话列表或活动元数据。
		invalid := tx.Where("source = ?", string(SessionSourceOIDC))
		if policy.Enabled {
			invalid = invalid.Where(`role IS NULL OR role <> ?
				OR oidc_issuer IS NULL OR oidc_issuer = '' OR oidc_issuer <> ?
				OR oidc_client_id IS NULL OR oidc_client_id = '' OR oidc_client_id <> ?
				OR oidc_subject IS NULL OR oidc_subject = ''`, string(RoleAdmin), policy.Issuer, policy.ClientID)
		}
		result := invalid.Unscoped().Delete(&entities.AuthSession{})
		if result.Error != nil {
			return result.Error
		}
		deleted += result.RowsAffected
		if policy.Enabled {
			identityInvalid = result.RowsAffected
		}
		if !policy.Enabled || len(policy.AllowedSubjects) == 0 {
			return nil
		}

		// 准入列表在内存中精确匹配，避免大量 subject 撑破 SQL 参数上限；仅读取哈希与 subject。
		allowed := make(map[string]struct{}, len(policy.AllowedSubjects))
		for _, subject := range policy.AllowedSubjects {
			allowed[subject] = struct{}{}
		}
		var rows []struct {
			TokenHash   string
			OIDCSubject string `gorm:"column:oidc_subject"`
		}
		if err := tx.Model(&entities.AuthSession{}).Select("token_hash", "oidc_subject").Where("source = ?", string(SessionSourceOIDC)).Find(&rows).Error; err != nil {
			return err
		}
		hashes := make([]string, 0, oidcSessionDeleteBatchSize)
		flush := func() error {
			if len(hashes) == 0 {
				return nil
			}
			result := tx.Unscoped().Where("source = ? AND token_hash IN ?", string(SessionSourceOIDC), hashes).Delete(&entities.AuthSession{})
			deleted += result.RowsAffected
			subjectDenied += result.RowsAffected
			hashes = hashes[:0]
			return result.Error
		}
		for _, row := range rows {
			if _, ok := allowed[row.OIDCSubject]; ok {
				continue
			}
			hashes = append(hashes, row.TokenHash)
			if len(hashes) == oidcSessionDeleteBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return flush()
	})
	if err != nil {
		return 0, err
	}
	// 事务提交后才报告真实撤销数量；复用删除结果，不增加查询或输出会话身份。
	if deleted > 0 {
		fields := logrus.Fields{"revoked": deleted}
		if !policy.Enabled {
			fields["reason"] = "oidc_disabled"
		} else {
			fields["reason"] = "session_policy_mismatch"
			fields["identity_invalid"] = identityInvalid
			fields["subject_denied"] = subjectDenied
		}
		logrus.WithFields(fields).Info("Revoked OIDC sessions during startup")
	}
	return deleted, nil
}
