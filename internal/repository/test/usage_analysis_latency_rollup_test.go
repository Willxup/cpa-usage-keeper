package test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

func TestAnalysisLatencyRollupCustomDayAlwaysUsesDayBuckets(t *testing.T) {
	testCases := []struct {
		name         string
		timezone     string
		start        func(*time.Location) time.Time
		calendarDays int
		now          func(*time.Location, time.Time) time.Time
		seedHour     bool
		wantTTFT     int64
	}{
		{
			name: "one day including today", timezone: "Asia/Shanghai", calendarDays: 1,
			start: func(location *time.Location) time.Time {
				return time.Date(2026, 7, 26, 0, 0, 0, 0, location)
			},
			now: func(_ *time.Location, end time.Time) time.Time {
				return end.Add(-14 * time.Hour)
			},
			seedHour: true, wantTTFT: 1100,
		},
		{
			name: "thirty days in Asia Shanghai", timezone: "Asia/Shanghai", calendarDays: 30,
			start: func(location *time.Location) time.Time {
				return time.Date(2026, 6, 1, 0, 0, 0, 0, location)
			},
			seedHour: true, wantTTFT: 2300,
		},
		{
			name: "thirty one days in Asia Shanghai", timezone: "Asia/Shanghai", calendarDays: 31,
			start: func(location *time.Location) time.Time {
				return time.Date(2026, 6, 1, 0, 0, 0, 0, location)
			},
			seedHour: true, wantTTFT: 3310,
		},
		{
			name: "three hundred sixty five days", timezone: "Asia/Shanghai", calendarDays: 365,
			start: func(location *time.Location) time.Time {
				return time.Date(2025, 7, 27, 0, 0, 0, 0, location)
			},
			seedHour: true, wantTTFT: 4365,
		},
		{
			name: "thirty days across DST fallback", timezone: "America/New_York", calendarDays: 30,
			start: func(location *time.Location) time.Time {
				return time.Date(2026, 10, 20, 0, 0, 0, 0, location)
			},
			seedHour: true, wantTTFT: 5330,
		},
		{
			name: "historical single day with only retained daily data", timezone: "Asia/Shanghai", calendarDays: 1,
			start: func(location *time.Location) time.Time {
				return time.Date(2026, 3, 28, 0, 0, 0, 0, location)
			},
			now: func(location *time.Location, _ time.Time) time.Time {
				return time.Date(2026, 7, 26, 10, 0, 0, 0, location)
			},
			wantTTFT: 6120,
		},
	}

	for caseIndex, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, testCase.timezone)
			db := openTestDatabase(t)
			start := testCase.start(location)
			end := start.AddDate(0, 0, testCase.calendarDays)
			now := end.Add(-time.Second)
			if testCase.now != nil {
				now = testCase.now(location, end)
			}
			if testCase.seedHour {
				hourTTFT := testCase.wantTTFT + 1
				hourEventTime := end.Add(-time.Hour)
				if hourEventTime.After(now) {
					hourEventTime = now.Add(-time.Hour)
				}
				seedAnalysisLatencyRows(t, db, now, entities.UsageLatencyBucketHour, []entities.UsageEvent{
					analysisLatencyEvent(int64(100+caseIndex*10), "target", hourEventTime, hourTTFT, hourTTFT*10),
				})
			}
			seedAnalysisLatencyRows(t, db, now, entities.UsageLatencyBucketDay, []entities.UsageEvent{
				analysisLatencyEvent(int64(101+caseIndex*10), "target", start.Add(time.Hour), testCase.wantTTFT, testCase.wantTTFT*10),
			})

			diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
				Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true,
			})
			if err != nil {
				t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
			}
			assertSingleAnalysisLatencyPoint(t, diagnostics, testCase.wantTTFT, testCase.wantTTFT*10)
		})
	}
}

