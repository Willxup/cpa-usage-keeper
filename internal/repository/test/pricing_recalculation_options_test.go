package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

// TestLoadEarliestHotUsageHourUsesAbsoluteTime 验证DST回拨和半小时时区都沿既有绝对整小时分桶。
func TestLoadEarliestHotUsageHourUsesAbsoluteTime(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		location   string
		timestamps []string
		want       string
	}{
		{
			name: "dst_fallback", location: "America/New_York",
			timestamps: []string{"2026-11-01T01:15:00-05:00", "2026-11-01T01:30:00-04:00"},
			want:       "2026-11-01T01:00:00-04:00",
		},
		{
			name: "half_hour_offset", location: "Asia/Kolkata",
			timestamps: []string{"2026-09-23T10:15:00+05:30"},
			want:       "2026-09-23T09:30:00+05:30",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			withRepositoryTestLocation(t, scenario.location)
			db := openTestDatabase(t)
			for index, raw := range scenario.timestamps {
				instant, err := time.Parse(time.RFC3339, raw)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&entities.UsageEvent{EventKey: scenario.name + string(rune('a'+index)), Timestamp: instant}).Error; err != nil {
					t.Fatal(err)
				}
			}
			hour, err := repository.LoadEarliestHotUsageHour(context.Background(), db)
			if err != nil || hour == nil || hour.Format(time.RFC3339) != scenario.want {
				t.Fatalf("earliest hot hour=%v err=%v, want %s", hour, err, scenario.want)
			}
		})
	}
}
