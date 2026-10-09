package test

import (
	"fmt"
	"strconv"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
)

func TestLoadFromEnvRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct{ key, value, want string }{
		{"REDIS_QUEUE_BATCH_SIZE", "0", "REDIS_QUEUE_BATCH_SIZE must be positive"},
		{"REDIS_QUEUE_BATCH_SIZE", strconv.Itoa(cpa.ManagementUsageQueueMaxBatchSize + 1), fmt.Sprintf("REDIS_QUEUE_BATCH_SIZE must be <= %d", cpa.ManagementUsageQueueMaxBatchSize)},
		{"BACKUP_INTERVAL", "0s", "BACKUP_INTERVAL must be positive"},
		{"BACKUP_INTERVAL", "-1h", "BACKUP_INTERVAL must be positive"},
		{"BACKUP_RETENTION_DAYS", "-1", "BACKUP_RETENTION_DAYS must be non-negative"},
		{"LOG_RETENTION_DAYS", "-1", "LOG_RETENTION_DAYS must be non-negative"},
		{"QUOTA_REFRESH_WORKER_LIMIT", "101", "QUOTA_REFRESH_WORKER_LIMIT must be <= 100"},
		{"QUOTA_REFRESH_WORKER_LIMIT", "0", "QUOTA_REFRESH_WORKER_LIMIT must be positive"},
		{"REDIS_QUEUE_IDLE_INTERVAL", "0s", "REDIS_QUEUE_IDLE_INTERVAL must be positive"},
		{"APP_BASE_PATH", "cpa", "APP_BASE_PATH is invalid: must start with '/'"},
		{"AUTH_SESSION_TTL", "0s", "AUTH_SESSION_TTL must be positive"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			isolateConfigEnv(t)
			setRequiredConfig(t)
			t.Setenv(tc.key, tc.value)
			_, err := config.LoadFromEnv()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
