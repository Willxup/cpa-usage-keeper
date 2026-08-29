package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestUsageIdentityAggregationPreservesExistingFinalSnapshots(t *testing.T) {
	// 准备：固定项目时区和聚合 now，构造 active、deleted 与 metadata 后创建三类 identity。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)

	// active auth file 带初始统计，验证增量只能追加不能覆盖旧值。
	active := entities.UsageIdentity{
		Name: "Active", AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "auth-active", Type: "codex",
		TotalRequests: 10, SuccessCount: 8, FailureCount: 2, InputTokens: 1000, OutputTokens: 200,
		ReasoningTokens: 30, CachedTokens: 400, CacheReadTokens: 40, TotalTokens: 1270,
	}
	// deleted AI provider 仍必须继续累计历史事件，不能因 metadata 状态跳过。
	deleted := entities.UsageIdentity{
		Name: "Deleted", AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "key-deleted", Type: "openai", IsDeleted: true,
		TotalRequests: 20, SuccessCount: 15, FailureCount: 5, InputTokens: 2000, OutputTokens: 300,
		ReasoningTokens: 40, CachedTokens: 500, CacheReadTokens: 50, TotalTokens: 2340,
	}
	if err := db.Create(&[]entities.UsageIdentity{active, deleted}).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}

	// late identity 的事件先入库，首次聚合时还没有对应 metadata 行。
	events := []entities.UsageEvent{
		{EventKey: "identity-active-1", AuthType: "oauth", AuthIndex: "auth-active", Timestamp: now.Add(-2 * time.Hour), Failed: false, InputTokens: 100, OutputTokens: 20, ReasoningTokens: 3, CachedTokens: 90, CacheReadTokens: 9, TotalTokens: 123},
		{EventKey: "identity-active-2", AuthType: "oauth", AuthIndex: "auth-active", Timestamp: now.Add(-time.Hour), Failed: true, InputTokens: 200, OutputTokens: 30, ReasoningTokens: 4, CachedTokens: 80, CacheReadTokens: 8, TotalTokens: 234},
		{EventKey: "identity-deleted-1", AuthType: "apikey", AuthIndex: "key-deleted", Timestamp: now.Add(-90 * time.Minute), Failed: false, InputTokens: 300, OutputTokens: 40, ReasoningTokens: 5, CachedTokens: 70, CacheReadTokens: 7, TotalTokens: 345},
		{EventKey: "identity-late-1", AuthType: "oauth", AuthIndex: "auth-late", Timestamp: now.Add(-3 * time.Hour), Failed: false, InputTokens: 400, OutputTokens: 50, ReasoningTokens: 6, CachedTokens: 60, CacheReadTokens: 6, TotalTokens: 456},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert identity usage events: %v", err)
	}

	// 执行：第一次聚合只处理当时存在的 active/deleted identities。
	if err := repository.AggregateUsageIdentityStats(context.Background(), db, now); err != nil {
		t.Fatalf("first AggregateUsageIdentityStats: %v", err)
	}
	// 断言：active/deleted 的旧统计、cached_tokens、cursor 和首尾时间逐字段保持原语义。
	assertUsageIdentitySnapshot(t, db, "auth-active", usageIdentitySnapshot{
		TotalRequests: 12, Success: 9, Failure: 3, Input: 1300, Output: 250, Reasoning: 37,
		Cached: 570, CacheRead: 57, Total: 1627, Cursor: 2,
		FirstUsedAt: now.Add(-2 * time.Hour), LastUsedAt: now.Add(-time.Hour), StatsUpdatedAt: now, IsDeleted: false,
	})
	assertUsageIdentitySnapshot(t, db, "key-deleted", usageIdentitySnapshot{
		TotalRequests: 21, Success: 16, Failure: 5, Input: 2300, Output: 340, Reasoning: 45,
		Cached: 570, CacheRead: 57, Total: 2685, Cursor: 3,
		FirstUsedAt: now.Add(-90 * time.Minute), LastUsedAt: now.Add(-90 * time.Minute), StatsUpdatedAt: now, IsDeleted: true,
	})

	// 执行：metadata 后创建 identity，并从 cursor=0 再运行一次完整聚合。
	late := entities.UsageIdentity{Name: "Late", AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "auth-late", Type: "codex"}
	if err := db.Create(&late).Error; err != nil {
		t.Fatalf("create late identity: %v", err)
	}
	if err := repository.AggregateUsageIdentityStats(context.Background(), db, now.Add(time.Minute)); err != nil {
		t.Fatalf("second AggregateUsageIdentityStats: %v", err)
	}
	// 断言：late identity 必须回补已经存在的历史事件。
	assertUsageIdentitySnapshot(t, db, "auth-late", usageIdentitySnapshot{
		TotalRequests: 1, Success: 1, Failure: 0, Input: 400, Output: 50, Reasoning: 6,
		Cached: 60, CacheRead: 6, Total: 456, Cursor: 4,
		FirstUsedAt: now.Add(-3 * time.Hour), LastUsedAt: now.Add(-3 * time.Hour), StatsUpdatedAt: now.Add(time.Minute), IsDeleted: false,
	})

	// 执行：第三次完整聚合验证所有每行 cursor 已经追平。
	if err := repository.AggregateUsageIdentityStats(context.Background(), db, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("third AggregateUsageIdentityStats: %v", err)
	}
	// 断言：旧 snapshot 保持幂等，不能重复累计。
	assertUsageIdentitySnapshot(t, db, "auth-active", usageIdentitySnapshot{
		TotalRequests: 12, Success: 9, Failure: 3, Input: 1300, Output: 250, Reasoning: 37,
		Cached: 570, CacheRead: 57, Total: 1627, Cursor: 2,
		FirstUsedAt: now.Add(-2 * time.Hour), LastUsedAt: now.Add(-time.Hour), StatsUpdatedAt: now, IsDeleted: false,
	})
}

