package test

import (
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
)

func TestOverviewSpeedValidity(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		output, latency            int64
		ttft                       *int64
		failed                     bool
		wantSpeed, wantDecode      float64
		wantCount, wantDecodeCount int64
	}{
		{"normal", 61, 2000, new(int64(1000)), false, 30.5, 61, 1, 1},
		{"missing TTFT", 61, 2000, nil, false, 30.5, 0, 1, 0},
		{"zero TTFT", 61, 2000, new(int64(0)), false, 30.5, 0, 1, 0},
		{"negative TTFT", 61, 2000, new(int64(-1)), false, 30.5, 0, 1, 0},
		{"TTFT equals latency", 61, 2000, new(int64(2000)), false, 30.5, 0, 1, 0},
		{"TTFT exceeds latency", 61, 2000, new(int64(3000)), false, 30.5, 0, 1, 0},
		{"zero output", 0, 2000, new(int64(100)), false, 0, 0, 0, 0},
		{"negative output", -1, 2000, new(int64(100)), false, 0, 0, 0, 0},
		{"zero latency", 61, 0, new(int64(100)), false, 0, 0, 0, 0},
		{"negative latency", 61, -1, new(int64(100)), false, 0, 0, 0, 0},
		{"failed with output", 61, 2000, new(int64(1000)), true, 30.5, 61, 1, 1},
		{"one token not subtracted", 1, 3000, new(int64(1000)), false, 1.0 / 3, 0.5, 1, 1},
		{"integer limits", math.MaxInt64, math.MaxInt64, new(int64(math.MaxInt64 - 1)), false, 1000, float64(math.MaxInt64) * 1000, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := entities.UsageEvent{ID: 1, Model: "model", Timestamp: time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local), OutputTokens: tc.output, LatencyMS: tc.latency, TTFTMS: tc.ttft, Failed: tc.failed, CostUSD: new(float64(0)), CostAvailable: new(false)}
			hourly, daily, _, err := overview.BuildRows([]entities.UsageEvent{event})
			if err != nil {
				t.Fatal(err)
			}
			if len(hourly) != 1 || len(daily) != 1 {
				t.Fatal("missing buckets")
			}
			for _, got := range [][4]float64{
				{hourly[0].SpeedTPSSum, hourly[0].DecodeSpeedTPSSum, float64(hourly[0].SpeedSampleCount), float64(hourly[0].DecodeSpeedSampleCount)},
				{daily[0].SpeedTPSSum, daily[0].DecodeSpeedTPSSum, float64(daily[0].SpeedSampleCount), float64(daily[0].DecodeSpeedSampleCount)},
			} {
				want := [4]float64{tc.wantSpeed, tc.wantDecode, float64(tc.wantCount), float64(tc.wantDecodeCount)}
				for i := range got {
					if math.Abs(got[i]-want[i]) > 1e-12*math.Max(1, math.Abs(want[i])) {
						t.Fatalf("got %v want %v", got, want)
					}
				}
			}
		})
	}
}

func TestOverviewSpeedMergesRequestsNotDurationsOrBucketAverages(t *testing.T) {
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.Local)
	// 同一模型跨两小时且各桶样本数不等，日统计应等于逐请求速度之和。
	events := []entities.UsageEvent{
		{ID: 1, Model: "a", Timestamp: at, OutputTokens: 100, LatencyMS: 1000, TTFTMS: new(int64(500))},
		{ID: 2, Model: "a", Timestamp: at, OutputTokens: 100, LatencyMS: 9000, TTFTMS: new(int64(1000))},
		{ID: 3, Model: "a", Timestamp: at.Add(time.Hour), OutputTokens: 1, LatencyMS: 3000},
		{ID: 4, Model: "b", Timestamp: at, OutputTokens: 500, LatencyMS: 1000, TTFTMS: new(int64(500))},
	}
	for i := range events {
		events[i].CostUSD = new(float64(0))
		events[i].CostAvailable = new(true)
	}
	hourly, daily, _, err := overview.BuildRows(events)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	var count int64
	for _, row := range hourly {
		if row.Model == "a" {
			sum += row.SpeedTPSSum
			count += row.SpeedSampleCount
		}
	}
	want := 100 + 100.0/9 + 1.0/3
	if math.Abs(sum-want) > 1e-10 || count != 3 {
		t.Fatalf("hourly speed sum/count = %g/%d", sum, count)
	}
	for _, row := range daily {
		if row.Model == "a" {
			if math.Abs(row.SpeedTPSSum-want) > 1e-10 || row.SpeedSampleCount != 3 || math.Abs(row.DecodeSpeedTPSSum-212.5) > 1e-10 || row.DecodeSpeedSampleCount != 2 {
				t.Fatalf("daily stats = %+v", row)
			}
		}
	}
}
