package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addAuthSessionClientMetadataMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.AuthSession{}) {
		return nil
	}
	columns := []struct {
		name  string
		field string
	}{
		{name: "login_ip", field: "LoginIP"},
		{name: "last_seen_ip", field: "LastSeenIP"},
		{name: "user_agent", field: "UserAgent"},
		{name: "last_seen_at", field: "LastSeenAt"},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.AuthSession{}, column.name) {
			continue
		}
		if err := tx.Migrator().AddColumn(&entities.AuthSession{}, column.field); err != nil {
			return fmt.Errorf("add auth_sessions.%s column: %w", column.name, err)
		}
	}
	if err := tx.Model(&entities.AuthSession{}).
		Where("last_seen_at IS NULL").
		Update("last_seen_at", gorm.Expr("created_at")).Error; err != nil {
		return fmt.Errorf("backfill auth_sessions.last_seen_at: %w", err)
	}
	return nil
}
