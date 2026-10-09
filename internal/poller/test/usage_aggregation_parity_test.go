package poller_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestUsageAggregationRunnerPreservesExistingOverviewAndIdentityFinalSnapshots(t *testing.T) {
	// 准备：两个独立数据库写入完全相同的多维度事件和超过一页的 active/deleted identities。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	baselineDB := openUsageAggregationRunnerDatabase(t)
	runnerDB := openUsageAggregationRunnerDatabase(t)
	seedUsageAggregationParityDatabase(t, baselineDB, now)
	seedUsageAggregationParityDatabase(t, runnerDB, now)

	// 执行：基准库调用当前完整 catch-up，候选库按 Runner 的 rollups→Identity 有界 turn 轮转。
	if err := repository.AggregateUsageOverviewStats(context.Background(), baselineDB, now); err != nil {
		t.Fatalf("aggregate baseline overview: %v", err)
	}
	if err := repository.AggregateUsageIdentityStats(context.Background(), baselineDB, now); err != nil {
		t.Fatalf("aggregate baseline identities: %v", err)
	}
	runner := poller.NewUsageAggregationRunner(runnerDB)
	for transaction := 0; transaction < 6; transaction++ {
		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("runner transaction %d: %v", transaction+1, err)
		}
	}

	// 断言：结合 repository 固定 main golden 的 characterization tests，逐字段证明 Runner 拆批不改变旧业务结果。
	baseline := loadUsageAggregationParitySnapshot(t, baselineDB)
	candidate := loadUsageAggregationParitySnapshot(t, runnerDB)
	assertUsageAggregationParitySnapshot(t, candidate, baseline)
}

func assertUsageAggregationParitySnapshot(t *testing.T, candidate, baseline usageAggregationParitySnapshot) {
	t.Helper()
	if candidate.OverviewCursor != baseline.OverviewCursor || candidate.OverviewStatsUpdated != baseline.OverviewStatsUpdated {
		t.Fatalf("runner changed overview checkpoint: candidate cursor=%d stats_updated=%v; baseline cursor=%d stats_updated=%v", candidate.OverviewCursor, candidate.OverviewStatsUpdated, baseline.OverviewCursor, baseline.OverviewStatsUpdated)
	}
	if len(candidate.Hourly) != len(baseline.Hourly) || len(candidate.Daily) != len(baseline.Daily) || len(candidate.Identities) != len(baseline.Identities) {
		t.Fatalf("runner changed aggregation row counts: candidate hourly=%d daily=%d identities=%d; baseline hourly=%d daily=%d identities=%d", len(candidate.Hourly), len(candidate.Daily), len(candidate.Identities), len(baseline.Hourly), len(baseline.Daily), len(baseline.Identities))
	}
	for index := range baseline.Hourly {
		if !reflect.DeepEqual(candidate.Hourly[index], baseline.Hourly[index]) {
			t.Fatalf("runner changed hourly row %d: candidate=%+v baseline=%+v", index, candidate.Hourly[index], baseline.Hourly[index])
		}
	}
	for index := range baseline.Daily {
		if !reflect.DeepEqual(candidate.Daily[index], baseline.Daily[index]) {
			t.Fatalf("runner changed daily row %d: candidate=%+v baseline=%+v", index, candidate.Daily[index], baseline.Daily[index])
		}
	}
	for index := range baseline.Identities {
		if !reflect.DeepEqual(candidate.Identities[index], baseline.Identities[index]) {
			t.Fatalf("runner changed identity row %d: candidate=%+v baseline=%+v", index, candidate.Identities[index], baseline.Identities[index])
		}
	}
}

type usageAggregationParitySnapshot struct {
	Hourly               []usageAggregationOverviewRow
	Daily                []usageAggregationOverviewRow
	OverviewCursor       int64
	OverviewStatsUpdated bool
	Identities           []usageAggregationIdentityRow
}

type usageAggregationOverviewRow struct {
	BucketStart         time.Time
	APIGroupKey         string
	Model               string
	AuthIndex           string
	ModelAlias          string
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
}

type usageAggregationIdentityRow struct {
	Identity        string
	IsDeleted       bool
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	CacheReadTokens int64
	TotalTokens     int64
	Cursor          int64
	FirstUsedAt     *time.Time
	LastUsedAt      *time.Time
	StatsUpdated    bool
}

