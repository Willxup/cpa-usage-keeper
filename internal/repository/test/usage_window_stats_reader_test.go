package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/plugin/dbresolver"
)

func TestUsageWindowStatsCalculatorReadsRawAndHourlyWhileWriterIsOccupied(t *testing.T) {
	// 文件库提供真实独立 reader；内存 SQLite 无法证明唯一 writer 被占用时的路由行为。
	db, writerSQL, _ := openTestDatabasePools(t, "quota-reader.db")

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
	defer heldWriter.Close()

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
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stats, err := calculator.SumByAuthIndex(ctx, "reader-auth", start, &testCase.windowEnd)
			if err != nil {
				t.Fatalf("sum quota usage through reader: %v", err)
			}
			if stats.Tokens != testCase.wantTokens {
				t.Fatalf("expected %d reader tokens, got %+v", testCase.wantTokens, stats)
			}
		})
	}

}
