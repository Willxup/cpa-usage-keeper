package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"cpa-usage-keeper/internal/quota"
)

func TestRefreshCacheReadsRejectExpiryAfterRecentCleanup(t *testing.T) {
	service := &quota.Service{}
	setRefreshTasks(service, make(map[string]*quota.RefreshTaskRecord))
	now := time.Now()
	cleanupExpiredRefreshTasks(service, now)
	unauthorized := http.StatusUnauthorized
	// 在刚完成全量清理后放入过期项，读取仍须立即遵守 TTL。
	for _, authIndex := range []string{"task-expired", "cache-expired"} {
		setRefreshTask(service, authIndex, &quota.RefreshTaskRecord{
			AuthIndex: authIndex, Status: quota.RefreshTaskStatusFailed,
			HTTPStatusCode: &unauthorized, ExpiresAt: now.Add(-time.Second),
		})
	}
	setRefreshTask(service, "valid-error", &quota.RefreshTaskRecord{
		AuthIndex: "valid-error", Status: quota.RefreshTaskStatusFailed,
		HTTPStatusCode: &unauthorized, ExpiresAt: now.Add(time.Hour),
	})
	setRefreshTask(service, "completed", &quota.RefreshTaskRecord{
		AuthIndex: "completed", Status: quota.RefreshTaskStatusCompleted,
		Quota: &quota.CheckResponse{ID: "completed"},
	})
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "task-expired"); !errors.Is(err, quota.ErrTaskNotFound) {
		t.Fatalf("expired task must be absent: %v", err)
	}
	cache, err := service.GetCachedQuota(context.Background(), quota.CacheRequest{
		AuthIndexes: []string{"cache-expired", "valid-error", "completed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Items) != 2 || cache.Items[0].AuthIndex != "valid-error" || cache.Items[1].AuthIndex != "completed" {
		t.Fatalf("unexpected cache after expiry: %+v", cache.Items)
	}
}

func BenchmarkRefreshTaskLookup(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("cached_%d", count), func(b *testing.B) {
			service := &quota.Service{}
			tasks := make(map[string]*quota.RefreshTaskRecord, count)
			for i := range count {
				key := fmt.Sprintf("auth-%d", i)
				tasks[key] = &quota.RefreshTaskRecord{AuthIndex: key, Status: quota.RefreshTaskStatusCompleted}
			}
			setRefreshTasks(service, tasks)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := service.GetRefreshTaskByAuthIndex(ctx, "auth-0"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
