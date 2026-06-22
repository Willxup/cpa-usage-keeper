package benchmark

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

const redisProcessBenchmarkIdentityCount = 10_000

func BenchmarkRedisUsageInboxProcessBatch(b *testing.B) {
	for _, size := range []int{10, 100, 500, 1_000} {
		b.Run(fmt.Sprintf("rows_%d_identities_%d", size, redisProcessBenchmarkIdentityCount), func(b *testing.B) {
			benchmarkRedisUsageInboxProcessing(b, size, redisProcessBenchmarkIdentityCount, false)
		})
	}
}

func BenchmarkRedisUsageInboxProcessBatchThousandIdentities(b *testing.B) {
	for _, size := range []int{100, 500, 999, 1_000} {
		b.Run(fmt.Sprintf("rows_%d_identities_%d", size, 1_000), func(b *testing.B) {
			benchmarkRedisUsageInboxProcessing(b, size, 1_000, false)
		})
	}
}

func BenchmarkRedisUsageInboxProcessDrain(b *testing.B) {
	benchmarkRedisUsageInboxProcessing(b, 10_000, redisProcessBenchmarkIdentityCount, true)
}

func BenchmarkRedisUsageInboxProcessDrainThousandIdentities(b *testing.B) {
	benchmarkRedisUsageInboxProcessing(b, 10_000, 1_000, true)
}

func BenchmarkRedisUsageInboxWriterRefreshFiltered(b *testing.B) {
	b.ReportAllocs()
	var totalInserted int
	var totalElapsed time.Duration
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := openRedisProcessBenchmarkDBWithoutSeed(b, i)
		writer := poller.NewControlAwareRedisInboxWriter(poller.NewRedisInboxWriter(db), &redisProcessBenchmarkRefreshObserver{})
		messages := redisProcessBenchmarkMessagesWithRefresh(1_000, 1_000, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
		b.StartTimer()

		startedAt := time.Now()
		inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceRedisPull, messages, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
		elapsed := time.Since(startedAt)
		if err != nil {
			b.Fatalf("writer.Insert returned error: %v", err)
		}
		b.StopTimer()

		totalInserted += inserted
		totalElapsed += elapsed
		closeRedisProcessBenchmarkDB(b, db)
	}
	if totalElapsed > 0 {
		b.ReportMetric(float64(totalInserted)/totalElapsed.Seconds(), "inbox_rows/sec")
	}
}

func benchmarkRedisUsageInboxProcessing(b *testing.B, rowCount, identityCount int, drain bool) {
	b.Helper()
	b.ReportAllocs()

	var totalInserted int
	var totalBatches int
	var totalElapsed time.Duration

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := openRedisProcessBenchmarkDB(b, rowCount, identityCount, i)
		syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
			Now: func() time.Time { return time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC) },
		})
		b.StartTimer()

		startedAt := time.Now()
		result, nonEmptyBatches, err := processRedisUsageBenchmarkRows(syncService, drain)
		elapsed := time.Since(startedAt)
		if err != nil {
			b.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
		}
		b.StopTimer()

		totalInserted += result.InsertedEvents
		totalBatches += nonEmptyBatches
		totalElapsed += elapsed
		closeRedisProcessBenchmarkDB(b, db)
	}

	if totalElapsed > 0 {
		b.ReportMetric(float64(totalInserted)/totalElapsed.Seconds(), "events/sec")
	}
	if totalBatches > 0 {
		b.ReportMetric(float64(totalInserted)/float64(totalBatches), "events/batch")
	}
}

func processRedisUsageBenchmarkRows(syncService *service.SyncService, drain bool) (servicedto.RedisBatchSyncResult, int, error) {
	total := servicedto.RedisBatchSyncResult{}
	nonEmptyBatches := 0
	for {
		result, err := syncService.ProcessRedisUsageInbox(context.Background())
		if result != nil {
			total.InsertedEvents += result.InsertedEvents
			total.DedupedEvents += result.DedupedEvents
			total.Status = result.Status
			total.Empty = result.Empty
			if result.InsertedEvents > 0 {
				nonEmptyBatches++
			}
		}
		if err != nil {
			return total, nonEmptyBatches, err
		}
		if !drain || result == nil || result.Empty || result.InsertedEvents == 0 {
			return total, nonEmptyBatches, nil
		}
	}
}

