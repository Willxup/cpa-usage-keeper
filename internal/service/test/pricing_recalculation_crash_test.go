package test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

// TestPricingRecalculationProcessCrash 在第二批写事务中强杀独立进程，验证第一批已提交金额仍与汇总一致。
// 重开真实 WAL 数据库后任务为空；重新发起从头重算只补差额，不恢复任务或重复累计请求和 Token。
func TestPricingRecalculationProcessCrash(t *testing.T) {
	if path := os.Getenv("KEEPER_TEST_RECALC_CRASH_DB"); path != "" {
		runPricingRecalculationCrashChild(t, path)
		return
	}
	withUsageServiceLocation(t, "UTC")
	path := filepath.Join(t.TempDir(), "recalculation-crash.db")
	command := exec.Command(os.Args[0], "-test.run=^TestPricingRecalculationProcessCrash$", "-test.count=1")
	command.Env = append(os.Environ(), "KEEPER_TEST_RECALC_CRASH_DB="+path)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	joined := false
	t.Cleanup(func() {
		if !joined {
			_ = command.Process.Kill()
			<-exited
		}
	})
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
waiting:
	for {
		select {
		case err := <-exited:
			joined = true
			t.Fatalf("child exited before second batch: %v\n%s", err, output.String())
		case <-deadline.C:
			_ = command.Process.Kill()
			<-exited
			joined = true
			t.Fatalf("child did not reach second batch\n%s", output.String())
		case <-tick.C:
			if _, err := os.Stat(path + ".ready"); err == nil {
				break waiting
			}
		}
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := <-exited; err == nil {
		joined = true
		t.Fatal("child unexpectedly exited successfully instead of being killed")
	}
	joined = true

	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		readSQL, _ := reader.DB()
		writeSQL, _ := db.DB()
		_ = readSQL.Close()
		_ = writeSQL.Close()
	})
	assertPricingCrashTotals(t, db, 2003)
	checkpointBefore, err := repository.UsageOverviewAggregationCursor(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	var changed int64
	if err := db.Model(&entities.UsageEvent{}).Where("model = ? AND cost_usd = ?", "crash-model", 2).Count(&changed).Error; err != nil || changed != 1000 {
		t.Fatalf("committed changed events=%d, want 1000: %v", changed, err)
	}
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	current, err := provider.CurrentPricingRecalculation(context.Background())
	if err != nil || current != nil {
		t.Fatalf("crash resurrected an in-memory task: %+v, %v", current, err)
	}
	models, err := provider.ListPricingModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{
		StartAt: now.Add(-90 * time.Minute), ConfigRevision: models.ConfigRevision,
	})
	if err != nil || !response.Started {
		t.Fatalf("fresh recalculation after crash: %+v, %v", response, err)
	}
	provider.WaitPricingRecalculation()
	current, err = provider.CurrentPricingRecalculation(context.Background())
	if err != nil || current == nil || current.Status != servicedto.RecalculationCompleted || current.ProcessedCount != 1002 {
		t.Fatalf("rerun did not complete all target events: %+v, %v", current, err)
	}
	assertPricingCrashTotals(t, db, 2005)
	checkpointAfter, err := repository.UsageOverviewAggregationCursor(context.Background(), db)
	if err != nil || checkpointAfter != checkpointBefore {
		t.Fatalf("recalculation changed the covered checkpoint: before=%+v after=%+v err=%v", checkpointBefore, checkpointAfter, err)
	}
}

// runPricingRecalculationCrashChild 在第二页已更新一条但尚未提交时阻塞事务；测试回调仅报告强杀时点。
func runPricingRecalculationCrashChild(t *testing.T, path string) {
	t.Helper()
	withUsageServiceLocation(t, "UTC")
	db, _, err := repository.OpenDatabasePools(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 12, 30, 0, 0, time.UTC)
	cost, available := 1.0, true
	events := make([]entities.UsageEvent, 1003)
	for index := range events {
		events[index] = entities.UsageEvent{EventKey: fmt.Sprintf("crash-event-%d", index), Model: "crash-model",
			Timestamp: now.Add(-50 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000,
			CostUSD: &cost, CostAvailable: &available}
	}
	events[1002].Timestamp = now.Add(-150 * time.Minute) // 范围外的同日贡献必须保留。
	if err := db.CreateInBatches(&events, 100).Error; err != nil {
		t.Fatal(err)
	}
	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	saved, err := provider.SavePricingModel(context.Background(), completeServiceConfig("crash-model", 2))
	if err != nil {
		t.Fatal(err)
	}
	updated := 0
	if err := db.Callback().Update().Before("gorm:update").Register("test:block_second_repricing_batch", func(tx *gorm.DB) {
		if tx.Statement.Table != "usage_events" {
			return
		}
		updated++
		if updated == 1002 {
			if err := os.WriteFile(path+".ready", []byte("second batch has started"), 0600); err != nil {
				tx.AddError(err)
				return
			}
			<-make(chan struct{}) // 父进程强杀；不模拟正常退出或事务回滚。
		}
	}); err != nil {
		t.Fatal(err)
	}
	response, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{
		StartAt: now.Add(-90 * time.Minute), ConfigRevision: saved.ConfigRevision,
	})
	if err != nil || !response.Started {
		t.Fatalf("child start: %+v, %v", response, err)
	}
	provider.WaitPricingRecalculation()
	t.Fatal("child recalculation returned without being killed")
}

// assertPricingCrashTotals 分别核对事件、小时、日总额以及请求和 Token，防止只验证金额掩盖重复聚合。
func assertPricingCrashTotals(t *testing.T, db *gorm.DB, want float64) {
	t.Helper()
	for _, table := range []string{"usage_events", "usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var cost float64
		if err := db.Table(table).Select("SUM(cost_usd)").Scan(&cost).Error; err != nil || !usageFilterCostClose(cost, want) {
			t.Fatalf("%s total=%v want=%v: %v", table, cost, want, err)
		}
		countExpr := "SUM(request_count)"
		if table == "usage_events" {
			countExpr = "COUNT(*)"
		}
		var totals struct{ Requests, Tokens int64 }
		if err := db.Table(table).Select(countExpr + " AS requests, SUM(total_tokens) AS tokens").Scan(&totals).Error; err != nil || totals.Requests != 1003 || totals.Tokens != 1_003_000_000 {
			t.Fatalf("%s original facts changed: %+v, %v", table, totals, err)
		}
	}
}
