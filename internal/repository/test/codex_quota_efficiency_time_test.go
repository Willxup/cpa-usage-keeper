package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type quotaEfficiencyTimeCase struct {
	name, zone, start, reset, now string
	observed, events              []string
	requests                      []int64
}

func TestQuotaEfficiencyTimeReadMatchesIndependentInstants(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		for _, tc := range quotaEfficiencyTimeCases() {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				setAnalysisLatencyTestTimezone(t, tc.zone)
				db := openTestDatabase(t)
				cycle := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", tc.start, tc.reset, tc.observed)
				events := seedQuotaEfficiencyRawTimeEvents(t, db, provider, tc.events)
				result := queryQuotaEfficiencyTimeHistory(t, db, provider, nil, quotaEfficiencyInstant(t, tc.now))
				if len(result.Cycles) != 1 {
					t.Fatalf("expected one cycle, got %+v", result.Cycles)
				}
				got := result.Cycles[0]
				assertQuotaEfficiencyTimePeriod(t, got, cycle.ID, quotaEfficiencyInstant(t, tc.start), quotaEfficiencyInstant(t, tc.now), tc.observed, events)
				for index, requests := range tc.requests {
					if got.Transitions[index].Usage.Requests != requests {
						t.Errorf("transition %d requests = %d, want %d", index, got.Transitions[index].Usage.Requests, requests)
					}
				}
				if tc.name == "lord_howe_half_hour_fallback" && events[1].Timestamp.Sub(events[0].Timestamp) != 30*time.Minute {
					t.Fatal("the two 01:45 observations must be 30 real minutes apart")
				}
				if tc.name == "la_spring_forward" && quotaEfficiencyInstant(t, tc.observed[2]).Sub(quotaEfficiencyInstant(t, tc.observed[1])) != time.Second {
					t.Fatal("01:59:59 to 03:00:00 must be one real second")
				}
			})
		}
	}
}

