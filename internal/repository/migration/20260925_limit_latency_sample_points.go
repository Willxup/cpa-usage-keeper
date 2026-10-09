package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository/latencystore"

	"gorm.io/gorm"
)

// limitLatencySamplePointsMigration 保留全部统计量，只把每个旧桶的稳定样本缩至前 1000 点。
// 调用方以默认外层事务同时保护全部分页和版本标记，事务前执行通用数据库备份。
func limitLatencySamplePointsMigration(tx *gorm.DB) error {
	const pageSize = 200
	var lastID int64
	for {
		var rows []entities.UsageLatencyStat
		if err := tx.Where("id > ?", lastID).Order("id ASC").Limit(pageSize).Find(&rows).Error; err != nil {
			return fmt.Errorf("load latency sample page after id %d: %w", lastID, err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			// 查询合并器完整校验旧 BLOB 的全部 2500 点、两个 Sketch 和行计数，再保留前 1000 点。
			aggregate, err := latencystore.MergeDiagnosticsRows([]entities.UsageLatencyStat{row})
			if err != nil {
				return fmt.Errorf("validate latency row %d: %w", row.ID, err)
			}
			// 原样保留未超限的 BLOB，包含旧格式允许的非最短 varint 编码。
			original, err := latency.UnmarshalSampleSet(row.SamplePoints)
			if err != nil {
				return fmt.Errorf("decode latency row %d samples: %w", row.ID, err)
			}
			if original.Count() <= latency.MaxSamplePoints {
				continue
			}
			encoded, err := aggregate.SamplePoints.MarshalBinary()
			if err != nil {
				return fmt.Errorf("encode latency row %d samples: %w", row.ID, err)
			}
			// UpdateColumn 只修改这个 BLOB，不触发 GORM 的 updated_at 自动更新。
			result := tx.Model(&entities.UsageLatencyStat{}).Where("id = ?", row.ID).UpdateColumn("sample_points", encoded)
			if result.Error != nil {
				return fmt.Errorf("update latency row %d samples: %w", row.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("update latency row %d samples affected %d rows", row.ID, result.RowsAffected)
			}
		}
		lastID = rows[len(rows)-1].ID
	}
}