func TestAnalysisLatencyRollupSupportsAllAnalysisRangeKinds(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")

	t.Run("custom hour keeps exact aligned bucket range", func(t *testing.T) {
		db := openTestDatabase(t)
		start := time.Date(2026, 7, 26, 8, 0, 0, 0, location)
		end := time.Date(2026, 7, 26, 14, 0, 0, 0, location)
		events := []entities.UsageEvent{
			analysisLatencyEvent(2101, "target", start.Add(-time.Hour).Add(10*time.Minute), 7, 70),
			analysisLatencyEvent(2102, "target", start.Add(10*time.Minute), 8, 80),
			analysisLatencyEvent(2103, "target", end.Add(-time.Hour).Add(10*time.Minute), 13, 130),
			analysisLatencyEvent(2104, "target", end.Add(10*time.Minute), 14, 140),
		}
		seedAnalysisLatencyRows(t, db, end.Add(time.Hour), entities.UsageLatencyBucketHour, events)
		seedAnalysisLatencyRows(t, db, end.Add(time.Hour), entities.UsageLatencyBucketDay, []entities.UsageEvent{
			analysisLatencyEvent(2110, "target", start.Add(30*time.Minute), 999, 9990),
		})

		diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
			Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
		})
		if err != nil {
			t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
		}
		assertAnalysisLatencyPointSet(t, diagnostics, [][2]int64{{8, 80}, {13, 130}})
	})

	for caseIndex, testCase := range []struct {
		name      string
		rangeName string
		start     time.Time
		end       time.Time
		wantDaily bool
		skipHour  bool
	}{
		{
			name: "rolling thirty days", rangeName: "30d",
			start:     time.Date(2026, 6, 26, 10, 30, 0, 0, location),
			end:       time.Date(2026, 7, 26, 10, 30, 0, 0, location),
			wantDaily: true,
			skipHour:  true,
		},
		{
			name: "rolling two days", rangeName: "2d",
			start:     time.Date(2026, 7, 24, 10, 30, 0, 0, location),
			end:       time.Date(2026, 7, 26, 10, 30, 0, 0, location),
			wantDaily: true,
		},
		{
			name: "rolling twenty four hours", rangeName: "24h",
			start: time.Date(2026, 7, 25, 10, 30, 0, 0, location),
			end:   time.Date(2026, 7, 26, 10, 30, 0, 0, location),
		},
		{
			name: "today", rangeName: "today",
			start: time.Date(2026, 7, 26, 0, 0, 0, 0, location),
			end:   time.Date(2026, 7, 26, 23, 59, 59, int(time.Second-time.Nanosecond), location),
		},
		{
			name: "yesterday", rangeName: "yesterday",
			start: time.Date(2026, 7, 25, 0, 0, 0, 0, location),
			end:   time.Date(2026, 7, 25, 23, 59, 59, int(time.Second-time.Nanosecond), location),
		},
	} {
		bucketName := "hour"
		if testCase.wantDaily {
			bucketName = "day"
		}
		t.Run(testCase.name+" uses "+bucketName+" buckets", func(t *testing.T) {
			db := openTestDatabase(t)
			hourTTFT := int64(2600 + caseIndex*100)
			dayTTFT := hourTTFT + 1
			eventTime := testCase.start.Add(12*time.Hour + 10*time.Minute)
			if !testCase.skipHour {
				seedAnalysisLatencyRows(t, db, testCase.end, entities.UsageLatencyBucketHour, []entities.UsageEvent{
					analysisLatencyEvent(int64(2201+caseIndex*10), "target", eventTime, hourTTFT, hourTTFT*10),
				})
			}
			seedAnalysisLatencyRows(t, db, testCase.end, entities.UsageLatencyBucketDay, []entities.UsageEvent{
				analysisLatencyEvent(int64(2202+caseIndex*10), "target", eventTime, dayTTFT, dayTTFT*10),
			})

			diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
				Range: testCase.rangeName, StartTime: &testCase.start, EndTime: &testCase.end,
			})
			if err != nil {
				t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
			}
			wantTTFT := hourTTFT
			if testCase.wantDaily {
				wantTTFT = dayTTFT
			}
			assertSingleAnalysisLatencyPoint(t, diagnostics, wantTTFT, wantTTFT*10)
		})
	}
}

