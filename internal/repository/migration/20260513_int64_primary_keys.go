package migration

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type int64PrimaryKeyColumn struct {
	Name string `gorm:"column:name"`
	Type string `gorm:"column:type"`
	PK   int    `gorm:"column:pk"`
}

func useInt64PrimaryKeysMigration(tx *gorm.DB) error {
	for _, table := range []string{"usage_events", "usage_identities", "redis_usage_inboxes", "model_price_settings"} {
		ok, err := tableHasInt64PrimaryKey(tx, table)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("table %s id column is not an integer primary key", table)
		}
	}
	return nil
}

func tableHasInt64PrimaryKey(tx *gorm.DB, table string) (bool, error) {
	var columns []int64PrimaryKeyColumn
	if err := tx.Raw(fmt.Sprintf("PRAGMA table_info(%s)", table)).Scan(&columns).Error; err != nil {
		return false, fmt.Errorf("inspect %s schema: %w", table, err)
	}
	if len(columns) == 0 {
		return true, nil
	}
	for _, column := range columns {
		if strings.EqualFold(column.Name, "id") {
			return column.PK > 0 && strings.Contains(strings.ToUpper(column.Type), "INT"), nil
		}
	}
	return false, nil
}