type usageIdentitySnapshot struct {
	TotalRequests  int64
	Success        int64
	Failure        int64
	Input          int64
	Output         int64
	Reasoning      int64
	Cached         int64
	CacheRead      int64
	Total          int64
	Cursor         int64
	FirstUsedAt    time.Time
	LastUsedAt     time.Time
	StatsUpdatedAt time.Time
	IsDeleted      bool
}

func assertUsageIdentitySnapshot(t *testing.T, db interface {
	Where(query any, args ...any) *gorm.DB
}, identity string, want usageIdentitySnapshot) {
	// 逐字段比较最终 identity 行，确保批次化只改变事务边界、不改变聚合效果。
	t.Helper()
	var row entities.UsageIdentity
	if err := db.Where("identity = ?", identity).Take(&row).Error; err != nil {
		t.Fatalf("load identity %s: %v", identity, err)
	}
	if row.TotalRequests != want.TotalRequests || row.SuccessCount != want.Success || row.FailureCount != want.Failure ||
		row.InputTokens != want.Input || row.OutputTokens != want.Output || row.ReasoningTokens != want.Reasoning ||
		row.CachedTokens != want.Cached || row.CacheReadTokens != want.CacheRead || row.TotalTokens != want.Total ||
		row.LastAggregatedUsageEventID != want.Cursor || row.IsDeleted != want.IsDeleted {
		t.Fatalf("unexpected identity %s snapshot: got=%+v want=%+v", identity, row, want)
	}
	if row.FirstUsedAt == nil || !row.FirstUsedAt.Equal(want.FirstUsedAt) {
		t.Fatalf("unexpected identity %s first_used_at: got=%v want=%v", identity, row.FirstUsedAt, want.FirstUsedAt)
	}
	if row.LastUsedAt == nil || !row.LastUsedAt.Equal(want.LastUsedAt) {
		t.Fatalf("unexpected identity %s last_used_at: got=%v want=%v", identity, row.LastUsedAt, want.LastUsedAt)
	}
	if row.StatsUpdatedAt == nil || !row.StatsUpdatedAt.Equal(want.StatsUpdatedAt) {
		t.Fatalf("unexpected identity %s stats_updated_at: got=%v want=%v", identity, row.StatsUpdatedAt, want.StatsUpdatedAt)
	}
}