func TestAnalysisLatencyRollupAlignsRollingBoundsUpAndNeverQueriesUsageEvents(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 26, 9, 14, 21, 0, location)
	end := time.Date(2026, 7, 26, 11, 14, 21, 0, location)
	seedAnalysisLatencyRows(t, db, end, entities.UsageLatencyBucketHour, []entities.UsageEvent{
		analysisLatencyEvent(3001, "target", time.Date(2026, 7, 26, 9, 30, 0, 0, location), 9, 90),
		analysisLatencyEvent(3002, "target", time.Date(2026, 7, 26, 10, 30, 0, 0, location), 10, 100),
		analysisLatencyEvent(3003, "target", time.Date(2026, 7, 26, 11, 10, 0, 0, location), 11, 110),
		analysisLatencyEvent(3004, "target", time.Date(2026, 7, 26, 12, 10, 0, 0, location), 12, 120),
	})
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}
	queries := captureAnalysisLatencyQueries(t, db)

	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		Range: "2h", StartTime: &start, EndTime: &end,
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	assertAnalysisLatencyPointSet(t, diagnostics, [][2]int64{{10, 100}, {11, 110}})

	joined := strings.Join(*queries, "\n")
	if strings.Contains(joined, "usage_events") {
		t.Fatalf("Latency rollup query must not access usage_events:\n%s", joined)
	}
	if !strings.Contains(joined, "usage_latency_stats") || !strings.Contains(joined, "bucket_type") || !strings.Contains(joined, "bucket_start") {
		t.Fatalf("expected a bounded usage_latency_stats query, got:\n%s", joined)
	}
}

func TestAnalysisLatencyRollupFiltersAPIKeyAndReturnsEmptySlices(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, location)
	end := start.Add(2 * time.Hour)
	seedAnalysisLatencyRows(t, db, end, entities.UsageLatencyBucketHour, []entities.UsageEvent{
		analysisLatencyEvent(4001, "target", start.Add(30*time.Minute), 1010, 10010),
		analysisLatencyEvent(4002, "other", start.Add(30*time.Minute), 202, 2002),
	})

	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, APIGroupKey: "target",
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	assertSingleAnalysisLatencyPoint(t, diagnostics, 1010, 10010)

	empty, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, APIGroupKey: "missing",
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned empty-range error: %v", err)
	}
	if empty.TotalPoints != 0 || empty.Sampled || len(empty.Points) != 0 || empty.Points == nil || len(empty.Density) != 0 || empty.Density == nil {
		t.Fatalf("expected compatible empty diagnostics with non-nil slices, got %+v", empty)
	}
}

func TestAnalysisLatencyRollupMapsBoundedSamplesSketchesAndExactMax(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, location)
	end := start.Add(time.Hour)
	events := make([]entities.UsageEvent, 0, 2600)
	for index := 0; index < 2600; index++ {
		value := int64(index + 1)
		events = append(events, analysisLatencyEvent(value, "target", start.Add(time.Duration(index)*time.Second), value, value*10))
	}
	seedAnalysisLatencyRows(t, db, end, entities.UsageLatencyBucketHour, events)

	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	if diagnostics.TotalPoints != 2600 || !diagnostics.Sampled || len(diagnostics.Points) != latency.MaxSamplePoints {
		t.Fatalf("expected 2600 total and %d bounded samples, got total=%d sampled=%v points=%d", latency.MaxSamplePoints, diagnostics.TotalPoints, diagnostics.Sampled, len(diagnostics.Points))
	}
	if diagnostics.MaxTTFTMS != 2600 || diagnostics.MaxLatencyMS != 26000 {
		t.Fatalf("expected exact max values, got ttft=%d latency=%d", diagnostics.MaxTTFTMS, diagnostics.MaxLatencyMS)
	}
	assertAnalysisLatencyP95Close(t, diagnostics.P95TTFTMS, 2470)
	assertAnalysisLatencyP95Close(t, diagnostics.P95LatencyMS, 24700)
	for _, point := range diagnostics.Points {
		if point.LatencyMS != point.TTFTMS*10 {
			t.Fatalf("sample pair was split while mapping diagnostics: %+v", point)
		}
	}
	if diagnostics.Density == nil || len(diagnostics.Density) != 0 {
		t.Fatalf("expected compatible empty density array, got %+v", diagnostics.Density)
	}
}

