package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	// sqliteWriterMaxOpenConnections 保证整个应用同一时刻只有一条 SQLite 写连接。
	sqliteWriterMaxOpenConnections = 1
	// sqliteWriterMaxIdleConnections 常驻唯一 writer，避免写事务反复创建物理连接。
	sqliteWriterMaxIdleConnections = 1
	// sqliteReaderMaxOpenConnections 允许突发统计查询按需扩到八条独立只读连接。
	sqliteReaderMaxOpenConnections = 8
	// sqliteReaderMaxIdleConnections 允许已经按需建立的四条 reader 在空闲后继续复用。
	sqliteReaderMaxIdleConnections = 4
)

// OpenDatabasePools 为文件库注册 GORM 官方读写路由，并保持旧内存库的单池语义。
func OpenDatabasePools(cfg config.Config) (*gorm.DB, *gorm.DB, error) {
	// writer 始终先完成 WAL、schema 和迁移，避免初始化查询或写入在 reader 创建前被自动路由。
	db, err := OpenDatabase(cfg)
	if err != nil {
		return nil, nil, err
	}
	// :memory: 和临时内存 URI 按连接隔离，必须复用 writer 才能保持原来同一份数据库。
	if sqliteDatabaseRequiresSinglePool(cfg.SQLitePath) {
		return db, db, nil
	}
	// 文件数据库先打开硬只读 reader；它仍与 writer 指向同一个 app.db 和 WAL/SHM。
	reader, err := OpenReadDatabase(cfg)
	if err != nil {
		closeDatabasePool(db)
		return nil, nil, err
	}
	readerSQL, err := reader.DB()
	if err != nil {
		closeDatabasePool(reader)
		closeDatabasePool(db)
		return nil, nil, fmt.Errorf("configure sqlite read database resolver: %w", err)
	}

	// dbresolver 只负责路由：Query/Row 使用 reader，Create/Update/Delete 和默认事务继续使用 writer。
	resolver := dbresolver.Register(dbresolver.Config{
		Replicas: []gorm.Dialector{sqlite.New(sqlite.Config{Conn: readerSQL})},
	})
	if err := db.Use(resolver); err != nil {
		closeDatabasePool(reader)
		closeDatabasePool(db)
		return nil, nil, fmt.Errorf("register sqlite database resolver: %w", err)
	}

	// 第一个句柄供全部业务代码统一使用；第二个句柄仅保留 reader 的生命周期和池状态入口。
	return db, reader, nil
}

// closeDatabasePool 回收尚未交给 App 的局部数据库池；初始化错误不能遗留文件描述符。
func closeDatabasePool(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// OpenDatabase 统一创建 GORM SQLite 连接，并按新库/旧库分支执行初始化或迁移。
func OpenDatabase(cfg config.Config) (*gorm.DB, error) {
	// 先判断物理文件是否存在，后续用它区分全新数据库和需要跑迁移的旧库。
	databaseExists, err := sqliteDatabaseFileExists(cfg.SQLitePath)
	if err != nil {
		return nil, err
	}
	// SQLite DSN 统一补齐 busy_timeout/foreign_keys，调用方只需要传项目配置里的路径。
	dsn := sqliteDSN(cfg.SQLitePath)
	// GORM 自动时间也先落到项目 TZ，再由 storageTime serializer 输出统一字符串。
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:  logging.NewGORMLogger(),
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %s: %w", filepath.Clean(cfg.SQLitePath), err)
	}
	// GORM 已经创建底层连接池；后续任一初始化步骤失败都必须在返回前统一回收。
	closeOnError := true
	defer func() {
		if closeOnError {
			closeDatabasePool(db)
		}
	}()

	// SQLite 写入仍是单 writer，连接池限制成单连接，避免同进程多个连接互相抢写锁。
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("configure sqlite database: %w", err)
	}
	// writer 的打开连接上限固定为一，database/sql 会让其它写操作在池外排队。
	sqlDB.SetMaxOpenConns(sqliteWriterMaxOpenConnections)
	// 唯一 writer 空闲时继续常驻，保持连接级 PRAGMA 和写入时延稳定。
	sqlDB.SetMaxIdleConns(sqliteWriterMaxIdleConnections)

	// WAL 让读写并发更友好，但 writer 之间仍串行；读取 PRAGMA 返回值，不能把“执行无报错”误当成已经进入 WAL。
	var journalMode string
	if err := db.Raw("PRAGMA journal_mode=WAL").Scan(&journalMode).Error; err != nil {
		return nil, fmt.Errorf("enable sqlite WAL: %w", err)
	}
	// 文件库的独立 reader 依赖 WAL；内存库不支持 WAL，继续沿用原来的单池 memory journal 语义。
	if !sqliteDatabaseRequiresSinglePool(cfg.SQLitePath) && !strings.EqualFold(strings.TrimSpace(journalMode), "wal") {
		return nil, fmt.Errorf("enable sqlite WAL: journal mode is %q", journalMode)
	}
	if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
		return nil, fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	if err := db.Exec("PRAGMA foreign_keys=ON").Error; err != nil {
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	// 空文件和新文件都按新库处理，直接 AutoMigrate 到当前 schema 后标记历史迁移已完成。
	hasTables, err := sqliteDatabaseHasTables(db)
	if err != nil {
		return nil, err
	}
	if !databaseExists || !hasTables {
		if err := db.AutoMigrate(entities.All()...); err != nil {
			return nil, fmt.Errorf("auto migrate fresh database: %w", err)
		}
		if err := migration.MarkAllAsApplied(db); err != nil {
			return nil, fmt.Errorf("mark schema migrations applied: %w", err)
		}
		// 新库初始化已完成，连接池开始由调用方负责生命周期。
		closeOnError = false
		return db, nil
	}

	// 已有业务表的数据库必须走显式迁移，确保旧库按版本顺序补齐结构和索引。
	if err := migration.Run(db); err != nil {
		return nil, fmt.Errorf("run schema migrations: %w", err)
	}

	// 旧库迁移已完成，连接池开始由调用方负责生命周期。
	closeOnError = false
	return db, nil
}

