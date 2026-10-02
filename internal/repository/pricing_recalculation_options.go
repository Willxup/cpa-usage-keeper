package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// LoadEarliestHotUsageHour 按绝对秒找到热表最早事件所属的既有小时桶，供重算起点与热表范围取交集。
// SQLite 文本排序会把 DST 回拨的两次本地小时排错；同一秒内任一事件都属于同一绝对小时。
func LoadEarliestHotUsageHour(ctx context.Context, db *gorm.DB) (*time.Time, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	const epochSecond = "CAST(strftime('%s', substr(timestamp,1,19) || CASE WHEN substr(timestamp,-1) = 'Z' THEN 'Z' ELSE substr(timestamp,-6) END) AS INTEGER)"
	var raw string
	err := db.Clauses(dbresolver.Read).WithContext(ctx).Table("usage_events").
		Select("timestamp").Order(epochSecond + " ASC").Limit(1).Row().Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load earliest hot usage event: %w", err)
	}
	instant, err := timeutil.ParseStorageTime(raw)
	if err != nil {
		return nil, fmt.Errorf("parse earliest hot usage event: %w", err)
	}
	hour := instant.Truncate(time.Hour).In(time.Local)
	return &hour, nil
}
