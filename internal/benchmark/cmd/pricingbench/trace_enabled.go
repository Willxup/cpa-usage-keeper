//go:build sqlite_trace

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// WriterTraceSummary 只保存真实 writer 的事务时间边界摘要，不保留 SQL 或事件内容。
// 首写语句开始到 COMMIT/ROLLBACK 完成是写锁占用上界，首写完成到提交语句开始是下界；均非精确锁时长。
type WriterTraceSummary struct {
	Enabled                bool   `json:"enabled"`
	Note                   string `json:"note"`
	WriteTransactionCount  int64  `json:"write_transaction_count"`
	RolledBackCount        int64  `json:"rolled_back_count"`
	IncompleteCount        int64  `json:"incomplete_count"`
	MaxWriteUpperNS        int64  `json:"max_write_upper_ns"`
	MaxWriteLowerNS        int64  `json:"max_write_lower_ns"`
	TotalWriteUpperNS      int64  `json:"total_write_upper_ns"`
	TotalWriteLowerNS      int64  `json:"total_write_lower_ns"`
	AutocommitWriteCount   int64  `json:"autocommit_write_count"`
	MaxAutocommitWriteNS   int64  `json:"max_autocommit_write_ns"`
	TotalAutocommitWriteNS int64  `json:"total_autocommit_write_ns"`
}

const writerTraceNote = "first write statement start to COMMIT/ROLLBACK profile is an upper bound; first successful write profile to COMMIT/ROLLBACK statement start is a lower bound; autocommit writes are separate; none is exact SQLite lock time"

type writerTraceKind uint8

const (
	traceOther writerTraceKind = iota
	traceBeginDeferred
	traceBeginWrite
	traceCommit
	traceRollback
	traceWrite
)

type writerTraceStatement struct {
	kind    writerTraceKind
	started time.Time
}

type writerTraceRecorder struct {
	mu                sync.Mutex
	summary           WriterTraceSummary
	statements        map[uintptr]writerTraceStatement
	inTransaction     bool
	firstWriteStarted time.Time
	firstWriteDone    time.Time
}