func quotaEfficiencyTimeCases() []quotaEfficiencyTimeCase {
	utcObserved := []string{"2026-10-21T00:00:00Z", "2026-10-21T00:30:00Z", "2026-10-21T00:40:00Z"}
	cases := []quotaEfficiencyTimeCase{
		{
			name: "la_fallback_range", zone: "America/Los_Angeles", start: "2026-11-01T00:00:00-07:00", reset: "2026-11-01T04:00:00-08:00", now: "2026-11-01T01:30:00-08:00",
			observed: []string{"2026-11-01T00:00:00-07:00", "2026-11-01T01:20:00-08:00"},
			events:   []string{"2026-11-01T01:45:00-07:00", "2026-11-01T01:15:00-08:00"}, requests: []int64{2},
		},
		{
			name: "la_fallback_intervals", zone: "America/Los_Angeles", start: "2026-11-01T00:00:00-07:00", reset: "2026-11-01T04:00:00-08:00", now: "2026-11-01T03:00:00-08:00",
			observed: []string{"2026-11-01T00:00:00-07:00", "2026-11-01T01:50:00-07:00", "2026-11-01T01:20:00-08:00", "2026-11-01T02:00:00-08:00"},
			events:   []string{"2026-11-01T01:40:00-07:00", "2026-11-01T01:10:00-08:00"}, requests: []int64{1, 1, 0},
		},
		{
			name: "utc_fractional_lower_bound", zone: "UTC", start: "2026-10-21T00:00:00Z", reset: "2026-10-21T05:00:00Z", now: "2026-10-21T01:00:00Z",
			observed: utcObserved, events: []string{"2026-10-21T00:00:00.5Z"}, requests: []int64{1, 0},
		},
		{
			name: "utc_same_second_reverse_order", zone: "UTC", start: "2026-10-21T00:00:00Z", reset: "2026-10-21T05:00:00Z", now: "2026-10-21T01:00:00Z",
			observed: utcObserved, events: []string{"2026-10-21T00:30:00Z", "2026-10-21T00:30:00.5Z"}, requests: []int64{1, 1},
		},
		{
			name: "utc_fractional_prefixes", zone: "UTC", start: "2026-10-21T00:00:00Z", reset: "2026-10-21T05:00:00Z", now: "2026-10-21T01:00:00Z",
			observed: []string{"2026-10-21T00:00:00Z", "2026-10-21T00:30:00.1Z", "2026-10-21T00:30:00.100000001Z", "2026-10-21T00:30:00.11Z", "2026-10-21T00:40:00Z"},
			events:   []string{"2026-10-21T00:30:00.1Z", "2026-10-21T00:30:00.11Z", "2026-10-21T00:30:00.100000001Z"}, requests: []int64{1, 1, 1, 0},
		},
		{
			name: "utc_exclusive_boundaries_and_equal_instants", zone: "UTC", start: "2026-10-21T00:00:00Z", reset: "2026-10-21T05:00:00Z", now: "2026-10-21T01:00:00Z",
			observed: []string{"2026-10-21T00:10:00Z", "2026-10-21T00:30:00Z", "2026-10-21T00:40:00Z"},
			events: []string{
				"2026-10-20T23:59:59.999999999Z", "2026-10-21T00:00:00Z", "2026-10-21T00:00:00.000000001Z",
				"2026-10-21T00:09:59.999999999Z", "2026-10-21T00:10:00Z", "2026-10-21T00:10:00.000000001Z",
				"2026-10-21T00:29:59.999999999Z", "2026-10-21T00:30:00Z", "2026-10-21T00:30:00Z", "2026-10-21T00:30:00.000000001Z",
				"2026-10-21T00:39:59.999999999Z", "2026-10-21T00:40:00Z", "2026-10-21T00:40:00.000000001Z",
				"2026-10-21T00:59:59.999999999Z", "2026-10-21T01:00:00Z", "2026-10-21T01:00:00.000000001Z",
			}, requests: []int64{4, 3},
		},
		{
			name: "la_offset_segment_boundary", zone: "America/Los_Angeles", start: "2026-11-01T00:00:00-07:00", reset: "2026-11-01T04:00:00-08:00", now: "2026-11-01T03:00:00-08:00",
			observed: []string{"2026-11-01T00:00:00-07:00", "2026-11-01T01:59:59.999999999-07:00", "2026-11-01T01:00:00-08:00", "2026-11-01T01:00:00.000000001-08:00", "2026-11-01T02:00:00-08:00"},
			events:   []string{"2026-11-01T01:59:59.999999999-07:00", "2026-11-01T01:00:00-08:00", "2026-11-01T01:00:00-08:00", "2026-11-01T01:00:00.000000001-08:00"}, requests: []int64{1, 2, 1, 0},
		},
		{
			name: "la_spring_forward", zone: "America/Los_Angeles", start: "2026-03-08T00:00:00-08:00", reset: "2026-03-08T06:00:00-07:00", now: "2026-03-08T04:00:00-07:00",
			observed: []string{"2026-03-08T00:00:00-08:00", "2026-03-08T01:59:59-08:00", "2026-03-08T03:00:00-07:00", "2026-03-08T03:10:00-07:00"},
			events:   []string{"2026-03-08T01:59:58.999999999-08:00", "2026-03-08T01:59:59-08:00", "2026-03-08T01:59:59.999999999-08:00", "2026-03-08T03:00:00-07:00", "2026-03-08T03:00:00.000000001-07:00"}, requests: []int64{2, 2, 1},
		},
		{
			name: "lord_howe_half_hour_fallback", zone: "Australia/Lord_Howe", start: "2026-04-05T00:00:00+11:00", reset: "2026-04-05T04:00:00+10:30", now: "2026-04-05T03:00:00+10:30",
			observed: []string{"2026-04-05T00:00:00+11:00", "2026-04-05T01:50:00+11:00", "2026-04-05T01:40:00+10:30", "2026-04-05T02:00:00+10:30"},
			events:   []string{"2026-04-05T01:45:00+11:00", "2026-04-05T01:45:00+10:30", "2026-04-05T01:59:59.999999999+11:00", "2026-04-05T01:30:00+10:30", "2026-04-05T01:30:00.000000001+10:30"}, requests: []int64{1, 3, 1},
		},
	}
	for _, normal := range []struct{ name, zone, offset string }{{"utc", "UTC", "Z"}, {"shanghai", "Asia/Shanghai", "+08:00"}, {"la", "America/Los_Angeles", "-07:00"}} {
		at := func(clock string) string { return "2026-10-21T" + clock + normal.offset }
		cases = append(cases, quotaEfficiencyTimeCase{
			name: "ordinary_" + normal.name, zone: normal.zone, start: at("00:00:00"), reset: at("05:00:00"), now: at("03:00:00"),
			observed: []string{at("00:10:00"), at("01:00:00"), at("02:00:00")},
			events:   []string{at("00:00:00"), at("00:45:00"), at("01:00:00"), at("01:30:00"), at("02:30:00"), at("03:00:00")}, requests: []int64{2, 1},
		})
	}
	return cases
}

