package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLoadUsageIdentityActivityOrdersRealInstantsAndIsolatesTypes(t *testing.T) {
	for _, zone := range []string{"UTC", "America/Los_Angeles", "Europe/London"} {
		t.Run(zone, func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, zone)
			now := time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC).In(location)
			db := openTestDatabase(t)
			if err := db.Create(&[]entities.UsageIdentity{
				{ID: 1, Identity: "shared", AuthType: entities.UsageIdentityAuthTypeCredential},
				{ID: 2, Identity: "shared", AuthType: entities.UsageIdentityAuthTypeAIProvider},
				{ID: 3, Identity: "", AuthType: entities.UsageIdentityAuthTypeCredential},
			}).Error; err != nil {
				t.Fatal(err)
			}
			code := 429
			stamp := time.Date(2026, 11, 1, 9, 15, 0, 0, time.UTC)
			events := []entities.UsageEvent{
				{ID: 1, EventKey: "later-first", Timestamp: stamp.Add(100000001 * time.Nanosecond), AuthType: "oauth", AuthIndex: "shared"},
				{ID: 2, EventKey: "failure", Timestamp: stamp, AuthType: "oauth", AuthIndex: "shared", Failed: true, StatusCode: &code},
				{ID: 3, EventKey: "later-second", Timestamp: stamp.Add(100 * time.Millisecond), AuthType: "oauth", AuthIndex: "shared"},
				{ID: 4, EventKey: "different-type", Timestamp: now.Add(-time.Minute), AuthType: "apikey", AuthIndex: "shared", Failed: true},
				{ID: 5, EventKey: "end", Timestamp: now, AuthType: "oauth", AuthIndex: "shared", Failed: true},
				{ID: 6, EventKey: "before-start", Timestamp: now.Add(-5*time.Hour - time.Nanosecond), AuthType: "oauth", AuthIndex: "shared", Failed: true},
				{ID: 7, EventKey: "empty-index", Timestamp: now.Add(-time.Second), AuthType: "oauth", AuthIndex: "", Failed: true},
			}
			if err := db.Create(&events).Error; err != nil {
				t.Fatal(err)
			}
			result, err := repository.LoadUsageIdentityActivity(context.Background(), db, 1, now)
			if err != nil {
				t.Fatal(err)
			}
			if !result.WindowStart.Equal(now.Add(-5*time.Hour)) || !result.WindowEnd.Equal(now) || result.Latest == nil || result.Latest.ID != 1 || result.Latest.Failed || result.LastSuccess == nil || result.LastSuccess.ID != 1 || result.LastSuccess.StatusCode != nil || result.LastFailure == nil || result.LastFailure.ID != 2 || result.LastFailure.StatusCode == nil || *result.LastFailure.StatusCode != 429 || result.Streak != 2 || result.StreakAtLeast {
				t.Fatalf("wrong chronological activity: %+v", result)
			}
			provider, err := repository.LoadUsageIdentityActivity(context.Background(), db, 2, now)
			if err != nil || provider.Latest == nil || provider.Latest.ID != 4 || !provider.Latest.Failed || provider.Streak != 1 || provider.LastSuccess != nil {
				t.Fatalf("provider type isolation: %+v, %v", provider, err)
			}
			empty, err := repository.LoadUsageIdentityActivity(context.Background(), db, 3, now)
			if err != nil || empty.Latest != nil || empty.Streak != 0 {
				t.Fatalf("empty identity matched events: %+v, %v", empty, err)
			}
		})
	}
}