// OpenReadDatabase 为纯查询路径创建独立只读池；schema 初始化和所有写事务仍只由 OpenDatabase 负责。
func OpenReadDatabase(cfg config.Config) (*gorm.DB, error) {
	// writer 必须先完成 WAL 与 schema 初始化，这里只基于同一路径构造只读 DSN。
	dsn, err := sqliteReadDSN(cfg.SQLitePath)
	// 内存库或非法 query 参数无法形成独立硬只读 URI，必须在打开连接前明确失败。
	if err != nil {
		return nil, fmt.Errorf("build sqlite read database DSN %s: %w", filepath.Clean(cfg.SQLitePath), err)
	}
	// reader 使用与 writer 相同的项目时区 NowFunc，保证查询参数和 GORM 时间处理语义一致。
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:  logging.NewGORMLogger(),
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	// 只读文件或 DSN 无法打开时直接终止 App 初始化，禁止静默退回 writer 连接。
	if err != nil {
		return nil, fmt.Errorf("open sqlite read database %s: %w", filepath.Clean(cfg.SQLitePath), err)
	}
	// reader 在完成池配置前仍属于局部资源，中途失败时不能遗留只读文件句柄。
	closeOnError := true
	defer func() {
		if closeOnError {
			closeDatabasePool(db)
		}
	}()

	// 取出 GORM 底层 database/sql 池，读并发上限必须配置在这个共享池上。
	sqlDB, err := db.DB()
	// 无法取得底层池代表 reader 配置不完整，不能向上层暴露半初始化连接。
	if err != nil {
		return nil, fmt.Errorf("configure sqlite read database: %w", err)
	}
	// 最多八条 reader 同时执行纯查询；第九个读取由 database/sql 排队等待空闲连接。
	sqlDB.SetMaxOpenConns(sqliteReaderMaxOpenConnections)
	// 峰值结束后允许保留最多四条已建立 reader；SetMaxIdleConns 本身不会主动预热连接。
	sqlDB.SetMaxIdleConns(sqliteReaderMaxIdleConnections)
	// reader 配置已完成，连接池开始由调用方负责生命周期。
	closeOnError = false
	// 返回文件库的独立 reader；上层只把它用于生命周期和连接池状态管理。
	return db, nil
}

// sqliteDSN 在调用方没有自定义 query 参数时追加 SQLite 连接级默认参数。
func sqliteDSN(path string) string {
	// 保留调用方显式传入的 DSN 参数，避免覆盖测试或特殊部署配置。
	trimmed := strings.TrimSpace(path)
	if strings.Contains(trimmed, "?") {
		return trimmed
	}
	return trimmed + "?_busy_timeout=5000&_foreign_keys=on"
}

