package migration

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func findUsageIdentity(t *testing.T, identities []entities.UsageIdentity, authType entities.UsageIdentityAuthType, identity string) entities.UsageIdentity {
	t.Helper()
	for _, usageIdentity := range identities {
		if usageIdentity.AuthType == authType && usageIdentity.Identity == identity {
			return usageIdentity
		}
	}
	t.Fatalf("usage identity auth_type=%d identity=%q not found in %+v", authType, identity, identities)
	return entities.UsageIdentity{}
}

func loadUsageIdentity(t *testing.T, db *gorm.DB, authType entities.UsageIdentityAuthType, identity string) entities.UsageIdentity {
	t.Helper()
	var usageIdentity entities.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", authType, identity).First(&usageIdentity).Error; err != nil {
		t.Fatalf("load usage identity auth_type=%d identity=%q: %v", authType, identity, err)
	}
	return usageIdentity
}

func countUsageIdentities(t *testing.T, db *gorm.DB, authType entities.UsageIdentityAuthType, identity string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&entities.UsageIdentity{}).Where("auth_type = ? AND identity = ?", authType, identity).Count(&count).Error; err != nil {
		t.Fatalf("count usage identity auth_type=%d identity=%q: %v", authType, identity, err)
	}
	return count
}

func openMigratedDatabase(t *testing.T, dbPath string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	if err := Run(db, RunOptions{BeforeDestructiveMigration: func(context.Context, string) error { return nil }}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return db
}

func captureMigrationLogs(t *testing.T, level logrus.Level) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(level)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func closeOpenedDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

func testSQLiteDSN(path string) string {
	trimmed := strings.TrimSpace(path)
	if strings.Contains(trimmed, "?") {
		return trimmed
	}
	return trimmed + "?_busy_timeout=5000&_foreign_keys=on"
}