func TestQuotaEfficiencyTimeReadRelocatesAcrossGapsAndCoveredCycles(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			setAnalysisLatencyTestTimezone(t, "UTC")
			db := openTestDatabase(t)
			// 两个周期共享小数秒边界；文本先读 .5Z，再读整秒 Z，必须能从后周期回到前周期。
			firstObserved := []string{"2026-10-21T00:00:00Z", "2026-10-21T00:30:00Z"}
			first := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", "2026-10-21T00:00:00Z", "2026-10-21T01:00:00Z", firstObserved)
			covered := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", "2026-10-21T00:30:00.3Z", "2026-10-21T00:30:01.3Z", []string{"2026-10-21T00:30:00.35Z"})
			secondObserved := []string{"2026-10-21T00:30:00.25Z"}
			second := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", "2026-10-21T00:30:00.25Z", "2026-10-21T00:35:00.25Z", secondObserved)
			// 后观察到的新周期覆盖中间短周期，其理论起点仍保留；末周期前还存在真实空洞。
			if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", second.ID).Update("last_observed_at", "2026-10-21T00:30:00.6Z").Error; err != nil {
				t.Fatal(err)
			}
			lastObserved := []string{"2026-10-21T00:40:00Z", "2026-10-21T00:50:00Z"}
			last := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", "2026-10-21T00:39:59.75Z", "2026-10-21T00:59:59.75Z", lastObserved)
			events := seedQuotaEfficiencyRawTimeEvents(t, db, provider, []string{
				"2026-10-21T00:30:00.5Z", "2026-10-21T00:30:00Z", "2026-10-21T00:30:00.249999999Z", "2026-10-21T00:30:00.25Z", "2026-10-21T00:30:00.250000001Z",
				"2026-10-21T00:35:00.25Z", "2026-10-21T00:35:00.5Z", "2026-10-21T00:35:00Z", "2026-10-21T00:39:59.75Z", "2026-10-21T00:40:00Z", "2026-10-21T00:50:00Z",
				"2026-10-21T00:59:59.9Z", "2026-10-21T00:59:59Z", "2026-10-21T00:59:59.749999999Z", "2026-10-21T00:59:59.75Z",
			})
			result := queryQuotaEfficiencyTimeHistory(t, db, provider, nil, quotaEfficiencyInstant(t, "2026-10-21T01:00:00Z"))
			if len(result.Cycles) != 3 {
				t.Fatalf("covered cycle %d must be omitted: %+v", covered.ID, result.Cycles)
			}
			for _, expected := range []struct {
				cycle    entities.QuotaCycle
				observed []string
			}{{first, firstObserved}, {second, secondObserved}, {last, lastObserved}} {
				end := expected.cycle.ResetAt
				if expected.cycle.ID == first.ID {
					end = second.WindowStartedAt
				}
				found := false
				for _, got := range result.Cycles {
					if got.ID == expected.cycle.ID {
						found = true
						if !got.EffectiveStartedAt.Equal(expected.cycle.WindowStartedAt) || !got.EffectiveEndedAt.Equal(end) {
							t.Fatalf("effective period changed: %+v", got)
						}
						assertQuotaEfficiencyTimePeriod(t, got, expected.cycle.ID, expected.cycle.WindowStartedAt, end, expected.observed, events)
					}
				}
				if !found {
					t.Fatalf("expected cycle %d missing: %+v", expected.cycle.ID, result.Cycles)
				}
			}
		})
	}
}