// sqliteReadDSN 把文件路径规范化为 SQLite URI，并强制底层只读模式与连接级 query_only 保护。
func sqliteReadDSN(path string) (string, error) {
	// 内存数据库无法同时使用 mode=memory 和 mode=ro，调用方必须复用原 writer 池。
	if sqliteDatabaseRequiresSinglePool(path) {
		return "", fmt.Errorf("memory database requires the writer pool")
	}
	// 文件名和 query 参数分开处理，避免简单追加产生重复 mode 或 _query_only 参数。
	trimmed := strings.TrimSpace(path)
	filename, rawQuery, hasQuery := strings.Cut(trimmed, "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", fmt.Errorf("parse sqlite query parameters: %w", err)
	}
	// 无自定义 query 时沿用 writer 的连接级默认值；显式参数则保持旧 DSN 的覆盖语义。
	if !hasQuery {
		query.Set("_busy_timeout", "5000")
		query.Set("_foreign_keys", "on")
	}
	// Set 会删除同名冲突值，保证调用方不能用参数顺序关闭 reader 的两层保护。
	query.Set("mode", "ro")
	query.Set("_query_only", "on")
	query.Set("cache", "private")

	// go-sqlite3 只有 file: URI 才会把 mode=ro 交给 SQLite；绝对化还能避免相对路径被误作 URI authority。
	uriFilename := filename
	if !strings.HasPrefix(strings.ToLower(uriFilename), "file:") {
		absoluteFilename, err := filepath.Abs(uriFilename)
		if err != nil {
			return "", fmt.Errorf("resolve sqlite file path: %w", err)
		}
		uriFilename = BuildSQLiteFileURI(absoluteFilename)
	}
	// 返回唯一 query string；SQLite core 处理 mode=ro，驱动处理下划线开头的 PRAGMA 参数。
	return uriFilename + "?" + query.Encode(), nil
}

// BuildSQLiteFileURI 把已经绝对化的本地文件名转换成 SQLite file URI，并保留跨平台路径语义。
func BuildSQLiteFileURI(filename string) string {
	// Windows 盘符必须位于 URI path 的 /C:/... 中；缺少前导斜杠会被 net/url 误写成 authority。
	uriPath := filepath.ToSlash(filename)
	if len(uriPath) >= 2 && uriPath[1] == ':' && ((uriPath[0] >= 'A' && uriPath[0] <= 'Z') || (uriPath[0] >= 'a' && uriPath[0] <= 'z')) {
		uriPath = "/" + uriPath
	}
	// url.URL 继续负责空格、# 等字符的标准转义；Unix 绝对路径和 UNC 路径保持原样。
	return (&url.URL{Scheme: "file", Path: uriPath}).String()
}

// sqliteDatabaseRequiresSinglePool 判断路径是否创建连接私有的内存/临时数据库。
func sqliteDatabaseRequiresSinglePool(path string) bool {
	// 空路径和裸 :memory: 都会为每条 SQLite 连接创建互不相同的数据库。
	trimmed := strings.TrimSpace(path)
	filename, rawQuery, _ := strings.Cut(trimmed, "?")
	if filename == "" || strings.EqualFold(filename, ":memory:") || strings.EqualFold(filename, "file::memory:") {
		return true
	}
	// 命名 URI 的 mode=memory 同样属于内存库；解析失败留给 DSN 构造函数返回明确错误。
	query, err := url.ParseQuery(rawQuery)
	return err == nil && strings.EqualFold(strings.TrimSpace(query.Get("mode")), "memory")
}

// sqliteDatabaseFileExists 判断磁盘数据库文件是否存在；内存库和空路径都按新库处理。
func sqliteDatabaseFileExists(path string) (bool, error) {
	// 内存 DSN 没有物理文件；必须在 os.Stat 前识别，避免 Windows 把 file::memory: 当成非法文件名。
	if sqliteDatabaseRequiresSinglePool(path) {
		return false, nil
	}
	trimmed := strings.TrimSpace(path)
	if before, _, ok := strings.Cut(trimmed, "?"); ok {
		trimmed = before
	}
	_, err := os.Stat(trimmed)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("check sqlite database %s: %w", filepath.Clean(trimmed), err)
}

// sqliteDatabaseHasTables 用业务表数量判断文件是否已经初始化过。
func sqliteDatabaseHasTables(db *gorm.DB) (bool, error) {
	var count int64
	// sqlite_% 是 SQLite 内部表，不能用来判断项目 schema 是否存在。
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&count).Error; err != nil {
		return false, fmt.Errorf("check sqlite database tables: %w", err)
	}
	return count > 0, nil
}