func TestAnalysisLatencyRollupExplicitlyUsesReaderWhenWriterIsOccupied(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")
	db, reader, closePools := openLatencyReaderPoolTestDatabase(t)
	t.Cleanup(closePools)
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, location)
	end := start.Add(2 * time.Hour)
	seedAnalysisLatencyRows(t, db, end, entities.UsageLatencyBucketHour, []entities.UsageEvent{
		analysisLatencyEvent(5001, "target", start.Add(30*time.Minute), 88000, 880000),
	})

	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader pool: %v", err)
	}
	assertAnalysisLatencyReaderPoolLimits(t, readerSQL)
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer pool: %v", err)
	}
	heldWriter, err := writerSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold writer connection: %v", err)
	}
	t.Cleanup(func() { _ = heldWriter.Close() })

	queryContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// 调用方先标记 Write，生产入口必须显式覆盖为 Reader，才能绕过被占用的唯一 Writer。
	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db.Clauses(dbresolver.Write).WithContext(queryContext), repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, APIGroupKey: "target",
	})
	if err != nil {
		t.Fatalf("expected latency query to bypass occupied writer: %v", err)
	}
	assertSingleAnalysisLatencyPoint(t, diagnostics, 88000, 880000)
}

func TestAnalysisLatencyRollupRejectsInvalidRangesAndCorruptPayloads(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, location)
	end := start.Add(time.Hour)

	invalidFilters := []repodto.UsageQueryFilter{
		{EndTime: &end},
		{StartTime: &start},
		{StartTime: &start, EndTime: &start},
		{StartTime: &end, EndTime: &start},
	}
	for index, filter := range invalidFilters {
		if diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, filter); err == nil {
			t.Fatalf("invalid filter %d returned no error: %+v", index, diagnostics)
		}
	}
	if diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(nil, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end}); err == nil {
		t.Fatalf("nil database returned no error: %+v", diagnostics)
	}

	seedAnalysisLatencyRows(t, db, end, entities.UsageLatencyBucketHour, []entities.UsageEvent{
		analysisLatencyEvent(6001, "target", start.Add(10*time.Minute), 60, 600),
	})
	if err := db.Clauses(dbresolver.Write).Model(&entities.UsageLatencyStat{}).Where("bucket_type = ?", entities.UsageLatencyBucketHour).Update("format_version", latency.FormatVersion+1).Error; err != nil {
		t.Fatalf("corrupt latency row: %v", err)
	}
	if diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
	}); err == nil {
		t.Fatalf("corrupt latency payload returned no error or partial-result guard: %+v", diagnostics)
	}
}

func seedAnalysisLatencyRows(t *testing.T, db *gorm.DB, now time.Time, bucketType entities.UsageLatencyBucketType, events []entities.UsageEvent) {
	t.Helper()
	rows, err := latency.BuildRows(events, now)
	if err != nil {
		t.Fatalf("build latency rows: %v", err)
	}
	selected := make([]entities.UsageLatencyStat, 0, len(rows))
	for _, row := range rows {
		if row.BucketType == bucketType {
			selected = append(selected, row)
		}
	}
	if len(selected) == 0 {
		t.Fatalf("BuildRows returned no %s rows for %+v", bucketType, events)
	}
	if err := db.Clauses(dbresolver.Write).Create(&selected).Error; err != nil {
		t.Fatalf("seed %s latency rows: %v", bucketType, err)
	}
}

func analysisLatencyEvent(id int64, apiGroupKey string, timestamp time.Time, ttftMS, latencyMS int64) entities.UsageEvent {
	generate := true
	return entities.UsageEvent{
		ID: id, APIGroupKey: apiGroupKey, Timestamp: timestamp, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: latencyMS,
	}
}