func TestQuotaEfficiencyTimeReadClipsCandidatesBeforeCostValidation(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			setAnalysisLatencyTestTimezone(t, "UTC")
			db := openTestDatabase(t)
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			sqlDB.SetMaxOpenConns(1)
			seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", "2026-10-21T00:00:00.5Z", "2026-10-21T05:00:00.5Z", []string{"2026-10-21T00:00:00.5Z", "2026-10-21T00:40:00Z"})
			events := seedQuotaEfficiencyRawTimeEvents(t, db, provider, []string{"2026-10-21T00:00:00.1Z", "2026-10-21T00:30:00Z", "2026-10-21T01:00:00.5Z", "2026-10-21T01:00:00.9Z"})
			for _, index := range []int{0, 2, 3} {
				if err := db.Model(&entities.UsageEvent{}).Where("event_key = ?", events[index].EventKey).Updates(map[string]any{"cost_usd": nil, "cost_available": nil}).Error; err != nil {
					t.Fatal(err)
				}
			}
			got := queryQuotaEfficiencyTimeHistory(t, db, provider, nil, quotaEfficiencyInstant(t, "2026-10-21T01:00:00.5Z"))
			want := referenceQuotaEfficiencyTimeUsage(events[1:2], events[1].Timestamp.Add(-time.Second), events[1].Timestamp.Add(time.Second), nil, nil)
			assertQuotaEfficiencyTimeUsage(t, got.Cycles[0].Usage, want)
			if err := db.Model(&entities.UsageEvent{}).Where("event_key = ?", events[1].EventKey).Update("cost_usd", nil).Error; err != nil {
				t.Fatal(err)
			}
			_, err = repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{Provider: provider, AuthIndex: "time-auth", Now: quotaEfficiencyInstant(t, "2026-10-21T01:00:00.5Z"), RangeStart: quotaEfficiencyInstant(t, "2026-09-21T01:00:00.5Z")})
			if err == nil || !strings.Contains(err.Error(), "invalid stored cost") {
				t.Fatalf("in-range unbackfilled cost must still fail, got %v", err)
			}
			checkCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var one int
			if err := db.WithContext(checkCtx).Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
				t.Fatalf("cost-error cursor did not release its connection: one=%d err=%v", one, err)
			}
		})
	}
}

func TestQuotaEfficiencyTimeReadFallbackAcrossCycles(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			setAnalysisLatencyTestTimezone(t, "America/Los_Angeles")
			db := openTestDatabase(t)
			periods := []struct {
				start, reset string
				observed     []string
			}{
				{"2026-11-01T00:00:00-07:00", "2026-11-01T01:50:00-07:00", []string{"2026-11-01T00:00:00-07:00", "2026-11-01T01:45:00-07:00"}},
				{"2026-11-01T01:50:00-07:00", "2026-11-01T01:15:00-08:00", []string{"2026-11-01T01:55:00-07:00", "2026-11-01T01:10:00-08:00"}},
				{"2026-11-01T01:20:00-08:00", "2026-11-01T04:00:00-08:00", []string{"2026-11-01T01:25:00-08:00"}},
			}
			ids := make([]int64, len(periods))
			for index, period := range periods {
				ids[index] = seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", period.start, period.reset, period.observed).ID
			}
			events := seedQuotaEfficiencyRawTimeEvents(t, db, provider, []string{
				"2026-11-01T01:40:00-07:00", "2026-11-01T01:49:59.999999999-07:00", "2026-11-01T01:50:00-07:00", "2026-11-01T01:50:00.000000001-07:00",
				"2026-11-01T01:55:00-07:00", "2026-11-01T01:10:00-08:00", "2026-11-01T01:14:59.999999999-08:00", "2026-11-01T01:15:00-08:00",
				"2026-11-01T01:17:00-08:00", "2026-11-01T01:20:00-08:00", "2026-11-01T01:30:00-08:00",
			})
			now := quotaEfficiencyInstant(t, "2026-11-01T03:00:00-08:00")
			result := queryQuotaEfficiencyTimeHistory(t, db, provider, nil, now)
			if len(result.Cycles) != len(periods) {
				t.Fatalf("cycles = %d, want %d", len(result.Cycles), len(periods))
			}
			for index, period := range periods {
				end := quotaEfficiencyInstant(t, period.reset)
				if now.Before(end) {
					end = now
				}
				got := result.Cycles[len(periods)-index-1]
				assertQuotaEfficiencyTimePeriod(t, got, ids[index], quotaEfficiencyInstant(t, period.start), end, period.observed, events)
			}
		})
	}
}

