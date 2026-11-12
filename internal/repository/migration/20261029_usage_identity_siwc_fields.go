package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addUsageIdentitySIWCFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) {
		return nil
	}
	// 仅增 nullable 列，旧身份在首次成功同步前保留未知状态。
	for _, field := range []string{"SIWCBindSupported", "SIWCBound", "UseSIWC", "SIWCStatus", "SIWCExpiresAt"} {
		if tx.Migrator().HasColumn(&entities.UsageIdentity{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&entities.UsageIdentity{}, field); err != nil {
			return fmt.Errorf("add usage identity SIWC field %s: %w", field, err)
		}
	}
	return nil
}
