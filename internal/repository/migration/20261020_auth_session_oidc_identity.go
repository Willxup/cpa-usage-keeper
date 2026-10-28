package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// addAuthSessionOIDCIdentityMigration 只增可空列，旧会话和业务历史不做身份猜测或回填。
func addAuthSessionOIDCIdentityMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.AuthSession{}) {
		return nil
	}
	for _, field := range []string{"OIDCIssuer", "OIDCSubject", "OIDCClientID"} {
		if tx.Migrator().HasColumn(&entities.AuthSession{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&entities.AuthSession{}, field); err != nil {
			return fmt.Errorf("add auth_sessions OIDC identity column: %w", err)
		}
	}
	return nil
}