func seedUsageAggregationParityDatabase(t *testing.T, db *gorm.DB, now time.Time) {
	// 准备：27 行强制 Identity runner 分两页提交，1001 条事件强制 Overview/Activity 各分两批。
	t.Helper()
	identities := make([]entities.UsageIdentity, 0, 27)
	events := make([]entities.UsageEvent, 0, 1001)
	for index := 1; index <= 27; index++ {
		identity := fmt.Sprintf("parity-auth-%02d", index)
		identities = append(identities, entities.UsageIdentity{
			Name: identity, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: identity, Type: "codex", IsDeleted: index%2 == 0,
		})
	}
	// 准备：事件循环复用 27 个 identity，并覆盖多 API group、model、alias、成功失败和全部旧 Token 字段。
	for index := 1; index <= 1001; index++ {
		identityIndex := (index-1)%27 + 1
		identity := fmt.Sprintf("parity-auth-%02d", identityIndex)
		alias := fmt.Sprintf("alias-%d", index%3)
		events = append(events, entities.UsageEvent{
			EventKey: fmt.Sprintf("parity-event-%04d", index), APIGroupKey: fmt.Sprintf("provider-%d", index%2),
			Model: fmt.Sprintf("model-%d", index%4), ModelAlias: &alias, AuthType: "oauth", AuthIndex: identity,
			Timestamp: now.Add(-time.Duration(index%27+1) * time.Minute), Failed: index%5 == 0,
			InputTokens: int64(index), OutputTokens: int64(index * 2), ReasoningTokens: int64(index * 3),
			CachedTokens: int64(index * 11), CacheReadTokens: int64(index * 5), CacheCreationTokens: int64(index * 7), TotalTokens: int64(index * 13),
		})
	}
	// 准备：先建 identity，确保两条执行路径读取相同 ID 页边界。
	if err := db.Create(&identities).Error; err != nil {
		t.Fatalf("seed parity identities: %v", err)
	}
	// 准备：再写 usage events，确保两个数据库自增 ID 完全一致。
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed parity usage events: %v", err)
	}
}

func loadUsageAggregationParitySnapshot(t *testing.T, db *gorm.DB) usageAggregationParitySnapshot {
	t.Helper()
	var hourly []entities.UsageOverviewHourlyStat
	if err := db.Order("bucket_start asc, api_group_key asc, model asc, auth_index asc, model_alias asc").Find(&hourly).Error; err != nil {
		t.Fatalf("load parity hourly rows: %v", err)
	}
	var daily []entities.UsageOverviewDailyStat
	if err := db.Order("bucket_start asc, api_group_key asc, model asc, auth_index asc, model_alias asc").Find(&daily).Error; err != nil {
		t.Fatalf("load parity daily rows: %v", err)
	}
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load parity overview checkpoint: %v", err)
	}
	var identities []entities.UsageIdentity
	if err := db.Order("identity asc").Find(&identities).Error; err != nil {
		t.Fatalf("load parity identities: %v", err)
	}
	// 断言准备：机械转换为不含自增 ID 和调度时间戳的业务快照。
	// 调度改为异步后实际执行时间可以不同，跨路径只比较旧 timestamp 字段是否被正确推进。
	snapshot := usageAggregationParitySnapshot{OverviewCursor: checkpoint.LastAggregatedUsageEventID, OverviewStatsUpdated: checkpoint.StatsUpdatedAt != nil}
	for _, row := range hourly {
		snapshot.Hourly = append(snapshot.Hourly, usageAggregationOverviewRowFromHourly(row))
	}
	for _, row := range daily {
		snapshot.Daily = append(snapshot.Daily, usageAggregationOverviewRowFromDaily(row))
	}
	for _, row := range identities {
		snapshot.Identities = append(snapshot.Identities, usageAggregationIdentityRow{
			Identity: row.Identity, IsDeleted: row.IsDeleted, TotalRequests: row.TotalRequests,
			SuccessCount: row.SuccessCount, FailureCount: row.FailureCount, InputTokens: row.InputTokens,
			OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens, CachedTokens: row.CachedTokens,
			CacheReadTokens: row.CacheReadTokens, TotalTokens: row.TotalTokens, Cursor: row.LastAggregatedUsageEventID,
			FirstUsedAt: row.FirstUsedAt, LastUsedAt: row.LastUsedAt,
			StatsUpdated: row.StatsUpdatedAt != nil,
		})
	}
	return snapshot
}

func usageAggregationOverviewRowFromHourly(row entities.UsageOverviewHourlyStat) usageAggregationOverviewRow {
	return usageAggregationOverviewRow{
		BucketStart: row.BucketStart, APIGroupKey: row.APIGroupKey, Model: row.Model, AuthIndex: row.AuthIndex, ModelAlias: row.ModelAlias,
		RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, FailureCount: row.FailureCount,
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
		CachedTokens: row.CachedTokens, CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, TotalTokens: row.TotalTokens,
	}
}

func usageAggregationOverviewRowFromDaily(row entities.UsageOverviewDailyStat) usageAggregationOverviewRow {
	return usageAggregationOverviewRow{
		BucketStart: row.BucketStart, APIGroupKey: row.APIGroupKey, Model: row.Model, AuthIndex: row.AuthIndex, ModelAlias: row.ModelAlias,
		RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, FailureCount: row.FailureCount,
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
		CachedTokens: row.CachedTokens, CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, TotalTokens: row.TotalTokens,
	}
}
