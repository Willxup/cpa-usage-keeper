package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addUsageEventTraceMetadataMigration(tx *gorm.DB) error {
	// 仅幂等增列，避免重建大表或扫描历史消息；缺省元数据继续保持 NULL。
	for _, table := range []struct {
		model any
		name  string
	}{
		{model: &entities.UsageEvent{}, name: "usage_events"},
		{model: &entities.UsageEventArchive{}, name: "usage_events_archive"},
	} {
		if !tx.Migrator().HasTable(table.model) {
			continue
		}
		for _, column := range []struct {
			name  string
			field string
		}{
			{name: "execution_id", field: "ExecutionID"},
			{name: "trace_id", field: "TraceID"},
			{name: "node_kind", field: "NodeKind"},
			{name: "is_fork", field: "IsFork"},
			{name: "is_compaction", field: "IsCompaction"},
		} {
			if tx.Migrator().HasColumn(table.model, column.name) {
				continue
			}
			if err := tx.Migrator().AddColumn(table.model, column.field); err != nil {
				return fmt.Errorf("add %s.%s column: %w", table.name, column.name, err)
			}
		}
	}
	return nil
}
