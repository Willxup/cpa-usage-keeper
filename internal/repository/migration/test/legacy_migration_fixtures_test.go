package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
)

// publishedTestOverviewStat 供旧迁移夹具按真实旧列写入和读取，不随当前统计实体移除维度。
type publishedTestOverviewStat struct {
	ID                  int64     `gorm:"primaryKey"`
	BucketStart         time.Time `gorm:"serializer:storageTime"`
	APIGroupKey         string
	Model               string
	AuthIndex           string
	ModelAlias          string
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
	RequestCount        int64
	SuccessCount        int64
	FailureCount        int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	CreatedAt           time.Time `gorm:"serializer:storageTime"`
	UpdatedAt           time.Time `gorm:"serializer:storageTime"`
}

// 每次重新标记目标为待执行，保留旧测试对重复执行迁移的幂等检查。
func runLegacyMigration(db *gorm.DB, version string) error {
	if err := migration.MarkAllAsApplied(db); err != nil {
		return err
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		return err
	}
	return migration.Run(db)
}

func orderedMigrationVersions(t *testing.T) []string {
	t.Helper()
	db := openUnmigratedTestDatabase(t)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("record registered migration versions: %v", err)
	}
	// MarkAllAsApplied 按注册顺序写入；rowid 保留该顺序，不能按版本字符串排序。
	var versions []string
	if err := db.Table("schema_migrations").Order("rowid").Pluck("version", &versions).Error; err != nil {
		t.Fatalf("read registered migration order: %v", err)
	}
	return versions
}
