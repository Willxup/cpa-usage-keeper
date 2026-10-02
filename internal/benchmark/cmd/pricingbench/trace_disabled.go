//go:build !sqlite_trace

package main

import (
	"context"

	"gorm.io/gorm"
)

// WriterTraceSummary 在普通构建中明确标记未启用，不能把零计数误报为零写锁占用。
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

// startSQLiteWriterTrace 的普通构建不修改连接或写入，只给runner明确的未观测标记。
func startSQLiteWriterTrace(_ context.Context, _ *gorm.DB) (func(context.Context) (WriterTraceSummary, error), error) {
	summary := WriterTraceSummary{Enabled: false, Note: "SQLite writer trace requires the sqlite_trace benchmark build tag"}
	return func(context.Context) (WriterTraceSummary, error) { return summary, nil }, nil
}
