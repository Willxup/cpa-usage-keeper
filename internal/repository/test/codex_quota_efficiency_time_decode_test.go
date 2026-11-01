package test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 私有读取 helper 的窄访问用于对照真实驱动与原 serializer，不新增公开生产接口。
//
//go:linkname scanQuotaEfficiencyEventTime cpa-usage-keeper/internal/repository.scanCodexQuotaEfficiencyEventTime
func scanQuotaEfficiencyEventTime(value any) (time.Time, error)

//go:linkname scanQuotaEfficiencyObservedTime cpa-usage-keeper/internal/repository.scanCodexQuotaEfficiencyObservedTime
func scanQuotaEfficiencyObservedTime(value any) (time.Time, error)

type quotaEfficiencyDecodedEventTime struct{ time.Time }

func (at *quotaEfficiencyDecodedEventTime) Scan(value any) error {
	decoded, err := scanQuotaEfficiencyEventTime(value)
	if err == nil {
		at.Time = decoded
	}
	return err
}

type quotaEfficiencyDecodedTimeReference struct {
	Timestamp time.Time `gorm:"serializer:sortableTime"`
}

func TestQuotaEfficiencyTimestampDecodingMatchesDriver(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/Los_Angeles"} {
		t.Run(zone, func(t *testing.T) {
			setAnalysisLatencyTestTimezone(t, zone)
			db := openTestDatabase(t).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sqlDB.Exec("CREATE TABLE quota_decode_values (id INTEGER PRIMARY KEY, timestamp DATETIME)"); err != nil {
				t.Fatal(err)
			}
			rawTypes := make(map[string]bool)
			for _, sample := range quotaEfficiencyTimestampSamples() {
				t.Run(sample.name, func(t *testing.T) {
					var id int64
					if err := sqlDB.QueryRow("INSERT INTO quota_decode_values(timestamp) VALUES (?) RETURNING id", sample.value).Scan(&id); err != nil {
						t.Fatal(err)
					}
					var raw any
					if err := sqlDB.QueryRow("SELECT +timestamp AS timestamp FROM quota_decode_values WHERE id = ?", id).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					rawTypes[fmt.Sprintf("%T", raw)] = true
					var reference time.Time
					oldErr := sqlDB.QueryRow("SELECT timestamp FROM quota_decode_values WHERE id = ?", id).Scan(&reference)
					var decoded quotaEfficiencyDecodedEventTime
					newErr := sqlDB.QueryRow("SELECT +timestamp AS timestamp FROM quota_decode_values WHERE id = ?", id).Scan(&decoded)
					assertQuotaEfficiencyDecodeError(t, oldErr, newErr)
					if oldErr == nil {
						// 与事件累计入口一样，在直接 Scan 成功之后才归一化。
						assertQuotaEfficiencyDecodedTime(t, timeutil.NormalizeStorageTime(reference), timeutil.NormalizeStorageTime(decoded.Time))
					}
					var observed quotaEfficiencyDecodedTimeReference
					oldErr = db.Table("quota_decode_values").Select("timestamp").Where("id = ?", id).Find(&observed).Error
					actual, newErr := scanQuotaEfficiencyObservedTime(raw)
					assertQuotaEfficiencyDecodeError(t, oldErr, newErr)
					if oldErr == nil {
						assertQuotaEfficiencyDecodedTime(t, observed.Timestamp, actual)
					}
				})
			}
			for _, kind := range []string{"<nil>", "int64", "float64", "[]uint8", "string"} {
				if !rawTypes[kind] {
					t.Fatalf("missing SQLite runtime type %s: %v", kind, rawTypes)
				}
			}
		})
	}
}

