package test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

func TestOpenDatabaseUsesPinnedSQLiteVersion(t *testing.T) {
	// 准备：通过项目正式数据库入口打开全新文件，版本必须来自本次固定的 go-sqlite3 依赖。
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeResolverTestDatabase(t, db)

	// 执行：直接读取驱动内嵌的 SQLite core 版本，不依赖系统 sqlite3 命令。
	var version string
	if err := db.Raw("SELECT sqlite_version()").Scan(&version).Error; err != nil {
		t.Fatalf("read sqlite version: %v", err)
	}

	// 断言：v1.14.48 固定内嵌 SQLite 3.53.3，防止依赖解析意外回退到旧 WAL bug 版本。
	if version != "3.53.3" {
		t.Fatalf("expected SQLite 3.53.3 from go-sqlite3 v1.14.48, got %q", version)
	}
}

func TestOpenDatabasePoolsAutomaticallyRoutesQueriesAndWrites(t *testing.T) {
	// 准备：统一业务 DB 和硬只读 reader 指向同一个带特殊字符的 WAL 文件。
	dbPath := filepath.Join(t.TempDir(), "app #resolver.db")
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabasePools returned error: %v", err)
	}
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer sql db: %v", err)
	}
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader sql db: %v", err)
	}
	closeResolverTestPools(t, writerSQL, readerSQL)
	if writerSQL == readerSQL {
		t.Fatal("expected file database to use independent writer and reader pools")
	}

	// 准备：独占唯一 writer；若统一 DB 尚未注册 dbresolver，后续 Count 会错误等待这条连接。
	heldWriter, err := writerSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold writer connection: %v", err)
	}
	writerHeld := true
	defer func() {
		if writerHeld {
			_ = heldWriter.Close()
		}
	}()

	// 执行：普通 GORM Query 必须由 dbresolver 自动发往 reader，而不是依赖调用方手选 ReadDB。
	queryContext, cancelQuery := context.WithTimeout(context.Background(), time.Second)
	defer cancelQuery()
	var count int64
	if err := db.WithContext(queryContext).Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("expected routed query to bypass occupied writer: %v", err)
	}
	// 执行：Raw SELECT 也必须遵循 dbresolver 官方 SQL 分类并使用 reader。
	if err := db.WithContext(queryContext).Raw("SELECT COUNT(*) FROM usage_events").Scan(&count).Error; err != nil {
		t.Fatalf("expected routed raw query to bypass occupied writer: %v", err)
	}

	// 执行：同一个统一 DB 发出的 Create 必须自动留在 writer，并在唯一 writer 被占用时排队。
	writeResult := make(chan error, 1)
	go func() {
		writeResult <- db.Create(&entities.UsageEvent{EventKey: "resolver-write", Model: "model-a", Timestamp: time.Now()}).Error
	}()
	select {
	case err := <-writeResult:
		t.Fatalf("expected routed write to wait for occupied writer, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// 执行：释放 writer 后，排队写入必须正常提交，随后统一 DB 的查询从 reader 看到提交结果。
	if err := heldWriter.Close(); err != nil {
		t.Fatalf("release writer connection: %v", err)
	}
	writerHeld = false
	select {
	case err := <-writeResult:
		if err != nil {
			t.Fatalf("routed write returned error after writer release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("routed write did not finish after writer release")
	}
	if err := db.Model(&entities.UsageEvent{}).Where("event_key = ?", "resolver-write").Count(&count).Error; err != nil {
		t.Fatalf("read routed writer commit: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected reader to observe routed writer commit, got %d rows", count)
	}
}

func TestOpenDatabasePoolsKeepsDefaultTransactionOnWriter(t *testing.T) {
	// 准备：占满四条 reader，验证默认事务不依赖任何 reader 连接。
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatalf("OpenDatabasePools returned error: %v", err)
	}
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer sql db: %v", err)
	}
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader sql db: %v", err)
	}
	closeResolverTestPools(t, writerSQL, readerSQL)
	heldReaders := make([]*sql.Conn, 0, 4)
	for index := 0; index < 4; index++ {
		connection, err := readerSQL.Conn(context.Background())
		if err != nil {
			t.Fatalf("hold reader connection %d: %v", index, err)
		}
		heldReaders = append(heldReaders, connection)
	}
	defer func() {
		for _, connection := range heldReaders {
			_ = connection.Close()
		}
	}()

	// 执行：事务中的 SELECT 和 Create 必须始终复用默认 writer 事务连接。
	transactionContext, cancelTransaction := context.WithTimeout(context.Background(), time.Second)
	defer cancelTransaction()
	err = db.WithContext(transactionContext).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
			return err
		}
		return tx.Create(&entities.UsageEvent{EventKey: "writer-transaction", Model: "model-a", Timestamp: time.Now()}).Error
	})
	if err != nil {
		t.Fatalf("expected default transaction to stay on writer: %v", err)
	}

	// 断言：事务提交结果可以直接从 writer 读取，不需要释放被占用的 reader。
	var count int64
	if err := db.Clauses(dbresolver.Write).Model(&entities.UsageEvent{}).Where("event_key = ?", "writer-transaction").Count(&count).Error; err != nil {
		t.Fatalf("read committed transaction from writer: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one committed transaction row, got %d", count)
	}
}

func closeResolverTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql db for cleanup: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close sql db: %v", err)
		}
	})
}

func closeResolverTestPools(t *testing.T, writer, reader *sql.DB) {
	t.Helper()
	t.Cleanup(func() {
		if reader != nil && reader != writer {
			if err := reader.Close(); err != nil {
				t.Fatalf("close reader sql db: %v", err)
			}
		}
		if writer != nil {
			if err := writer.Close(); err != nil {
				t.Fatalf("close writer sql db: %v", err)
			}
		}
	})
}
