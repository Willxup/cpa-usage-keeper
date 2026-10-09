package test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestAnalysisLatencyQueryMatchesPersistedRowsExactDTO(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, location)
	end := start.Add(4 * time.Hour)
	events := make([]entities.UsageEvent, 0, 3600)
	for index := 0; index < 3600; index++ {
		id := int64(index + 1)
		group := "target"
		if index%3 == 0 {
			group = "other"
		}
		events = append(events, validLatencyEvent(id, group, start.Add(time.Duration(index%4)*time.Hour+time.Duration(index/4)*time.Second), 10+id%113, 100+id%277))
	}
	rows, err := latency.BuildRows(events, end)
	if err != nil {
		t.Fatal(err)
	}
	hourRows := make([]entities.UsageLatencyStat, 0, 8)
	for _, row := range rows {
		if row.BucketType == entities.UsageLatencyBucketHour {
			hourRows = append(hourRows, row)
		}
	}
	if len(hourRows) != 8 {
		t.Fatalf("hour rows=%d, want 8", len(hourRows))
	}
	if err := db.Create(&hourRows).Error; err != nil {
		t.Fatal(err)
	}

	// 每个持久化桶都未触及 2500 上限；旧版查询合并逐行排序裁剪后的精确预期可由这些 BLOB 独立重建。
	all := make([]latency.SamplePoint, 0, 3600)
	ttft := latency.NewSketch()
	latencySketch := latency.NewSketch()
	var maxTTFT, maxLatency int64
	for _, row := range hourRows {
		decoded, err := latency.UnmarshalSampleSet(row.SamplePoints)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, decoded.Points()...)
		partTTFT, err := latency.UnmarshalSketch(row.TTFTSketch)
		if err != nil {
			t.Fatal(err)
		}
		partLatency, err := latency.UnmarshalSketch(row.LatencySketch)
		if err != nil {
			t.Fatal(err)
		}
		if err := ttft.Merge(partTTFT); err != nil {
			t.Fatal(err)
		}
		if err := latencySketch.Merge(partLatency); err != nil {
			t.Fatal(err)
		}
		maxTTFT = max(maxTTFT, row.MaxTTFTMS)
		maxLatency = max(maxLatency, row.MaxLatencyMS)
	}
	slices.SortFunc(all, func(a, b latency.SamplePoint) int {
		if a.Priority < b.Priority {
			return -1
		}
		if a.Priority > b.Priority {
			return 1
		}
		if a.EventID < b.EventID {
			return -1
		}
		if a.EventID > b.EventID {
			return 1
		}
		return 0
	})
	want := repodto.AnalysisLatencyDiagnosticsRecord{
		Points:      make([]repodto.AnalysisLatencyPointRecord, 0, latency.MaxSamplePoints),
		Density:     make([]repodto.AnalysisLatencyDensityCellRecord, 0),
		TotalPoints: 3600, Sampled: true,
		P95TTFTMS: ttft.P95(), P95LatencyMS: latencySketch.P95(),
		MaxTTFTMS: maxTTFT, MaxLatencyMS: maxLatency,
	}
	for _, point := range all[:latency.MaxSamplePoints] {
		want.Points = append(want.Points, repodto.AnalysisLatencyPointRecord{TTFTMS: point.TTFTMS, LatencyMS: point.LatencyMS})
	}
	got, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query DTO differs from persisted-row baseline: got=%+v want=%+v", struct {
			Total                  int64
			P95T, P95L, MaxT, MaxL int64
			Sampled                bool
			Points                 int
		}{got.TotalPoints, got.P95TTFTMS, got.P95LatencyMS, got.MaxTTFTMS, got.MaxLatencyMS, got.Sampled, len(got.Points)}, struct {
			Total                  int64
			P95T, P95L, MaxT, MaxL int64
			Sampled                bool
			Points                 int
		}{want.TotalPoints, want.P95TTFTMS, want.P95LatencyMS, want.MaxTTFTMS, want.MaxLatencyMS, want.Sampled, len(want.Points)})
	}
}
