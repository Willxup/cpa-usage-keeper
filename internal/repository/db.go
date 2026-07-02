package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) }})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %s: %w", filepath.Clean(cfg.SQLitePath), err)
	}

	// SQLite 写入仍是单 writer，连接池限制成单连接，避免同进程多个连接互相抢写锁。
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("configure sqlite database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// WAL 让读写并发更友好，但 writer 之间仍串行；这里配合 busy_timeout 等待短暂锁竞争。
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return nil, fmt.Errorf("enable sqlite WAL: %w", err)
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
		return db, nil
	}

	// 已有业务表的数据库必须走显式迁移，确保旧库按版本顺序补齐结构和索引。
	if err := migration.Run(db); err != nil {
		return nil, fmt.Errorf("run schema migrations: %w", err)
	}

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

// sqliteDatabaseFileExists 判断磁盘数据库文件是否存在；内存库和空路径都按新库处理。
func sqliteDatabaseFileExists(path string) (bool, error) {
	trimmed := strings.TrimSpace(path)
	if before, _, ok := strings.Cut(trimmed, "?"); ok {
		trimmed = before
	}
	if trimmed == "" || trimmed == ":memory:" {
		return false, nil
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

type CleanupStorageOptions struct {
	// CleanupUsageEvents 控制是否删除过期 usage_events 原始事件；默认 false 表示保留原始事件。
	CleanupUsageEvents bool
}

// CleanupStorage 是每日维护任务的统一仓储清理入口：先清 Redis inbox，按配置清过期 usage_events，再清 Overview health 细粒度统计，最后执行 VACUUM。
// VACUUM 必须在删除完成后单独执行，任何一步失败都会停止后续步骤并把已完成部分的结果返回给上层日志。
func CleanupStorage(db *gorm.DB, now time.Time, options ...CleanupStorageOptions) (dto.StorageCleanupResult, error) {
	opts := CleanupStorageOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	redisResult, err := CleanupRedisUsageInbox(db, now)
	if err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult}, err
	}
	var usageEventsDeleted int64
	if opts.CleanupUsageEvents {
		usageEventsDeleted, err = cleanupUsageEvents(db, now)
		if err != nil {
			return dto.StorageCleanupResult{RedisInbox: redisResult, UsageEventsDeleted: usageEventsDeleted}, err
		}
	}
	// Health stats 只服务最近窗口展示，过期数据在每日维护中清掉，避免表无限增长。
	if err := CleanupUsageOverviewHealthStats(db, now); err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult, UsageEventsDeleted: usageEventsDeleted}, err
	}
	// SQLite 删除不会立即缩小文件，维护窗口最后统一 VACUUM。
	if err := db.Exec("VACUUM").Error; err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult, UsageEventsDeleted: usageEventsDeleted}, err
	}
	return dto.StorageCleanupResult{RedisInbox: redisResult, UsageEventsDeleted: usageEventsDeleted}, nil
}

// cleanupUsageEvents 删除当前页面查询窗口外的原始 usage_events，保留从上个月 1 日本地零点开始的数据。
func cleanupUsageEvents(db *gorm.DB, now time.Time) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	cutoff := usageEventsCleanupCutoff(now)
	result := db.Unscoped().Where("timestamp < ?", timeutil.FormatStorageTime(cutoff)).Delete(&entities.UsageEvent{})
	if result.Error != nil {
		return result.RowsAffected, fmt.Errorf("cleanup usage events: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func usageEventsCleanupCutoff(now time.Time) time.Time {
	localNow := now.In(time.Local)
	currentMonthStart := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, time.Local)
	return currentMonthStart.AddDate(0, -1, 0)
}

// Vacuum 提供单独的 SQLite 收缩入口，供需要只做文件整理的调用方使用。
func Vacuum(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	return db.Exec("VACUUM").Error
}