func setAnalysisLatencyTestTimezone(t *testing.T, name string) *time.Location {
	t.Helper()
	previous := time.Local
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load timezone %s: %v", name, err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	return location
}

func captureAnalysisLatencyQueries(t *testing.T, db *gorm.DB) *[]string {
	t.Helper()
	queries := make([]string, 0, 2)
	callbackName := fmt.Sprintf("test:capture_analysis_latency_%p", db)
	capture := func(tx *gorm.DB) {
		query := strings.ToLower(tx.Statement.SQL.String())
		queries = append(queries, query)
		if strings.Contains(query, "usage_events") {
			tx.AddError(fmt.Errorf("usage_events query is forbidden"))
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, capture); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callbackName, capture); err != nil {
		t.Fatalf("register row callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
		_ = db.Callback().Row().Remove(callbackName)
	})
	return &queries
}

func assertAnalysisLatencyReaderPoolLimits(t *testing.T, readerSQL *sql.DB) {
	t.Helper()
	if stats := readerSQL.Stats(); stats.MaxOpenConnections != 8 {
		t.Fatalf("expected reader MaxOpenConns=8, got %+v", stats)
	}
	connections := make([]*sql.Conn, 0, 8)
	for index := 0; index < 8; index++ {
		connection, err := readerSQL.Conn(context.Background())
		if err != nil {
			t.Fatalf("open reader connection %d: %v", index, err)
		}
		connections = append(connections, connection)
	}
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			t.Fatalf("close reader connection: %v", err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for {
		stats := readerSQL.Stats()
		if stats.OpenConnections == 4 && stats.Idle == 4 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected reader MaxIdleConns=4 after warmup, got %+v", stats)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertSingleAnalysisLatencyPoint(t *testing.T, diagnostics repodto.AnalysisLatencyDiagnosticsRecord, ttftMS, latencyMS int64) {
	t.Helper()
	assertAnalysisLatencyPointSet(t, diagnostics, [][2]int64{{ttftMS, latencyMS}})
	if diagnostics.MaxTTFTMS != ttftMS || diagnostics.MaxLatencyMS != latencyMS {
		t.Fatalf("unexpected exact max values: %+v", diagnostics)
	}
	assertAnalysisLatencyP95Close(t, diagnostics.P95TTFTMS, ttftMS)
	assertAnalysisLatencyP95Close(t, diagnostics.P95LatencyMS, latencyMS)
}

func assertAnalysisLatencyP95Close(t *testing.T, got, want int64) {
	t.Helper()
	relativeError := math.Abs(float64(got-want)) / float64(want)
	if relativeError > latency.RelativeAccuracy {
		t.Fatalf("P95=%d, want %d within %.2f%% relative error (got %.4f%%)", got, want, latency.RelativeAccuracy*100, relativeError*100)
	}
}

func assertAnalysisLatencyPointSet(t *testing.T, diagnostics repodto.AnalysisLatencyDiagnosticsRecord, want [][2]int64) {
	t.Helper()
	if diagnostics.TotalPoints != int64(len(want)) || diagnostics.Sampled || len(diagnostics.Points) != len(want) {
		t.Fatalf("unexpected latency counts: total=%d sampled=%v points=%+v", diagnostics.TotalPoints, diagnostics.Sampled, diagnostics.Points)
	}
	remaining := make(map[[2]int64]int, len(want))
	for _, point := range want {
		remaining[point]++
	}
	for _, point := range diagnostics.Points {
		key := [2]int64{point.TTFTMS, point.LatencyMS}
		if remaining[key] == 0 {
			t.Fatalf("unexpected latency point %+v, want %+v", point, want)
		}
		remaining[key]--
	}
	for point, count := range remaining {
		if count != 0 {
			t.Fatalf("missing %d occurrences of latency point %+v in %+v", count, point, diagnostics.Points)
		}
	}
}
