package test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type executionRecoverySQLMetrics struct {
	logger.Interface
	hotScanned, archiveScanned, inboxCandidates int64
	eventUpdates, inboxUpdates                  int
	maxUpdateParameters                         int
	firstQueries                                map[string]string
	transactions                                []time.Duration
}

func (m *executionRecoverySQLMetrics) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	statement, rows := fc()
	kind := ""
	switch {
	case strings.HasPrefix(statement, "SELECT `id` FROM `usage_events` "):
		m.hotScanned += rows
		kind = "hot-ID-range"
	case strings.HasPrefix(statement, "SELECT `id` FROM `usage_events_archive` "):
		m.archiveScanned += rows
		kind = "archive-ID-range"
	case strings.HasPrefix(statement, "SELECT id, raw_message FROM `redis_usage_inboxes` "):
		m.inboxCandidates += rows
		kind = "inbox-candidates"
	case strings.HasPrefix(statement, "UPDATE `usage_events` "), strings.HasPrefix(statement, "UPDATE `usage_events_archive` "):
		m.eventUpdates++
		kind = "event-range-update"
	case strings.HasPrefix(statement, "UPDATE `redis_usage_inboxes` "):
		m.inboxUpdates++
		kind = "inbox-guarded-update"
	}
	if kind != "" && m.firstQueries[kind] == "" {
		m.firstQueries[kind] = statement
	}
}

// 只记录迁移数据批次的 Begin→Commit，排除 runner 对已完成历史版本的空事务。
type executionRecoveryMeasuredPool struct {
	gorm.ConnPool
	metrics *executionRecoverySQLMetrics
}

func (p executionRecoveryMeasuredPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	startedAt := time.Now()
	tx, err := p.ConnPool.(gorm.TxBeginner).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &executionRecoveryMeasuredTx{ConnPool: tx, TxCommitter: tx, metrics: p.metrics, startedAt: startedAt}, nil
}

type executionRecoveryMeasuredTx struct {
	gorm.ConnPool
	gorm.TxCommitter
	metrics   *executionRecoverySQLMetrics
	startedAt time.Time
	dataBatch bool
}

func (tx *executionRecoveryMeasuredTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.HasPrefix(query, "UPDATE `usage_events` ") || strings.HasPrefix(query, "UPDATE `usage_events_archive` ") || strings.HasPrefix(query, "UPDATE `redis_usage_inboxes` ") {
		tx.dataBatch = true
		tx.metrics.maxUpdateParameters = max(tx.metrics.maxUpdateParameters, len(args))
	}
	return tx.ConnPool.ExecContext(ctx, query, args...)
}

func (tx *executionRecoveryMeasuredTx) Commit() error {
	err := tx.TxCommitter.Commit()
	if tx.dataBatch {
		tx.metrics.transactions = append(tx.metrics.transactions, time.Since(tx.startedAt))
	}
	return err
}

func observedExecutionRecoveryDB(db *gorm.DB) (*gorm.DB, *executionRecoverySQLMetrics) {
	metrics := &executionRecoverySQLMetrics{Interface: logger.Discard, firstQueries: make(map[string]string)}
	observed := db.Session(&gorm.Session{Logger: metrics, Context: context.Background(), NewDB: true})
	pool := executionRecoveryMeasuredPool{ConnPool: observed.Statement.ConnPool, metrics: metrics}
	observed.Config.ConnPool = pool
	observed.Statement.ConnPool = pool
	return observed, metrics
}

func explainExecutionRecoveryQueries(tb testing.TB, db *gorm.DB, metrics *executionRecoverySQLMetrics) {
	tb.Helper()
	for _, kind := range []string{"hot-ID-range", "archive-ID-range", "event-range-update", "inbox-candidates"} {
		statement := metrics.firstQueries[kind]
		if statement == "" || strings.Contains(strings.ToUpper(statement), "OFFSET") {
			tb.Fatalf("missing or non-cursor query %s: %s", kind, statement)
		}
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&plan).Error; err != nil {
			tb.Fatal(err)
		}
		details := make([]string, len(plan))
		for i := range plan {
			details[i] = plan[i].Detail
		}
		joined := strings.Join(details, "; ")
		if strings.Contains(joined, "SCAN ") || !strings.Contains(joined, "SEARCH ") {
			tb.Fatalf("%s must use bounded indexed search: %s", kind, joined)
		}
		if kind == "inbox-candidates" && !strings.Contains(joined, "idx_redis_usage_inboxes_status_id") {
			tb.Fatalf("inbox cursor must use existing status/ID index: %s", joined)
		}
		tb.Logf("EXPLAIN %s: %s", kind, joined)
	}
}

