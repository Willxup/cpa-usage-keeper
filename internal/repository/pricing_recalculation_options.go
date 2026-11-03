package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// LoadEarliestRetainedUsageHour returns the earliest retained event hour across hot
// and archive. Absolute seconds avoid ordering errors during DST transitions.
func LoadEarliestRetainedUsageHour(ctx context.Context, db *gorm.DB) (*time.Time, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	const epochSecond = "CAST(strftime('%s', substr(timestamp,1,19) || CASE WHEN substr(timestamp,-1) = 'Z' THEN 'Z' ELSE substr(timestamp,-6) END) AS INTEGER)"
	query := "SELECT MIN(" + epochSecond + "), COALESCE(SUM(CASE WHEN " + epochSecond + " IS NULL THEN 1 ELSE 0 END), 0) FROM (SELECT timestamp FROM usage_events UNION ALL SELECT timestamp FROM usage_events_archive)"
	var earliest sql.NullInt64
	var invalid int64
	if err := db.Clauses(dbresolver.Read).WithContext(ctx).Raw(query).Row().Scan(&earliest, &invalid); err != nil {
		return nil, fmt.Errorf("load earliest usage event: %w", err)
	}
	if invalid != 0 {
		return nil, fmt.Errorf("invalid stored usage event timestamp")
	}
	if !earliest.Valid {
		return nil, nil
	}
	hour := time.Unix(earliest.Int64, 0).Truncate(time.Hour).In(time.Local)
	return &hour, nil
}