// startSQLiteWriterTrace 仅在 benchmark 构建中给唯一现有 writer 物理连接装 trace，归还连接后正常迁移照旧运行。
// 停止闭包重新借同一连接卸载 trace；SQLite/连接池配置不变，不引入额外抢写锁探针。
func startSQLiteWriterTrace(ctx context.Context, writer *gorm.DB) (func(context.Context) (WriterTraceSummary, error), error) {
	if writer == nil {
		return nil, fmt.Errorf("pricing benchmark writer is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sqlDB, err := writer.Clauses(dbresolver.Write).DB()
	if err != nil {
		return nil, fmt.Errorf("open pricing benchmark writer pool: %w", err)
	}
	if sqlDB.Stats().MaxOpenConnections != 1 {
		return nil, fmt.Errorf("pricing benchmark trace requires the existing single writer connection")
	}
	recorder := &writerTraceRecorder{
		summary:    WriterTraceSummary{Enabled: true, Note: writerTraceNote},
		statements: make(map[uintptr]writerTraceStatement),
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("borrow pricing benchmark writer connection: %w", err)
	}
	var tracedConn *sqlite3.SQLiteConn
	installErr := conn.Raw(func(driverConn any) error {
		var ok bool
		tracedConn, ok = driverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("pricing benchmark writer is not go-sqlite3")
		}
		return tracedConn.SetTrace(&sqlite3.TraceConfig{
			Callback:  recorder.record,
			EventMask: sqlite3.TraceStmt | sqlite3.TraceProfile,
			// 展开的 SQL 可能含原始消息或凭证，摘要只需操作类型。
			WantExpandedSQL: false,
		})
	})
	closeErr := conn.Close()
	if installErr != nil {
		return nil, fmt.Errorf("install pricing benchmark SQLite trace: %w", installErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("return pricing benchmark writer connection: %w", closeErr)
	}
	var once sync.Once
	var stopped WriterTraceSummary
	var stopErr error
	stop := func(stopCtx context.Context) (WriterTraceSummary, error) {
		once.Do(func() {
			if stopCtx == nil {
				stopCtx = context.Background()
			}
			borrowed, err := sqlDB.Conn(stopCtx)
			if err != nil {
				stopErr = fmt.Errorf("borrow pricing benchmark writer to stop trace: %w", err)
			} else {
				stopErr = borrowed.Raw(func(driverConn any) error {
					current, ok := driverConn.(*sqlite3.SQLiteConn)
					if !ok || current != tracedConn {
						return fmt.Errorf("pricing benchmark writer connection changed during trace")
					}
					return current.SetTrace(nil)
				})
				if err := borrowed.Close(); err != nil && stopErr == nil {
					stopErr = err
				}
			}
			stopped = recorder.snapshot()
		})
		return stopped, stopErr
	}
	return stop, nil
}

// record 用同一物理连接的 SQLite statement/profile 事件计算事务上下界，不写任何 SQL 日志。
func (r *writerTraceRecorder) record(info sqlite3.TraceInfo) int {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	switch info.EventCode {
	case sqlite3.TraceStmt:
		kind := classifyWriterTraceStatement(info.StmtOrTrigger)
		r.statements[info.StmtHandle] = writerTraceStatement{kind: kind, started: now}
		// 失败的首写也可能已取得 SQLite 写锁；从语句开始计上界，不能等成功后才起算。
		if kind == traceWrite && r.inTransaction && r.firstWriteStarted.IsZero() {
			r.firstWriteStarted = now
		}
	case sqlite3.TraceProfile:
		statement, ok := r.statements[info.StmtHandle]
		if !ok {
			return 0
		}
		delete(r.statements, info.StmtHandle)
		if info.DBError.Code != 0 {
			return 0
		}
		switch statement.kind {
		case traceBeginDeferred:
			r.beginTransaction(time.Time{}, time.Time{})
		case traceBeginWrite:
			r.beginTransaction(statement.started, now)
		case traceWrite:
			if r.inTransaction {
				if r.firstWriteDone.IsZero() {
					r.firstWriteDone = now
				}
			} else {
				duration := now.Sub(statement.started).Nanoseconds()
				r.summary.AutocommitWriteCount++
				r.summary.TotalAutocommitWriteNS += duration
				r.summary.MaxAutocommitWriteNS = max(r.summary.MaxAutocommitWriteNS, duration)
			}
		case traceCommit, traceRollback:
			r.endTransaction(statement.started, now, statement.kind == traceRollback)
		}
	}
	return 0
}

// beginTransaction 只在 BEGIN IMMEDIATE/EXCLUSIVE 时把锁取得窗口从 BEGIN 语句开始计算。
func (r *writerTraceRecorder) beginTransaction(writeStarted, writeDone time.Time) {
	if r.inTransaction {
		r.summary.IncompleteCount++
	}
	r.inTransaction = true
	r.firstWriteStarted, r.firstWriteDone = writeStarted, writeDone
}

// endTransaction 用提交语句开始作为仍持锁的下界终点，用其完成作为已释放锁的上界终点。
func (r *writerTraceRecorder) endTransaction(commitStarted, commitDone time.Time, rolledBack bool) {
	if !r.inTransaction {
		return
	}
	if !r.firstWriteStarted.IsZero() {
		upper := commitDone.Sub(r.firstWriteStarted).Nanoseconds()
		lower := int64(0)
		if !r.firstWriteDone.IsZero() {
			lower = max(int64(0), commitStarted.Sub(r.firstWriteDone).Nanoseconds())
		}
		r.summary.WriteTransactionCount++
		if rolledBack {
			r.summary.RolledBackCount++
		}
		r.summary.TotalWriteUpperNS += upper
		r.summary.TotalWriteLowerNS += lower
		r.summary.MaxWriteUpperNS = max(r.summary.MaxWriteUpperNS, upper)
		r.summary.MaxWriteLowerNS = max(r.summary.MaxWriteLowerNS, lower)
	}
	r.inTransaction = false
	r.firstWriteStarted, r.firstWriteDone = time.Time{}, time.Time{}
}

// snapshot 在卸载 trace 后给未正常提交的活动事务单独计数，避免虚报已完成写占用。
func (r *writerTraceRecorder) snapshot() WriterTraceSummary {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.summary
	if r.inTransaction && !r.firstWriteStarted.IsZero() {
		copy.IncompleteCount++
	}
	return copy
}

// classifyWriterTraceStatement 仅识别事务与写入动词；触发器注释、查询和原始参数都不保存。
func classifyWriterTraceStatement(query string) writerTraceKind {
	trimmed := strings.TrimLeft(query, " \t\r\n")
	if trimmed == "" || strings.HasPrefix(trimmed, "--") {
		return traceOther
	}
	end := strings.IndexAny(trimmed, " \t\r\n(")
	if end < 0 {
		end = len(trimmed)
	}
	verb := trimmed[:end]
	switch {
	case strings.EqualFold(verb, "BEGIN"):
		if strings.Contains(strings.ToUpper(trimmed), "IMMEDIATE") || strings.Contains(strings.ToUpper(trimmed), "EXCLUSIVE") {
			return traceBeginWrite
		}
		return traceBeginDeferred
	case strings.EqualFold(verb, "COMMIT"), strings.EqualFold(verb, "END"):
		return traceCommit
	case strings.EqualFold(verb, "ROLLBACK"):
		// ROLLBACK TO 只撤销 savepoint，外层写事务及其 SQLite 锁仍在。
		tail := strings.TrimLeft(trimmed[end:], " \t\r\n")
		if strings.HasPrefix(strings.ToUpper(tail), "TO ") || strings.HasPrefix(strings.ToUpper(tail), "TRANSACTION TO ") {
			return traceOther
		}
		return traceRollback
	case strings.EqualFold(verb, "INSERT"), strings.EqualFold(verb, "UPDATE"), strings.EqualFold(verb, "DELETE"), strings.EqualFold(verb, "REPLACE"),
		strings.EqualFold(verb, "CREATE"), strings.EqualFold(verb, "ALTER"), strings.EqualFold(verb, "DROP"), strings.EqualFold(verb, "REINDEX"), strings.EqualFold(verb, "VACUUM"):
		return traceWrite
	default:
		return traceOther
	}
}