func TestExecutionRecoveryUsesBoundedIndexedQueries(t *testing.T) {
	db := openExecutionRecoveryDatabase(t, executionRecoveryDatabasePath(t))
	createExecutionRecoverySchema(t, db)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Exec("WITH RECURSIVE rows(id) AS (VALUES(1) UNION ALL SELECT id+1 FROM rows WHERE id < 1103) INSERT INTO " + table + " (id,event_key,execution_id) SELECT id*7, CASE WHEN id=1103 THEN '' ELSE 'existing' END,'execution-' || id FROM rows").Error; err != nil {
			t.Fatal(err)
		}
	}
	rows := make([]entities.RedisUsageInbox, 1103)
	for i := range rows {
		rows[i] = recoveryInbox(int64(i+1), `{"execution_id":"execution-inbox"}`)
	}
	if err := db.CreateInBatches(&rows, 40).Error; err != nil {
		t.Fatal(err)
	}
	observed, metrics := observedExecutionRecoveryDB(db)
	if err := migration.Run(observed); err != nil {
		t.Fatal(err)
	}
	if metrics.hotScanned != 1103 || metrics.archiveScanned != 1103 || metrics.eventUpdates != 6 || metrics.inboxCandidates != 1103 || metrics.inboxUpdates != 3 || len(metrics.transactions) != 9 || metrics.maxUpdateParameters > 999 {
		t.Fatalf("expected one sequential traversal and one update per bounded batch: %+v", metrics)
	}
	explainExecutionRecoveryQueries(t, observed, metrics)
}

func seedExecutionRecoveryBenchmark(b *testing.B, db *gorm.DB, count, matchEvery int) {
	b.Helper()
	// 完整现有表和索引，代表性的 Token/追踪/时间/客户端字段；不在 Go 内保存全量数据。
	key := "'request-' || id"
	if matchEvery > 0 {
		key = fmt.Sprintf("CASE WHEN id %% %d = 0 THEN '' ELSE 'request-' || id END", matchEvery)
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		statement := "WITH RECURSIVE rows(id) AS (VALUES(1) UNION ALL SELECT id+1 FROM rows WHERE id < ?) INSERT INTO " + table + " (id,event_key,request_id,execution_id,trace_id,session_id,node_kind,is_fork,is_compaction,api_group_key,provider,auth_type,auth_index,model,executor_type,source,timestamp,created_at,input_tokens,output_tokens,cached_tokens,total_tokens,latency_ms,client_ip,user_agent) SELECT id*3," + key + "," + key + ",'execution-' || id,'trace-' || id,'benchmark-session','root',0,0,'benchmark-key','codex','oauth','benchmark-auth','gpt-5.1-codex','CodexExecutor','benchmark','2026-10-01T01:02:03+08:00','2026-10-01T01:02:03+08:00',1000,200,100,1200,120,'192.0.2.1','synthetic-benchmark-client/1.0' FROM rows"
		if err := db.Exec(statement, count).Error; err != nil {
			b.Fatal(err)
		}
	}
	largeRaw := `{"request_id":"","execution_id":"benchmark-execution","response_headers":{"benchmark-padding":"` + strings.Repeat("x", 4096) + `"}}`
	inboxCount := count / 10
	if err := db.Exec("WITH RECURSIVE rows(id) AS (VALUES(1) UNION ALL SELECT id+1 FROM rows WHERE id < ?) INSERT INTO redis_usage_inboxes (id,source,message_hash,raw_message,status,attempt_count,last_error,popped_at,created_at,updated_at) SELECT id*5,'redis_subscribe:usage','hash-' || id,CASE WHEN id % 100 = 0 THEN '{\"execution_id\":null}' WHEN id % 20 = 0 THEN ? ELSE '{\"request_id\":\"request\",\"execution_id\":\"execution\"}' END,CASE WHEN id % 10 = 0 THEN 'decode_failed' ELSE 'processed' END,3,CASE WHEN id % 20 = 0 THEN ? ELSE 'another decode error' END,'2026-10-01T01:02:03+08:00','2026-10-01T01:02:03+08:00','2026-10-01T01:02:03+08:00' FROM rows", inboxCount, largeRaw, missingRequestIDError).Error; err != nil {
		b.Fatal(err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		b.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", executionRecoveryVersion).Error; err != nil {
		b.Fatal(err)
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		b.Fatal(err)
	}
}

type executionRecoveryMemorySample struct{ heap, rss atomic.Uint64 }

func (m *executionRecoveryMemorySample) sample() {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	if mem.HeapAlloc > m.heap.Load() {
		m.heap.Store(mem.HeapAlloc)
	}
	// Linux 的进程 RSS 包括 SQLite 原生内存；其它平台仍报告 Go heap。
	if status, err := os.ReadFile("/proc/self/status"); err == nil {
		for line := range strings.SplitSeq(string(status), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) > 1 {
					kilobytes, _ := strconv.ParseUint(fields[1], 10, 64)
					m.rss.Store(max(m.rss.Load(), kilobytes*1024))
				}
				break
			}
		}
	}
}

