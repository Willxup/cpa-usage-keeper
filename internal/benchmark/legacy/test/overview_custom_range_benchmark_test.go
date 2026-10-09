package legacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func BenchmarkUsageOverviewCustomDayRanges(b *testing.B) {
	now := time.Now()
	lastDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	db, queryLogger := openCustomOverviewBenchmarkDatabase(b, lastDay)
	provider := service.NewUsageService(db, pricing.NewCatalog(pricing.EmptySnapshot()))
	router := api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "")

	for _, days := range []int{30, 90, 365} {
		b.Run(fmt.Sprintf("days_%d", days), func(b *testing.B) {
			start := lastDay.AddDate(0, 0, -(days - 1))
			values := url.Values{
				"range": {"custom"},
				"unit":  {"day"},
				"start": {start.Format(time.DateOnly)},
				"end":   {lastDay.Format(time.DateOnly)},
			}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?"+values.Encode(), nil)
			warmup := httptest.NewRecorder()
			queryLogger.rollupRows = 0
			router.ServeHTTP(warmup, request)
			if warmup.Code != http.StatusOK {
				b.Fatalf("Overview status=%d body=%s", warmup.Code, warmup.Body.String())
			}
			var payload struct {
				Series struct {
					Buckets []string `json:"buckets"`
				} `json:"series"`
			}
			if err := json.Unmarshal(warmup.Body.Bytes(), &payload); err != nil {
				b.Fatalf("decode Overview response: %v", err)
			}
			wantPoints := min(days, 90)
			if len(payload.Series.Buckets) != wantPoints {
				b.Fatalf("series points=%d, want %d", len(payload.Series.Buckets), wantPoints)
			}
			wantRollupRows := int64(days * 4)
			if queryLogger.rollupRows != wantRollupRows {
				b.Fatalf("daily rollup rows=%d, want %d", queryLogger.rollupRows, wantRollupRows)
			}

			var responseBytes int
			var rollupRows int64
			b.ReportAllocs()
			for b.Loop() {
				response := httptest.NewRecorder()
				queryLogger.rollupRows = 0
				router.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					b.Fatalf("Overview status=%d body=%s", response.Code, response.Body.String())
				}
				if queryLogger.rollupRows != wantRollupRows {
					b.Fatalf("daily rollup rows=%d, want %d", queryLogger.rollupRows, wantRollupRows)
				}
				responseBytes = response.Body.Len()
				rollupRows = queryLogger.rollupRows
			}
			b.ReportMetric(float64(responseBytes), "response_B")
			b.ReportMetric(float64(rollupRows), "rollup_rows")
		})
	}
}

type overviewBenchmarkQueryLogger struct {
	logger.Interface
	rollupRows int64
}

func (l *overviewBenchmarkQueryLogger) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, rows := query()
	if strings.Contains(sql, "usage_overview_daily_stats") {
		l.rollupRows = rows
	}
}

func openCustomOverviewBenchmarkDatabase(b *testing.B, lastDay time.Time) (*gorm.DB, *overviewBenchmarkQueryLogger) {
	b.Helper()
	db := openLegacyBenchmarkDB(b)

	start := lastDay.AddDate(0, 0, -364)
	rows := make([]entities.UsageOverviewDailyStat, 0, 365*4*4*3)
	for dayIndex := 0; dayIndex < 365; dayIndex++ {
		bucket := start.AddDate(0, 0, dayIndex)
		for apiIndex := 0; apiIndex < 4; apiIndex++ {
			for modelIndex := 0; modelIndex < 4; modelIndex++ {
				for authIndex := 0; authIndex < 3; authIndex++ {
					requests := int64(20 + (dayIndex+apiIndex+modelIndex+authIndex)%17)
					inputTokens := requests * int64(80+modelIndex*10)
					cacheReadTokens := requests * int64((dayIndex+authIndex)%20)
					outputTokens := requests * int64(30+modelIndex*5)
					rows = append(rows, entities.UsageOverviewDailyStat{
						BucketStart: bucket, APIGroupKey: fmt.Sprintf("api-%d", apiIndex), Model: fmt.Sprintf("model-%d", modelIndex),
						AuthIndex: fmt.Sprintf("auth-%d", authIndex), RequestCount: requests, SuccessCount: requests - 1, FailureCount: 1,
						InputTokens: inputTokens, OutputTokens: outputTokens, CacheReadTokens: cacheReadTokens,
						TotalTokens: inputTokens + outputTokens + cacheReadTokens,
					})
				}
			}
		}
	}
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		b.Fatalf("seed daily rollups: %v", err)
	}
	queryLogger := &overviewBenchmarkQueryLogger{Interface: logger.Default.LogMode(logger.Silent)}
	db.Logger = queryLogger
	return db, queryLogger
}