func TestQuotaEfficiencyTimeReadQueriesAreBoundedAndIndexed(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		for _, tc := range []struct {
			name, zone, start string
			reads             int
		}{
			{"ordinary", "UTC", "2026-10-21T00:00:00Z", 1},
			{"fallback", "America/Los_Angeles", "2026-10-31T00:00:00-07:00", 2},
			{"spring_forward", "America/Los_Angeles", "2026-03-07T00:00:00-08:00", 2},
			{"half_hour_fallback", "Australia/Lord_Howe", "2026-04-04T00:00:00+11:00", 2},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				setAnalysisLatencyTestTimezone(t, tc.zone)
				db := openTestDatabase(t)
				start := quotaEfficiencyInstant(t, tc.start).In(time.Local)
				for index := range 12 {
					cycleStart := start.Add(time.Duration(index) * 5 * time.Hour)
					seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", cycleStart.Format(time.RFC3339Nano), cycleStart.Add(5*time.Hour).Format(time.RFC3339Nano), []string{cycleStart.Format(time.RFC3339Nano), cycleStart.Add(time.Hour).Format(time.RFC3339Nano)})
				}
				var statements []string
				queryDB := db.Session(&gorm.Session{Logger: quotaEfficiencyTimeQueryLogger{Interface: logger.Default.LogMode(logger.Silent), statements: &statements}})
				queryQuotaEfficiencyTimeHistory(t, queryDB, provider, nil, start.Add(60*time.Hour))
				if len(statements) != tc.reads {
					t.Fatalf("12 cycles require %d timezone reads, got %d: %v", tc.reads, len(statements), statements)
				}
				for _, statement := range statements {
					if !strings.Contains(statement, "ORDER BY usage_events.timestamp ASC, usage_events.id ASC") {
						t.Fatalf("missing deterministic index order: %s", statement)
					}
					var plan []struct{ Detail string }
					if err := db.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&plan).Error; err != nil {
						t.Fatal(err)
					}
					var details []string
					for _, row := range plan {
						details = append(details, row.Detail)
					}
					joined := strings.Join(details, "\n")
					if !strings.Contains(joined, "idx_usage_events_auth_index_timestamp_id") || !strings.Contains(joined, "auth_index=? AND timestamp>? AND timestamp<?") || strings.Contains(joined, "TEMP B-TREE") {
						t.Fatalf("expected auth/time index range without temporary sort:\n%s\n%s", statement, joined)
					}
				}
			})
		}
	}
}

