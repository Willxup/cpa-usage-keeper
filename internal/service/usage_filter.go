package service

import "time"

type UsageFilter struct {
	Range     string
	StartTime *time.Time
	EndTime   *time.Time
}
