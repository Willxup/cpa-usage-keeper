package test

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestQuotaEfficiencySegmentProjectionPreservesCompleteResponse(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/Los_Angeles"} {
		for _, provider := range []string{"codex", "claude"} {
			for _, role := range []string{"", "primary", "secondary"} {
				t.Run(zone+"/"+provider+"/"+role, func(t *testing.T) {
					setAnalysisLatencyTestTimezone(t, zone)
					db := openTestDatabase(t)
					now, ids := seedQuotaEfficiencyProjectionHistory(t, db, provider)
					var statements []quotaEfficiencyProjectionStatement
					queryDB := db.Session(&gorm.Session{Logger: quotaEfficiencyProjectionLogger{
						Interface: logger.Default.LogMode(logger.Silent), statements: &statements,
					}})
					var selectedRole *string
					if role != "" {
						selectedRole = &role
					}
					result := queryQuotaEfficiencyTimeHistory(t, queryDB, provider, selectedRole, now)
					selected := role
					if selected == "" {
						selected = "primary"
					}
					fixture := filepath.Join("testdata", "quota_efficiency_projection", strings.ReplaceAll(zone, "/", "_")+"_"+selected+".json")
					assertQuotaEfficiencyHistoryFixture(t, fixture, result)
					assertQuotaEfficiencySegmentProjection(t, statements, ids[selected], 4)
				})
			}
		}
	}
}

func TestQuotaEfficiencyNoEffectiveCyclesSkipsDetailReads(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
			cycle := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", "2026-08-21T00:00:00Z", "2026-08-21T05:00:00Z", []string{"2026-08-21T01:00:00Z"})
			// 正常 schema 不允许零宽周期；隔离库中的旧异常行覆盖无有效周期的防御分支。
			if err := db.Exec("PRAGMA ignore_check_constraints = ON").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).Update("window_started_at", timeutil.FormatSortableStorageTime(cycle.ResetAt)).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("PRAGMA ignore_check_constraints = OFF").Error; err != nil {
				t.Fatal(err)
			}
			var statements []quotaEfficiencyProjectionStatement
			queryDB := db.Session(&gorm.Session{Logger: quotaEfficiencyProjectionLogger{
				Interface: logger.Default.LogMode(logger.Silent), statements: &statements,
			}})
			result := queryQuotaEfficiencyTimeHistory(t, queryDB, provider, nil, now)
			if len(result.Windows) != 1 || result.SelectedWindow == nil || result.SelectedWindow.WindowRole != "primary" || result.SelectedWindow.HasCurrentCycle {
				t.Fatalf("empty effective range lost its window selection: %+v", result)
			}
			if result.Cycles == nil || len(result.Cycles) != 0 {
				t.Fatalf("expected the original non-nil empty cycle slice, got %#v", result.Cycles)
			}
			if len(statements) != 1 || !strings.Contains(statements[0].sql, "FROM `quota_cycles`") {
				t.Fatalf("no effective cycles must only read parents, got %+v", statements)
			}
		})
	}
}

// 每个角色各含一个被较新长周期覆盖的父行，并保留更早周期及真实空洞。
func seedQuotaEfficiencyProjectionHistory(t testing.TB, db *gorm.DB, provider string) (time.Time, map[string][]int64) {
	t.Helper()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	ids := make(map[string][]int64, 2)
	for _, role := range []string{"primary", "secondary"} {
		older := seedQuotaEfficiencyTimeCycle(t, db, provider, role, "2026-08-20T16:00:00.000000017Z", "2026-08-21T02:00:00.000000017Z", []string{
			"2026-08-20T16:01:00.123456789Z", "2026-08-20T18:00:00.123456789Z",
		})
		seedQuotaEfficiencyTimeCycle(t, db, provider, role, "2026-08-21T06:00:00Z", "2026-08-21T11:00:00Z", []string{"2026-08-21T06:10:00Z"})
		start, reset := "2026-08-21T00:00:00.000000019Z", "2026-08-28T00:00:00.000000019Z"
		if role == "secondary" {
			start, reset = "2026-08-21T03:00:00.000000019Z", "2026-08-21T13:00:00.000000019Z"
		}
		current := seedQuotaEfficiencyTimeCycle(t, db, provider, role, start, reset, []string{
			"2026-08-21T10:00:00.234567891Z", "2026-08-21T11:00:00.345678912Z",
		})
		if err := db.Model(&entities.QuotaPercentSegment{}).Where("cycle_id IN ?", []int64{older.ID, current.ID}).Update("observation_count", 3).Error; err != nil {
			t.Fatal(err)
		}
		ids[role] = []int64{current.ID, older.ID}
	}
	// 同账号其他 provider 和同 provider 其他账号不应进入父查询或子段 IN 范围。
	otherProvider := "claude"
	if provider == "claude" {
		otherProvider = "codex"
	}
	seedQuotaEfficiencyTimeCycle(t, db, otherProvider, "primary", "2026-08-21T05:00:00Z", "2026-08-21T10:00:00Z", []string{"2026-08-21T09:00:00Z"})
	otherAuth := seedQuotaEfficiencyTimeCycle(t, db, provider, "primary", "2026-08-21T04:00:00Z", "2026-08-21T09:00:00Z", []string{"2026-08-21T08:00:00Z"})
	if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", otherAuth.ID).Update("auth_index", "another-auth").Error; err != nil {
		t.Fatal(err)
	}
	seedQuotaEfficiencyRawTimeEvents(t, db, provider, []string{
		"2026-08-20T16:00:00.000000017Z", "2026-08-20T17:00:00.123456789Z", "2026-08-21T00:00:00.000000019Z",
		"2026-08-21T02:30:00Z", "2026-08-21T03:00:00.000000019Z", "2026-08-21T10:00:00.234567891Z",
		"2026-08-21T10:30:00.3Z", "2026-08-21T11:00:00.345678912Z", "2026-08-21T12:00:00Z",
	})
	return now, ids
}