func quotaEfficiencyTimestampSamples() []struct {
	name  string
	value any
} {
	var samples []struct {
		name  string
		value any
	}
	add := func(name string, value any) {
		samples = append(samples, struct {
			name  string
			value any
		}{name, value})
	}
	for _, separator := range []string{"T", " "} {
		for _, suffix := range []string{"Z", "+00:00", "+08:00", "-07:00", "+10:30", "-00:00", ""} {
			for digits := 0; digits <= 12; digits++ {
				fraction := ""
				if digits > 0 {
					fraction = "." + "123456789123"[:digits]
				}
				value := "2026-08-21" + separator + "10:15:30" + fraction + suffix
				add(value, value)
			}
		}
	}
	for index, value := range []string{
		"2026-08-21", "2026-08-21Z", "2026-08-21 10:15", "2026-08-21T10:15",
		"2026-08-21T10:15Z", "2026-08-21T10:15:30,123456789Z", "2026-08-21 10:15:30,123+08:00",
		"2026-11-01T01:30:00-07:00", "2026-11-01T01:30:00-08:00", "2026-04-05T01:45:00+10:30",
		"0000-01-01T00:00:00Z", "0001-01-01T00:00:00Z", "9999-12-31T23:59:59.999999999-07:00",
		"2026-08-21T10:15:30+24:00", "2026-08-21T10:15:30+00:60", "2026-08-21T10:15:30-24:60",
		"", " ", "invalid", "2026-02-30T10:15:30Z", "2026-08-21T24:00:00Z", "2026-08-21T10:15:60Z",
		"2026-08-21T1:15:30Z", "2026-08-21t10:15:30z", "2026-08-21T10:15:30.Z", "2026-08-21T10:15:30ZZ",
		"2026-08-21T10:15:30+08:00Z", " 2026-08-21T10:15:30Z", "2026-08-21T10:15:30Z ",
		"2026-08-21T10:15:30Z\x00", "2026-08-21T10:15:30+8:00", "2026-08-21T10:15:30+08:0",
	} {
		add(fmt.Sprintf("text-edge-%02d", index), value)
	}
	for _, value := range []int64{math.MinInt64, math.MaxInt64, -1000000000001, -1000000000000, -999999999999, -1, 0, 1, 999999999999, 1000000000000, 1000000000001, 1787307330123} {
		add(fmt.Sprintf("integer-%d", value), value)
	}
	add("null", nil)
	for index, value := range []float64{1.25, -1.25, math.Inf(1), math.Inf(-1), math.NaN()} {
		add(fmt.Sprintf("real-%d", index), value)
	}
	for index, value := range [][]byte{
		{}, []byte("2026-08-21T10:15:30.123456789Z"), []byte("2026-08-21 10:15:30.123456789"),
		[]byte(" 2026-08-21T10:15:30Z "), []byte("invalid"), []byte("2026-08-21"), {0, 255},
	} {
		add(fmt.Sprintf("blob-%d", index), value)
	}
	return samples
}

func assertQuotaEfficiencyDecodeError(t *testing.T, reference, actual error) {
	t.Helper()
	if fmt.Sprint(reference) != fmt.Sprint(actual) {
		t.Fatalf("decoding error differs:\nreference: %v\nactual:    %v", reference, actual)
	}
}

func assertQuotaEfficiencyDecodedTime(t *testing.T, reference, actual time.Time) {
	t.Helper()
	if !reference.Equal(actual) || reference.Nanosecond() != actual.Nanosecond() ||
		reference.Location().String() != actual.Location().String() || reference.Format(time.RFC3339Nano) != actual.Format(time.RFC3339Nano) || reference.IsZero() != actual.IsZero() {
		t.Fatalf("decoded time differs:\nreference: %s ns=%d location=%s zero=%v\nactual:    %s ns=%d location=%s zero=%v", reference.Format(time.RFC3339Nano), reference.Nanosecond(), reference.Location(), reference.IsZero(), actual.Format(time.RFC3339Nano), actual.Nanosecond(), actual.Location(), actual.IsZero())
	}
}