func TestLoadUsageIdentityActivityWindowAndRepeatedHour(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "America/Los_Angeles")
	db := openTestDatabase(t)
	now := time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC).In(location)
	if err := db.Create(&entities.UsageIdentity{ID: 1, Identity: "auth", AuthType: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// 01:10 PST 比 01:50 PDT 更晚，不能按本地钟面字符串排序。
	events := []entities.UsageEvent{
		{ID: 1, EventKey: "first-01", AuthType: "oauth", AuthIndex: "auth", Timestamp: time.Date(2026, 11, 1, 8, 50, 0, 0, time.UTC)},
		{ID: 2, EventKey: "second-01", AuthType: "oauth", AuthIndex: "auth", Timestamp: time.Date(2026, 11, 1, 9, 10, 0, 0, time.UTC), Failed: true},
		{ID: 3, EventKey: "window-start", AuthType: "oauth", AuthIndex: "auth", Timestamp: now.Add(-5 * time.Hour)},
		{ID: 4, EventKey: "window-end", AuthType: "oauth", AuthIndex: "auth", Timestamp: now},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	result, err := repository.LoadUsageIdentityActivity(context.Background(), db, 1, now)
	if err != nil || result.Latest == nil || result.Latest.ID != 2 || result.LastSuccess == nil || result.LastSuccess.ID != 1 || result.Streak != 1 {
		t.Fatalf("repeated hour or end boundary: %+v, %v", result, err)
	}
	// 独立身份只有起点事件，验证窗口左闭；终点事件不得影响结果。
	if err := db.Create(&entities.UsageIdentity{ID: 2, Identity: "edge", AuthType: 1}).Error; err != nil {
		t.Fatal(err)
	}
	for _, event := range []entities.UsageEvent{
		{EventKey: "edge-start", AuthType: "oauth", AuthIndex: "edge", Timestamp: now.Add(-5 * time.Hour)},
		{EventKey: "edge-end", AuthType: "oauth", AuthIndex: "edge", Timestamp: now, Failed: true},
	} {
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	edge, err := repository.LoadUsageIdentityActivity(context.Background(), db, 2, now)
	if err != nil || edge.Latest == nil || edge.Latest.Failed || !edge.Latest.Timestamp.Equal(now.Add(-5*time.Hour)) || edge.LastFailure != nil {
		t.Fatalf("window must be [start,end): %+v, %v", edge, err)
	}
}

func TestLoadUsageIdentityActivityBreaksTimestampTiesByID(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageIdentity{ID: 1, Identity: "auth", AuthType: 1}).Error; err != nil {
		t.Fatal(err)
	}
	for _, event := range []entities.UsageEvent{
		{ID: 1, EventKey: "success", AuthType: "oauth", AuthIndex: "auth", Timestamp: now.Add(-time.Second)},
		{ID: 2, EventKey: "failure", AuthType: "oauth", AuthIndex: "auth", Timestamp: now.Add(-time.Second), Failed: true},
	} {
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := repository.LoadUsageIdentityActivity(context.Background(), db, 1, now)
	if err != nil || result.Latest == nil || result.Latest.ID != 2 || !result.Latest.Failed || result.Streak != 1 {
		t.Fatalf("same timestamp order: %+v, %v", result, err)
	}
}

func TestLoadUsageIdentityActivityStreakLimit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		failed        bool
		count         int
		oppositeAfter bool
		want          int
		atLeast       bool
	}{
		{"empty", false, 0, false, 0, false},
		{"success-99", false, 99, false, 99, false},
		{"success-100", false, 100, false, 100, false},
		{"success-over-100", false, 101, false, 100, true},
		{"success-opposite-after-100", false, 101, true, 100, false},
		{"failure-100", true, 100, false, 100, false},
		{"failure-over-100", true, 101, false, 100, true},
		{"failure-opposite-after-100", true, 101, true, 100, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
			if err := db.Create(&entities.UsageIdentity{ID: 1, Identity: "auth", AuthType: 1}).Error; err != nil {
				t.Fatal(err)
			}
			var events []entities.UsageEvent
			for i := 0; i < tc.count; i++ {
				events = append(events, entities.UsageEvent{EventKey: fmt.Sprint(i), AuthType: "oauth", AuthIndex: "auth", Timestamp: now.Add(-time.Duration(i+1) * time.Second), Failed: tc.failed})
			}
			if tc.oppositeAfter {
				events = append(events, entities.UsageEvent{EventKey: "opposite", AuthType: "oauth", AuthIndex: "auth", Timestamp: now.Add(-100*time.Second - 500*time.Millisecond), Failed: !tc.failed})
			}
			if len(events) > 0 {
				if err := db.CreateInBatches(events, 50).Error; err != nil {
					t.Fatal(err)
				}
			}
			result, err := repository.LoadUsageIdentityActivity(context.Background(), db, 1, now)
			if err != nil || result.Streak != tc.want || result.StreakAtLeast != tc.atLeast {
				t.Fatalf("streak = %+v, error %v; want %d atLeast=%t", result, err, tc.want, tc.atLeast)
			}
		})
	}
}