func openRedisProcessBenchmarkDB(b *testing.B, rowCount, identityCount, iteration int) *gorm.DB {
	b.Helper()
	db := openRedisProcessBenchmarkDBWithoutSeed(b, iteration)
	seedRedisProcessBenchmarkIdentities(b, db, identityCount)
	seedRedisProcessBenchmarkInbox(b, db, rowCount, identityCount)
	return db
}

func openRedisProcessBenchmarkDBWithoutSeed(b *testing.B, iteration int) *gorm.DB {
	b.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(b.TempDir(), fmt.Sprintf("redis-process-%d.db", iteration))})
	if err != nil {
		b.Fatalf("OpenDatabase returned error: %v", err)
	}
	return db
}

func seedRedisProcessBenchmarkIdentities(b *testing.B, db *gorm.DB, identityCount int) {
	b.Helper()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	identities := make([]entities.UsageIdentity, 0, identityCount)
	for i := 0; i < identityCount; i++ {
		identity := redisProcessBenchmarkAuthIndex(i)
		identities = append(identities, entities.UsageIdentity{
			Name:         identity,
			AuthType:     entities.UsageIdentityAuthTypeAuthFile,
			AuthTypeName: "auth_file",
			Identity:     identity,
			Type:         "codex",
			Provider:     "codex",
			LookupKey:    identity,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	if err := db.CreateInBatches(&identities, 20).Error; err != nil {
		b.Fatalf("seed usage identities returned error: %v", err)
	}
}

func seedRedisProcessBenchmarkInbox(b *testing.B, db *gorm.DB, rowCount, identityCount int) {
	b.Helper()
	messages := make([]string, 0, rowCount)
	base := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	for i := 0; i < rowCount; i++ {
		messages = append(messages, redisProcessBenchmarkMessage(i, identityCount, base))
	}
	if _, err := repository.InsertRedisUsageInboxRawMessages(db, "redis_pull:usage", messages, base); err != nil {
		b.Fatalf("seed redis usage inbox returned error: %v", err)
	}
}

func redisProcessBenchmarkMessage(i, identityCount int, base time.Time) string {
	authIndex := redisProcessBenchmarkAuthIndex(i % identityCount)
	timestamp := base.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)
	return fmt.Sprintf(
		`{"timestamp":%q,"latency_ms":120,"source":"redis","auth_index":%q,"tokens":{"input_tokens":120,"output_tokens":48,"reasoning_tokens":8,"cached_tokens":16,"cache_read_tokens":4,"cache_creation_tokens":2,"total_tokens":176},"provider":"codex","model":"gpt-5","auth_type":"oauth","api_key":"bench-group","request_id":%q}`,
		timestamp,
		authIndex,
		fmt.Sprintf("bench-%08d", i),
	)
}

func redisProcessBenchmarkMessagesWithRefresh(totalMessages, identityCount int, base time.Time) []string {
	messages := make([]string, 0, totalMessages)
	for i := 0; i < totalMessages; i++ {
		if i == totalMessages-1 {
			messages = append(messages, `{"refresh":true}`)
			continue
		}
		messages = append(messages, redisProcessBenchmarkMessage(i, identityCount, base))
	}
	return messages
}

func redisProcessBenchmarkAuthIndex(i int) string {
	return fmt.Sprintf("auth-%05d", i)
}

type redisProcessBenchmarkRefreshObserver struct{}

func (o *redisProcessBenchmarkRefreshObserver) MarkRefreshSupported() {}

func (o *redisProcessBenchmarkRefreshObserver) RequestMetadataRefresh() {}

func (o *redisProcessBenchmarkRefreshObserver) MarkRefreshPollingRequired(string) {}

func closeRedisProcessBenchmarkDB(b *testing.B, db *gorm.DB) {
	b.Helper()
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
