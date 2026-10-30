# Wayfinder Decision #8: PostgreSQL Migration & Dialect Abstraction Architecture

## 1. Context & Motivation
CPA Usage Keeper currently uses embedded SQLite (`gorm.io/driver/sqlite`) with WAL mode and dedicated single-writer connection pools. While lightweight, SQLite limits concurrency under high-throughput request ingestion and makes multi-instance clustering or external database management impossible.
Migrating to PostgreSQL enables:
- True concurrent multi-writer execution without table locks.
- Native enterprise clustering, backups, and point-in-time recovery.
- Centralized storage shared between multiple CPA Usage Keeper replicas if needed.

## 2. Dialect Abstraction Architecture
Introduce a storage dialect interface in `internal/repository/db.go`:

```go
type DatabaseDialect string

const (
    DialectSQLite   DatabaseDialect = "sqlite"
    DialectPostgres DatabaseDialect = "postgres"
)

type DatabaseConfig struct {
    Dialect     DatabaseDialect
    DSN         string
    MaxOpenCons int
    MaxIdleCons int
}
```

### Connection Pool Configuration
- **SQLite**: Keep existing single-writer semantics (`MaxOpen = 1`) and 8 read replicas with WAL mode.
- **PostgreSQL**: Standard connection pool with `MaxOpen = 25`, `MaxIdle = 10`, and `ConnMaxLifetime = 5m`.

### Abstracting SQLite-Specific Queries
1. Replace raw `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'` with dialect-agnostic GORM `db.Migrator().HasTable(&Entity{})`.
2. Replace `sqlite3.SQLiteTimestampFormats` custom parsing with standard SQL timestamp types.
3. Replace CGO `sqlite3_backup` in `internal/backup/backup.go` with a polymorphic backup strategy:
   - For SQLite: retain CGO `sqlite3_backup` online snapshot.
   - For PostgreSQL: trigger `pg_dump` worker or SQL transaction dump without locking readers.

## 3. Zero-Downtime Data Migration Route
1. Export existing SQLite tables to neutral JSONL / SQL dump.
2. Initialize PostgreSQL schema using GORM AutoMigrate.
3. Stream data batches from SQLite to PostgreSQL with identity mapping.
4. Verify checksums and row counts between both databases.
5. Cut over `DATABASE_URL` environment variable in `docker-compose.yml`.