func TestQuotaEfficiencyTimeReadCancellationClosesReader(t *testing.T) {
	setAnalysisLatencyTestTimezone(t, "America/Los_Angeles")
	db, _, readerSQL := openTestDatabasePools(t, "quota-cancellation.db")
	readerSQL.SetMaxOpenConns(1)
	seedQuotaEfficiencyTimeCycle(t, db, "codex", "primary", "2026-11-01T00:00:00-07:00", "2026-11-01T04:00:00-08:00", []string{"2026-11-01T00:00:00-07:00", "2026-11-01T02:00:00-08:00"})
	seedQuotaEfficiencyRawTimeEvents(t, db, "codex", []string{"2026-11-01T01:45:00-07:00", "2026-11-01T01:15:00-08:00"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var statements []string
	queryDB := db.Session(&gorm.Session{Logger: quotaEfficiencyTimeQueryLogger{Interface: logger.Default.LogMode(logger.Silent), statements: &statements, onRead: cancel}})
	_, err := repository.BuildCodexQuotaEfficiencyHistory(ctx, queryDB, repositorydto.CodexQuotaEfficiencyQuery{Provider: "codex", AuthIndex: "time-auth", Now: quotaEfficiencyInstant(t, "2026-11-01T03:00:00-08:00"), RangeStart: quotaEfficiencyInstant(t, "2026-10-02T03:00:00-07:00")})
	if !errors.Is(err, context.Canceled) || len(statements) != 1 {
		t.Fatalf("canceled read must stop before another timezone query: err=%v queries=%d", err, len(statements))
	}
	checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
	defer checkCancel()
	var one int
	if err := db.WithContext(checkCtx).Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
		t.Fatalf("canceled cursor did not release its connection: one=%d err=%v", one, err)
	}
}

func TestQuotaEfficiencyTimeReadUsesReaderPool(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			setAnalysisLatencyTestTimezone(t, "America/Los_Angeles")
			db, writerSQL, _ := openTestDatabasePools(t, "quota-route.db")
			observed := []string{"2026-11-01T00:00:00-07:00", "2026-11-01T01:20:00-08:00"}
			cycle := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", observed[0], "2026-11-01T04:00:00-08:00", observed)
			events := seedQuotaEfficiencyRawTimeEvents(t, db, provider, []string{"2026-11-01T01:45:00-07:00", "2026-11-01T01:15:00-08:00"})
			// 占满唯一 writer 后，父行、百分比段和两个事件读取段都必须通过 reader 完成。
			heldWriter, err := writerSQL.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer heldWriter.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			now := quotaEfficiencyInstant(t, "2026-11-01T01:30:00-08:00")
			result, err := repository.BuildCodexQuotaEfficiencyHistory(ctx, db, repositorydto.CodexQuotaEfficiencyQuery{Provider: provider, AuthIndex: "time-auth", Now: now, RangeStart: now.Add(-30 * 24 * time.Hour)})
			if err != nil {
				t.Fatalf("quota history did not use the reader pool: %v", err)
			}
			if len(result.Cycles) != 1 {
				t.Fatalf("cycles = %d, want 1", len(result.Cycles))
			}
			assertQuotaEfficiencyTimePeriod(t, result.Cycles[0], cycle.ID, quotaEfficiencyInstant(t, observed[0]), now, observed, events)
		})
	}
}

// 普通日的完整响应在修复前冻结；同时保护两个 provider 及默认/显式角色的全部字段和 null/空数组。
func TestQuotaEfficiencyTimeReadOrdinaryResponseBaseline(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/Los_Angeles"} {
		t.Run(zone, func(t *testing.T) {
			setAnalysisLatencyTestTimezone(t, zone)
			now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.Local)
			for _, provider := range []string{"codex", "claude"} {
				db := openTestDatabase(t)
				for _, role := range []string{"primary", "secondary"} {
					seedQuotaEfficiencyTimeCycle(t, db, provider, role, now.Add(-3*time.Hour).Format(time.RFC3339Nano), now.Add(2*time.Hour).Format(time.RFC3339Nano), []string{now.Add(-2 * time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano)})
				}
				events := seedQuotaEfficiencyRawTimeEvents(t, db, provider, []string{now.Add(-3 * time.Hour).Format(time.RFC3339Nano), now.Add(-90 * time.Minute).Format(time.RFC3339Nano), now.Add(-30 * time.Minute).Format(time.RFC3339Nano)})
				// 已有 NULL 列维持原 COALESCE 语义；total-only 与缺价样本由另外两行保留。
				if err := db.Model(&entities.UsageEvent{}).Where("event_key = ?", events[2].EventKey).Updates(map[string]any{"failed": nil, "input_tokens": nil, "output_tokens": nil, "reasoning_tokens": nil, "total_tokens": nil}).Error; err != nil {
					t.Fatal(err)
				}
				for _, role := range []string{"default", "primary", "secondary"} {
					var selected *string
					if role != "default" {
						selected = &role
					}
					result := queryQuotaEfficiencyTimeHistory(t, db, provider, selected, now)
					fixtureRole := role
					if role == "default" {
						fixtureRole = "primary"
					}
					fixturePath := filepath.Join("testdata", "quota_efficiency_time", strings.ReplaceAll(zone, "/", "_")+"_"+fixtureRole+".json")
					assertQuotaEfficiencyHistoryFixture(t, fixturePath, result)
				}
			}
		})
	}
}