// InsertUsageEvents 按 Redis inbox 消费结果逐条落库；request_id/event_key 重复也保留为独立事件。
func InsertUsageEvents(db *gorm.DB, events []entities.UsageEvent) (int, int, error) {
	if db == nil {
		return 0, 0, fmt.Errorf("database is nil")
	}
	if len(events) == 0 {
		return 0, 0, nil
	}

	// 仍保留 deduped 返回位是为了兼容上层结果结构；当前语义固定为不去重。
	inserted := 0

	err := db.Transaction(func(tx *gorm.DB) error {
		// 按仓储默认批次拆分写入，避免单条 INSERT 的 SQLite 变量数量过多。
		for start := 0; start < len(events); start += insertBatchSize(entities.UsageEvent{}) {
			end := min(start+insertBatchSize(entities.UsageEvent{}), len(events))
			batch := events[start:end]
			// 入库前统一规范时间，确保 storageTime 字符串比较和后续增量聚合使用同一时区语义。
			for index := range batch {
				batch[index].Timestamp = timeutil.NormalizeStorageTime(batch[index].Timestamp)
			}

			// Redis 队列是消费型数据源，同 request_id/event_key 的消息也代表独立消费记录。
			result := tx.Create(&batch)
			if result.Error != nil {
				return fmt.Errorf("insert usage events: %w", result.Error)
			}
			inserted += int(result.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	return inserted, 0, nil
}

const usageEventsRetentionDays = 90

// CleanupStorage 是每日维护任务的统一仓储入口：先清 inbox、归档 raw events，再清限期统计并条件式整理空闲页。
func CleanupStorage(db *gorm.DB, now time.Time) (dto.StorageCleanupResult, error) {
	redisResult, err := CleanupRedisUsageInbox(db, now)
	if err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult}, err
	}
	usageEventsArchive, err := ArchiveExpiredUsageEvents(databaseContext(db), db, now)
	result := dto.StorageCleanupResult{
		RedisInbox:               redisResult,
		UsageEventsArchived:      usageEventsArchive.Archived,
		UsageEventsArchiveStatus: usageEventsArchive.Status,
	}
	if err != nil {
		return result, err
	}
	// Activity 的 short/medium/long 分别按自身 retention 清理，daily 永久保留。
	if err := CleanupUsageActivityStats(db, now); err != nil {
		return result, err
	}
	// Latency 小时保留 3 天、自然日保留 365 天；空闲页由后面的条件式 VACUUM 统一评估。
	if err := CleanupUsageLatencyStats(db, now); err != nil {
		return result, err
	}
	vacuumResult, err := maybeVacuumStorage(db)
	result.Vacuum = vacuumResult
	if err != nil {
		return result, err
	}
	return result, nil
}

func usageEventAggregationsCaughtUp(tx *gorm.DB) (bool, error) {
	// 当前最大 event ID 是三个全局 checkpoint 和每行 Identity cursor 的共同安全目标。
	var maxEventID int64
	if err := tx.Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&maxEventID).Error; err != nil {
		return false, fmt.Errorf("load usage event archive watermark: %w", err)
	}
	// 空 hot 表没有待聚合数据，也没有需要归档的事件。
	if maxEventID == 0 {
		return true, nil
	}

	// 同一事务内一次读取三行，避免分别查询时观察到不一致水位。
	var checkpoints []entities.UsageAggregationCheckpoint
	names := []entities.UsageAggregationCheckpointName{
		entities.UsageAggregationCheckpointOverview,
		entities.UsageAggregationCheckpointActivity,
		entities.UsageAggregationCheckpointLatency,
	}
	if err := tx.Where("name IN ?", names).Find(&checkpoints).Error; err != nil {
		return false, fmt.Errorf("load usage aggregation archive watermarks: %w", err)
	}
	// 缺行、重复异常或任一 cursor 落后都必须保守保留 raw events。
	ready := make(map[entities.UsageAggregationCheckpointName]bool, len(names))
	for _, checkpoint := range checkpoints {
		ready[checkpoint.Name] = checkpoint.LastAggregatedUsageEventID >= maxEventID
	}
	for _, name := range names {
		if !ready[name] {
			return false, nil
		}
	}

	// Identity 没有全局 checkpoint；复用兼容追赶的同一 EXISTS 判断，但保持在当前 archive 事务内读取。
	pendingIdentity, err := hasPendingUsageIdentityAggregation(tx)
	if err != nil {
		return false, fmt.Errorf("check identity archive watermark: %w", err)
	}
	// 任一 active/deleted identity 仍有 delta 时都禁止事件离开 hot 表。
	return !pendingIdentity, nil
}

func usageEventsArchiveCutoff(now time.Time) time.Time {
	localNow := now.In(time.Local)
	localDayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	return localDayStart.AddDate(0, 0, -usageEventsRetentionDays)
}

// Vacuum 提供单独的 SQLite 收缩入口，供需要只做文件整理的调用方使用。
func Vacuum(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	return db.Exec("VACUUM").Error
}