func TestQuotaEfficiencyDatetimeProjectionPreservesTypes(t *testing.T) {
	db := openTestDatabase(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []struct{ table, name string }{
		{"usage_events", "timestamp"}, {"quota_percent_segments", "first_observed_at"}, {"quota_percent_segments", "last_observed_at"},
	} {
		rows, err := sqlDB.Query("SELECT " + column.name + ", +" + column.name + " AS raw_time FROM " + column.table + " LIMIT 0")
		if err != nil {
			t.Fatal(err)
		}
		types, err := rows.ColumnTypes()
		_ = rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(types[0].DatabaseTypeName(), "DATETIME") || types[1].DatabaseTypeName() != "" {
			t.Fatalf("unexpected declaration for %s.%s: original=%s projection=%s", column.table, column.name, types[0].DatabaseTypeName(), types[1].DatabaseTypeName())
		}
	}
	if _, err := sqlDB.Exec("CREATE TABLE quota_decode_types (id INTEGER PRIMARY KEY, timestamp DATETIME)"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{nil, int64(1000000000001), 1.25, []byte("2026-08-21T00:00:00Z"), "2026-08-21T00:00:00.123456789Z"} {
		var original, projected any
		if err := sqlDB.QueryRow("INSERT INTO quota_decode_types(timestamp) VALUES (?) RETURNING +timestamp, +timestamp AS timestamp", value).Scan(&original, &projected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(value, original) || !reflect.DeepEqual(value, projected) {
			t.Fatalf("projection changed runtime value: input=%T(%v) original=%T(%v) projected=%T(%v)", value, value, original, original, projected, projected)
		}
	}
}

func TestQuotaEfficiencyObservedDecoderPreservesGORMErrors(t *testing.T) {
	for _, sample := range []struct {
		name  string
		first any
		last  any
	}{
		{"real", 1.25, 2.5},
		{"invalid-blob", []byte("bad-first"), []byte("bad-last")},
		{"valid-blob", []byte("2026-08-21T01:00:00.123456789Z"), []byte("2026-08-21 01:00:00.123456789")},
		{"integer", int64(math.MinInt64), int64(math.MaxInt64)},
		{"invalid-text", "bad-first", "bad-last"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			db := openTestDatabase(t).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
			now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
			cycle := seedQuotaEfficiencyTimeCycle(t, db, "codex", "primary", "2026-08-21T00:00:00Z", "2026-08-21T05:00:00Z", []string{"2026-08-21T01:00:00Z", "2026-08-21T02:00:00Z"})
			if err := db.Exec("PRAGMA ignore_check_constraints = ON").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("UPDATE quota_percent_segments SET first_observed_at = ?, last_observed_at = ? WHERE cycle_id = ?", sample.first, sample.last, cycle.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("PRAGMA ignore_check_constraints = OFF").Error; err != nil {
				t.Fatal(err)
			}
			var reference []struct {
				ID               int64
				CycleID          int64
				RemainingPercent int
				FirstObservedAt  time.Time `gorm:"serializer:sortableTime"`
				LastObservedAt   time.Time `gorm:"serializer:sortableTime"`
				ObservationCount int64
			}
			oldErr := db.Table("quota_percent_segments").Select("id, cycle_id, remaining_percent, first_observed_at, last_observed_at, observation_count").Where("cycle_id = ?", cycle.ID).
				Order("cycle_id ASC, first_observed_at ASC, id ASC").Find(&reference).Error
			_, newErr := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{AuthIndex: "time-auth", Now: now, RangeStart: now.Add(-30 * 24 * time.Hour)})
			if oldErr != nil {
				if newErr == nil || newErr.Error() != "list codex quota efficiency segments: "+oldErr.Error() {
					t.Fatalf("GORM field/row error order changed: reference=%v actual=%v", oldErr, newErr)
				}
			} else if newErr != nil {
				t.Fatal(newErr)
			}
		})
	}
}

func TestQuotaEfficiencySegmentProjectionPreservesQueryPlan(t *testing.T) {
	db := openTestDatabase(t)
	now, _ := seedQuotaEfficiencyProjectionHistory(t, db, "codex")
	var statements []quotaEfficiencyProjectionStatement
	queryDB := db.Session(&gorm.Session{Logger: quotaEfficiencyProjectionLogger{Interface: logger.Default.LogMode(logger.Silent), statements: &statements}})
	queryQuotaEfficiencyTimeHistory(t, queryDB, "codex", nil, now)
	statement := statements[1].sql
	original := strings.ReplaceAll(strings.ReplaceAll(statement, "+first_observed_at AS first_observed_at", "first_observed_at"), "+last_observed_at AS last_observed_at", "last_observed_at")
	original = strings.ReplaceAll(original, "quota_percent_segments.", "")
	var reference, actual []struct{ Detail string }
	if err := db.Raw("EXPLAIN QUERY PLAN " + original).Scan(&reference).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&actual).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reference, actual) {
		t.Fatalf("segment projection changed original query plan: reference=%+v actual=%+v", reference, actual)
	}
}

var _ sql.Scanner = (*quotaEfficiencyDecodedEventTime)(nil)
