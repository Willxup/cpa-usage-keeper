package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/timeutil"
)

func TestParseUsageQueryRangeNormalizesSupportedRanges(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	anchor := time.Date(2026, 7, 21, 12, 34, 56, 0, location)
	testCases := []struct {
		name          string
		rangeValue    string
		unitValue     string
		startValue    string
		endValue      string
		wantUnit      timeutil.UsageQueryUnit
		wantCount     int
		wantStart     time.Time
		wantEnd       time.Time
		wantExclusive bool
	}{
		{
			name:       "rolling hours",
			rangeValue: "8h",
			wantUnit:   timeutil.UsageQueryUnitHour,
			wantCount:  8,
			wantStart:  anchor.Add(-8 * time.Hour),
			wantEnd:    anchor,
		},
		{
			name:       "today",
			rangeValue: "today",
			wantUnit:   timeutil.UsageQueryUnitDay,
			wantCount:  1,
			wantStart:  time.Date(2026, 7, 21, 0, 0, 0, 0, location),
			wantEnd:    time.Date(2026, 7, 22, 0, 0, 0, 0, location).Add(-time.Nanosecond),
		},
		{
			name:          "custom hours",
			rangeValue:    "custom",
			unitValue:     "hour",
			startValue:    "2026-07-21T00:00:00+08:00",
			endValue:      "2026-07-21T04:00:00+08:00",
			wantUnit:      timeutil.UsageQueryUnitHour,
			wantCount:     5,
			wantStart:     time.Date(2026, 7, 21, 0, 0, 0, 0, location),
			wantEnd:       time.Date(2026, 7, 21, 5, 0, 0, 0, location),
			wantExclusive: true,
		},
		{
			name:          "custom calendar days",
			rangeValue:    "custom",
			unitValue:     "day",
			startValue:    "2026-07-19",
			endValue:      "2026-07-21",
			wantUnit:      timeutil.UsageQueryUnitDay,
			wantCount:     3,
			wantStart:     time.Date(2026, 7, 19, 0, 0, 0, 0, location),
			wantEnd:       time.Date(2026, 7, 22, 0, 0, 0, 0, location),
			wantExclusive: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := timeutil.ParseUsageQueryRange(
				testCase.rangeValue,
				testCase.unitValue,
				testCase.startValue,
				testCase.endValue,
				anchor,
			)
			if err != nil {
				t.Fatalf("ParseUsageQueryRange returned error: %v", err)
			}
			if result.Range != testCase.rangeValue || result.Unit != testCase.wantUnit || result.Count != testCase.wantCount {
				t.Fatalf("unexpected normalized identity: %+v", result)
			}
			if !result.StartTime.Equal(testCase.wantStart) || !result.EndTime.Equal(testCase.wantEnd) {
				t.Fatalf("unexpected normalized bounds: got %s..%s want %s..%s", result.StartTime, result.EndTime, testCase.wantStart, testCase.wantEnd)
			}
			if result.EndExclusive != testCase.wantExclusive {
				t.Fatalf("EndExclusive=%v, want %v", result.EndExclusive, testCase.wantExclusive)
			}
		})
	}
}

func TestParseUsageQueryRangeCountsCustomDaysAcrossDSTByCalendarDate(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	result, err := timeutil.ParseUsageQueryRange(
		"custom",
		"day",
		"2026-03-07",
		"2026-03-08",
		time.Date(2026, 3, 8, 12, 0, 0, 0, location),
	)
	if err != nil {
		t.Fatalf("ParseUsageQueryRange returned error: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("calendar day count=%d, want 2", result.Count)
	}
	if duration := result.EndTime.Sub(result.StartTime); duration != 47*time.Hour {
		t.Fatalf("DST-normalized bounds duration=%s, want 47h", duration)
	}
}

func TestParseUsageQueryRangeRejectsInvalidInputs(t *testing.T) {
	anchor := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	for _, testCase := range []struct {
		name       string
		rangeValue string
		unitValue  string
		startValue string
		endValue   string
	}{
		{name: "missing range"},
		{name: "unsupported rolling range", rangeValue: "31d"},
		{name: "custom missing bounds", rangeValue: "custom", unitValue: "day"},
		{name: "custom invalid unit", rangeValue: "custom", unitValue: "week", startValue: "2026-07-20", endValue: "2026-07-21"},
		{name: "custom hour too short", rangeValue: "custom", unitValue: "hour", startValue: "2026-07-21T00:00:00Z", endValue: "2026-07-21T03:00:00Z"},
		{name: "custom day before horizon", rangeValue: "custom", unitValue: "day", startValue: "2026-06-21", endValue: "2026-07-21"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := timeutil.ParseUsageQueryRange(testCase.rangeValue, testCase.unitValue, testCase.startValue, testCase.endValue, anchor); err == nil {
				t.Fatal("expected invalid query range to be rejected")
			}
		})
	}
}