type identityActivityQuery struct {
	SQL  string
	Rows int64
}

type identityActivityQueryLogger struct {
	logger.Interface
	queries *[]identityActivityQuery
}

func (l identityActivityQueryLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	query, rows := fc()
	*l.queries = append(*l.queries, identityActivityQuery{SQL: query, Rows: rows})
	l.Interface.Trace(ctx, begin, func() (string, int64) { return query, rows }, err)
}

func TestLoadUsageIdentityActivityUsesTwoBoundedIndexedQueries(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageIdentity{ID: 1, Identity: "auth", AuthType: 1}).Error; err != nil {
		t.Fatal(err)
	}
	var events []entities.UsageEvent
	for i := 0; i < 2500; i++ {
		events = append(events, entities.UsageEvent{EventKey: fmt.Sprint(i), AuthType: "oauth", AuthIndex: "auth", Timestamp: now.Add(-time.Duration(i+1) * time.Millisecond), Failed: i%2 == 0})
	}
	for i := 0; i < 1000; i++ {
		events = append(events, entities.UsageEvent{EventKey: fmt.Sprintf("outside-%d", i), AuthType: "oauth", AuthIndex: "auth", Timestamp: now.Add(-6 * time.Hour), Failed: i%2 == 0})
	}
	if err := db.CreateInBatches(events, 100).Error; err != nil {
		t.Fatal(err)
	}
	var queries []identityActivityQuery
	capturedDB := db.Session(&gorm.Session{Logger: identityActivityQueryLogger{Interface: db.Logger, queries: &queries}})
	result, err := repository.LoadUsageIdentityActivity(context.Background(), capturedDB, 1, now)
	if err != nil || result.Latest == nil || !result.Latest.Failed || result.Streak != 1 {
		t.Fatalf("activity = %+v, error %v", result, err)
	}
	var eventQueries []identityActivityQuery
	for _, query := range queries {
		if strings.Contains(strings.ToLower(query.SQL), "usage_events") {
			eventQueries = append(eventQueries, query)
		}
	}
	if len(eventQueries) != 2 {
		t.Fatalf("expected two event queries, got %v", queries)
	}
	for index, query := range eventQueries {
		lower := strings.ToLower(strings.Join(strings.Fields(query.SQL), " "))
		if !strings.Contains(lower, "select id, timestamp, failed, status_code") || !strings.Contains(lower, "limit 101") || strings.Contains(lower, "count(") || query.Rows != 101 {
			t.Fatalf("unexpected activity query: %+v", query)
		}
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN "+query.SQL, "oauth", "auth", index == 1, timeutil.FormatSortableStorageTime(now.Add(-5*time.Hour)), timeutil.FormatSortableStorageTime(now)).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		details := fmt.Sprint(plan)
		if !strings.Contains(details, entities.UsageIdentityActivityIndex) || !strings.Contains(details, "<expr>>?") || !strings.Contains(details, "<expr><?") || strings.Contains(details, "TEMP B-TREE") {
			t.Fatalf("activity query is not an ordered indexed range: %s", details)
		}
		t.Logf("result=%t query plan: %s; fetched %d of 2500 in-window / 3500 total events", index == 1, details, query.Rows)
	}
}