type quotaEfficiencyProjectionStatement struct {
	sql  string
	rows int64
}

type quotaEfficiencyProjectionLogger struct {
	logger.Interface
	statements *[]quotaEfficiencyProjectionStatement
}

func (l quotaEfficiencyProjectionLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	statement, rows := fc()
	*l.statements = append(*l.statements, quotaEfficiencyProjectionStatement{sql: statement, rows: rows})
	l.Interface.Trace(ctx, begin, func() (string, int64) { return statement, rows }, err)
}

func assertQuotaEfficiencySegmentProjection(t *testing.T, statements []quotaEfficiencyProjectionStatement, cycleIDs []int64, rows int64) {
	t.Helper()
	if len(statements) != 3 {
		t.Fatalf("expected one parent, one segment and one event read, got %+v", statements)
	}
	statement := statements[1]
	columns := "id, cycle_id, remaining_percent, +first_observed_at AS first_observed_at, +last_observed_at AS last_observed_at, observation_count"
	var ids []string
	for _, id := range cycleIDs {
		ids = append(ids, fmt.Sprint(id))
	}
	want := "SELECT " + columns + " FROM `quota_percent_segments` WHERE cycle_id IN (" + strings.Join(ids, ",") + ") ORDER BY quota_percent_segments.cycle_id ASC, quota_percent_segments.first_observed_at ASC, quota_percent_segments.id ASC"
	if statement.sql != want || statement.rows != rows {
		t.Fatalf("expected only returned cycles and six stored columns (%d rows):\nwant %s\ngot  %s (%d rows)", rows, want, statement.sql, statement.rows)
	}
	if !slices.ContainsFunc(statements, func(statement quotaEfficiencyProjectionStatement) bool {
		return strings.Contains(statement.sql, "FROM usage_events INDEXED BY idx_usage_events_auth_index_timestamp_id")
	}) {
		t.Fatalf("missing indexed event read: %+v", statements)
	}
}

func BenchmarkCodexQuotaEfficiencyHistoryCoveredCycles(b *testing.B) {
	db := openQuotaEfficiencyBenchmarkDatabase(b, "quota-covered.db")
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	start := now.Add(-30 * 24 * time.Hour)
	returnedIDs := make(map[int64]bool, 72)
	for group := 0; group < 72; group++ {
		groupStart := start.Add(time.Duration(group) * 10 * time.Hour)
		for _, window := range []struct{ offset, duration time.Duration }{
			{offset: 5 * time.Hour, duration: 5 * time.Hour},
			{duration: 10 * time.Hour},
		} {
			cycleStart := groupStart.Add(window.offset)
			segments := quotaEfficiencyBenchmarkSegments(cycleStart, window.duration)
			cycle := seedCodexQuotaEfficiencyCycle(b, db, "codex-auth", cycleStart, cycleStart.Add(window.duration), segments)
			if window.offset == 0 {
				returnedIDs[cycle.ID] = true
			}
		}
	}
	const eventCount = 30_000
	seedQuotaEfficiencyBenchmarkEvents(b, db, start, eventCount, func(index int, at time.Time) entities.UsageEvent {
		return usageEventForQuotaEfficiency(fmt.Sprintf("covered-event-%d", index), "oauth", "codex-auth", at, 1000)
	})
	query := repositorydto.CodexQuotaEfficiencyQuery{AuthIndex: "codex-auth", Now: now, RangeStart: start}
	var statements []quotaEfficiencyProjectionStatement
	warmupDB := db.Session(&gorm.Session{Logger: quotaEfficiencyProjectionLogger{
		Interface: logger.Default.LogMode(logger.Silent), statements: &statements,
	}})
	if _, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), warmupDB, query); err != nil {
		b.Fatal(err)
	}
	var segmentRows int64
	for _, statement := range statements {
		if strings.Contains(statement.sql, "FROM `quota_percent_segments`") {
			segmentRows += statement.rows
		}
	}
	if segmentRows == 0 {
		b.Fatal("missing segment row count")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, query)
		if err != nil {
			b.Fatal(err)
		}
		var requests int64
		var cost float64
		for _, cycle := range result.Cycles {
			if !returnedIDs[cycle.ID] || cycle.Status != "completed" || len(cycle.Transitions) != 100 || cycle.ObservationCount != 101 ||
				cycle.FirstRemainingPercent == nil || *cycle.FirstRemainingPercent != 100 || cycle.LastRemainingPercent == nil || *cycle.LastRemainingPercent != 0 {
				b.Fatalf("unexpected covered cycle result: %+v", cycle)
			}
			requests += cycle.Usage.Requests
			cost += cycle.Usage.TotalCostUSD
		}
		if len(result.Cycles) != 72 || requests != eventCount || math.Abs(cost-30) > 1e-8 {
			b.Fatalf("unexpected covered history: cycles=%d requests=%d cost=%f", len(result.Cycles), requests, cost)
		}
	}
	b.ReportMetric(float64(segmentRows), "segments/op")
}
