package repository

import "time"

type UsageQueryFilter struct {
	StartTime *time.Time
	EndTime   *time.Time
}