func BenchmarkExecutionUsageRecovery(b *testing.B) {
	for _, count := range []int{100_000, 1_000_000} {
		for _, hit := range []struct {
			name  string
			every int
		}{{"zero", 0}, {"sparse", 1000}, {"dense", 1}} {
			b.Run(fmt.Sprintf("rows-per-table=%d/%s", count, hit.name), func(b *testing.B) {
				b.ReportAllocs()
				b.StopTimer()
				for iteration := 0; iteration < b.N; iteration++ {
					db, err := repository.OpenDatabase(config.Config{SQLitePath: executionRecoveryDatabasePath(b)})
					if err != nil {
						b.Fatal(err)
					}
					sqlDB, _ := db.DB()
					b.Cleanup(func() { _ = sqlDB.Close() })
					seedExecutionRecoveryBenchmark(b, db, count, hit.every)
					runtime.GC()
					observed, metrics := observedExecutionRecoveryDB(db)
					memory := &executionRecoveryMemorySample{}
					memory.sample()
					initialHeap, initialRSS := memory.heap.Load(), memory.rss.Load()
					stop, done := make(chan struct{}), make(chan struct{})
					go func() {
						defer close(done)
						ticker := time.NewTicker(5 * time.Millisecond)
						defer ticker.Stop()
						for {
							select {
							case <-ticker.C:
								memory.sample()
							case <-stop:
								memory.sample()
								return
							}
						}
					}()
					startedAt := time.Now()
					b.StartTimer()
					err = migration.Run(observed)
					b.StopTimer()
					elapsed := time.Since(startedAt)
					close(stop)
					<-done
					if err != nil {
						b.Fatal(err)
					}
					if metrics.hotScanned != int64(count) || metrics.archiveScanned != int64(count) || metrics.eventUpdates != 2*((count+499)/500) || metrics.inboxCandidates != int64(count/200) || metrics.maxUpdateParameters > 999 {
						b.Fatalf("unbounded/repeated traversal: %+v", metrics)
					}
					for _, table := range []string{"usage_events", "usage_events_archive"} {
						var fixed int64
						if err := db.Table(table).Where("event_key = execution_id").Count(&fixed).Error; err != nil {
							b.Fatal(err)
						}
						want := 0
						if hit.every > 0 {
							want = count / hit.every
						}
						if fixed != int64(want) {
							b.Fatalf("%s fixed=%d want=%d", table, fixed, want)
						}
					}
					var pending int64
					if err := db.Model(&entities.RedisUsageInbox{}).Where("status = 'pending'").Count(&pending).Error; err != nil || pending != int64(count/250) {
						b.Fatalf("inbox eligible count=%d error=%v", pending, err)
					}
					explainExecutionRecoveryQueries(b, observed, metrics)
					slices.Sort(metrics.transactions)
					p50 := metrics.transactions[len(metrics.transactions)/2]
					p95 := metrics.transactions[(len(metrics.transactions)-1)*95/100]
					maxDuration := metrics.transactions[len(metrics.transactions)-1]
					b.Logf("iteration=%d events=%d hot_scanned=%d archive_scanned=%d inbox_candidates=%d inbox_requeued=%d event_batches=%d inbox_batches=%d elapsed=%s tx_p50=%s tx_p95=%s tx_max=%s heap_initial=%d heap_peak=%d rss_initial=%d rss_peak=%d", iteration+1, 2*count, metrics.hotScanned, metrics.archiveScanned, metrics.inboxCandidates, pending, metrics.eventUpdates, metrics.inboxUpdates, elapsed, p50, p95, maxDuration, initialHeap, memory.heap.Load(), initialRSS, memory.rss.Load())
					b.ReportMetric(float64(memory.heap.Load()), "peak-heap-B")
					b.ReportMetric(float64(memory.rss.Load()), "peak-rss-B")
					b.ReportMetric(float64(maxDuration.Nanoseconds()), "max-tx-ns")
					b.ReportMetric(float64(2*count)/elapsed.Seconds(), "events/s")
					_ = sqlDB.Close()
				}
			})
		}
	}
}
