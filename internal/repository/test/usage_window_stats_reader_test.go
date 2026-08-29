package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/plugin/dbresolver"
)

func TestUsageWindowStatsCalculatorReadsRawAndHourlyWhileWriterIsOccupied(t *testing.T) {
	// 文件库提供真实独立 reader；内存 SQLite 无法证明唯一 writer 被占用时的路由行为。
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "quota-reader.db")})
	if err != nil {
		t.Fatalf("open quota reader pools: %v", err)
	}
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load quota writer pool: %v", err)
	}
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load quota reader pool: %v", err)
	}
	t.Cleanup(func() {
		_ = readerSQL.Close()
		_ = writerSQL.Close()
	})

	// 长窗口包含左 raw、hourly 中段和右 raw；短窗口只读取第一段 raw。
	start := time.Date(2026, 7, 20, 10, 30, 0, 0, time.Local)
	end := time.Date(2026, 7, 20, 18, 20, 0, 0, time.Local)
	events := []entities.UsageEvent{
		{EventKey: "quota-reader-left", AuthIndex: "reader-auth", Model: "unpriced", Timestamp: start.Add(15 * time.Minute), TotalTokens: 10},
		{EventKey: "quota-reader-right", AuthIndex: "reader-auth", Model: "unpriced", Timestamp: time.Date(2026, 7, 20, 17, 30, 0, 0, time.Local), TotalTokens: 30},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed quota raw events: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local), AuthIndex: "reader-auth", Model: "unpriced", TotalTokens: 20,
		CreatedAt: start, UpdatedAt: start,
	}).Error; err != nil {
		t.Fatalf("seed quota hourly row: %v", err)
	}

	// 即使调用方传入 Write scope，calculator 也必须为每次统计显式覆盖到 Reader。
	calculator, err := repository.NewUsageWindowStatsCalculator(context.Background(), db.Clauses(dbresolver.Write), pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver())
	if err != nil {
		t.Fatalf("create quota usage calculator: %v", err)
	}
	heldWriter, err := writerSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("occupy quota writer: %v", err)
	}
	writerHeld := true
	defer func() {
		if writerHeld {
			_ = heldWriter.Close()
		}
	}()

	tests := []struct {
		name       string
		windowEnd  time.Time
		wantTokens int64
	}{
		{name: "raw window", windowEnd: start.Add(30 * time.Minute), wantTokens: 10},
		{name: "hourly with raw boundaries", windowEnd: end, wantTokens: 60},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			resultCh := make(chan struct {
				stats repository.UsageWindowStats
				err   error
			}, 1)
			go func() {
				stats, sumErr := calculator.SumByAuthIndex(context.Background(), "reader-auth", start, &testCase.windowEnd)
				resultCh <- struct {
					stats repository.UsageWindowStats
					err   error
				}{stats: stats, err: sumErr}
			}()
			select {
			case result := <-resultCh:
				if result.err != nil {
					t.Fatalf("sum quota usage through reader: %v", result.err)
				}
				if result.stats.Tokens != testCase.wantTokens {
					t.Fatalf("expected %d reader tokens, got %+v", testCase.wantTokens, result.stats)
				}
			case <-time.After(time.Second):
				t.Fatal("quota usage query waited for the occupied writer")
			}
		})
	}

	if err := heldWriter.Close(); err != nil {
		t.Fatalf("release quota writer: %v", err)
	}
	writerHeld = false
}