type quotaEfficiencyTimeQueryLogger struct {
	logger.Interface
	statements *[]string
	onRead     func()
}

func (l quotaEfficiencyTimeQueryLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	statement, affected := fc()
	if strings.Contains(statement, "FROM usage_events INDEXED BY idx_usage_events_auth_index_timestamp_id") {
		*l.statements = append(*l.statements, statement)
		if l.onRead != nil {
			l.onRead()
		}
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return statement, affected }, err)
}

func quotaEfficiencyInstant(t testing.TB, value string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func seedQuotaEfficiencyTimeCycle(t testing.TB, db *gorm.DB, provider, role, start, reset string, observed []string) entities.QuotaCycle {
	t.Helper()
	segments := make([]codexQuotaEfficiencySegmentSeed, len(observed))
	for index, value := range observed {
		at := quotaEfficiencyInstant(t, value)
		segments[index] = codexQuotaEfficiencySegmentSeed{remaining: 100 - index, first: at, last: at}
	}
	windowRole := entities.CodexQuotaWindowRole(role)
	cycle := seedCodexQuotaEfficiencyRoleCycle(t, db, "time-auth", windowRole, quotaEfficiencyInstant(t, start), quotaEfficiencyInstant(t, reset), segments)
	key, ok := repositorydto.QuotaWindowKey(provider, role)
	if !ok {
		t.Fatalf("unknown fixture provider/role: %s/%s", provider, role)
	}
	if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).Updates(map[string]any{"provider": provider, "quota_key": key}).Error; err != nil {
		t.Fatal(err)
	}
	return cycle
}

func seedQuotaEfficiencyRawTimeEvents(t testing.TB, db *gorm.DB, provider string, timestamps []string) []entities.UsageEvent {
	t.Helper()
	events := make([]entities.UsageEvent, len(timestamps))
	for index, value := range timestamps {
		n := int64(index + 1)
		event := usageEventForQuotaEfficiency(fmt.Sprintf("time-event-%d", index), "oauth", "time-auth", quotaEfficiencyInstant(t, value), n*11)
		event.Provider, event.Failed = provider, index%2 == 1
		event.OutputTokens, event.ReasoningTokens = n*7, n*3
		event.CacheReadTokens, event.CacheCreationTokens, event.TotalTokens = n*5, n*2, n*101
		cost, available := float64(n)/32, index%3 != 1
		event.CostUSD, event.CostAvailable = &cost, &available
		if index%3 == 0 {
			event.InputTokens, event.OutputTokens, event.ReasoningTokens, event.CacheReadTokens, event.CacheCreationTokens = 0, 0, 0, 0, 0
		}
		seedCodexQuotaEfficiencyUsage(t, db, event)
		// 直接保存输入文本，避免 serializer 预先统一 offset/小数位而掩盖读取缺陷。
		if err := db.Exec("UPDATE usage_events SET timestamp = ? WHERE event_key = ?", value, event.EventKey).Error; err != nil {
			t.Fatal(err)
		}
		events[index] = event
	}
	return events
}

func assertQuotaEfficiencyHistoryFixture(t *testing.T, path string, result repositorydto.CodexQuotaEfficiencyHistory) {
	t.Helper()
	actual, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual)+"\n" != string(want) {
		t.Fatalf("complete history differs from frozen fixture %s:\ngot %s\nwant %s", path, actual, want)
	}
}

func queryQuotaEfficiencyTimeHistory(t *testing.T, db *gorm.DB, provider string, role *string, now time.Time) repositorydto.CodexQuotaEfficiencyHistory {
	t.Helper()
	result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{Provider: provider, AuthIndex: "time-auth", WindowRole: role, Now: now, RangeStart: now.Add(-30 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertQuotaEfficiencyTimePeriod(t *testing.T, got repositorydto.CodexQuotaEfficiencyCycle, id int64, start, end time.Time, observed []string, events []entities.UsageEvent) {
	t.Helper()
	if got.ID != id {
		t.Fatalf("cycle ID = %d, want %d", got.ID, id)
	}
	want := referenceQuotaEfficiencyTimeUsage(events, start, end, nil, nil)
	assertQuotaEfficiencyTimeUsage(t, got.Usage, want)
	if len(got.Transitions) != len(observed)-1 {
		t.Fatalf("transitions = %d, want %d", len(got.Transitions), len(observed)-1)
	}
	for index, transition := range got.Transitions {
		left, right := quotaEfficiencyInstant(t, observed[index]), quotaEfficiencyInstant(t, observed[index+1])
		if !transition.IntervalStartedAt.Equal(left) || !transition.IntervalEndedAt.Equal(right) {
			t.Fatalf("transition %d boundaries changed: %+v", index, transition)
		}
		want := referenceQuotaEfficiencyTimeUsage(events, start, end, &left, &right)
		assertQuotaEfficiencyTimeUsage(t, transition.Usage, want)
		if math.Abs(transition.TokensPerPoint-float64(want.TotalTokens)) > 1e-9 || math.Abs(transition.CostPerPoint-want.TotalCostUSD) > 1e-12 || transition.CostPerPointAvailable != want.CostAvailable {
			t.Fatalf("transition %d per-point values changed: %+v, want usage %+v", index, transition, want)
		}
	}
}

// 参考结果逐事件检查测试中明确给定的真实周期与区间，独立于生产分段、游标和定位 helper。
func referenceQuotaEfficiencyTimeUsage(events []entities.UsageEvent, start, end time.Time, left, right *time.Time) repositorydto.CodexQuotaEfficiencyUsage {
	result := repositorydto.CodexQuotaEfficiencyUsage{CostAvailable: true}
	for _, event := range events {
		if event.Timestamp.Before(start) || !event.Timestamp.Before(end) {
			continue
		}
		if left != nil && (!event.Timestamp.After(*left) || event.Timestamp.After(*right)) {
			continue
		}
		result.Requests++
		if event.Failed {
			result.FailedRequests++
		} else {
			result.SuccessfulRequests++
		}
		result.InputTokens += event.InputTokens
		result.OutputTokens += event.OutputTokens
		result.ReasoningTokens += event.ReasoningTokens
		result.CacheReadTokens += event.CacheReadTokens
		result.CacheCreationTokens += event.CacheCreationTokens
		result.TotalTokens += event.TotalTokens
		result.TotalCostUSD += *event.CostUSD
		result.CostAvailable = result.CostAvailable && *event.CostAvailable
	}
	return result
}

func assertQuotaEfficiencyTimeUsage(t *testing.T, got, want repositorydto.CodexQuotaEfficiencyUsage) {
	t.Helper()
	if math.IsNaN(got.TotalCostUSD) || math.IsInf(got.TotalCostUSD, 0) || math.Abs(got.TotalCostUSD-want.TotalCostUSD) > 1e-12 {
		t.Errorf("cost = %.17g, want %.17g", got.TotalCostUSD, want.TotalCostUSD)
	}
	got.TotalCostUSD, want.TotalCostUSD = 0, 0
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}
